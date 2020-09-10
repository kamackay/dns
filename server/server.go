package server

import (
	"errors"
	"fmt"
	"github.com/fsnotify/fsnotify"
	"github.com/miekg/dns"
	"gitlab.com/kamackay/dns/dns_resolver"
	"gitlab.com/kamackay/dns/logging"
	"gitlab.com/kamackay/dns/util"
	"math"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
)

func (this *Server) lookupInMap(domainName string) (*Domain, int8) {
	domainInterface, ok := lookupInMapAndUpdate(convertMutexToMap(&this.domains),
		domainName,
		func(domain *Domain) {
			domain.Requests++
		})
	var domain *Domain
	var result int8
	this.log.Debugf("Using: %+v, %t", domain, ok)
	if ok {
		domain = domainInterface.(*Domain)
		result = getResultFromDomain(domain)
		if time.Now().UnixNano()/NanoConv-domain.Time/NanoConv <= int64(domain.Ttl)*1000 {
			return domain, result
		}
	} else {
		result = NotFound
	}

	if domain != nil && domain.Block {
		// If the domain is blocked, add it to the map so that the next lookup is faster
		this.store(&Domain{
			Name:     domainName,
			Time:     time.Now().UnixNano(),
			Ip:       BlockedIp,
			Block:    true,
			Requests: 1,
			Ttl:      math.MaxUint32,
		})
	}

	if domain == nil {
		return getFailedDomainObj(domainName), result
	}
	this.stats.CachedRequests++
	return domain, result
}

func (this *Server) store(domain *Domain) {
	oldDomainInterface, ok := this.domains.Load(domain.Name)
	if ok {
		// Domain was already in map, update
		oldDomain := oldDomainInterface.(*Domain)
		oldDomain.Requests++
		this.domains.Store(domain.Name, oldDomain)
	} else {
		// Domain was not in map, add
		this.domains.Store(domain.Name, domain)
	}
}

func (this *Server) getIp(domainName string) (*Domain, error) {
	if this.checkBlock(domainName) {
		return getBlockedDomainObj(domainName), errors.New("blocked " + domainName)
	}
	address, result := this.lookupInMap(domainName)
	if result == Ok {
		return address, nil
	} else if result == Block {
		this.log.Warnf("Blocking %s", domainName)
		this.stats.BlockedRequests++
		this.addMetric(Metric{
			MetricType: "Block",
			Time:       time.Now().UnixNano() / NanoConv,
			Ip:         BlockedIp,
			Server:     NoServer,
			Blocked:    false,
			Domain:     domainName,
		})
		return getBlockedDomainObj(domainName), errors.New("blocked " + domainName)
	} else {
		if result, err := this.resolver.LookupHost(strings.TrimRight(domainName, "."));
			err != nil || len(result.Ips) == 0 {
			this.stats.FailedRequests++
			this.stats.FailedDomains = unique(append(this.stats.FailedDomains, domainName))
			this.log.Error(err)
			return getFailedDomainObj(domainName), err
		} else {
			answer := result.Ips[0]
			this.log.Infof("Fetched \"%s\" = %s from %s",
				domainName, answer.Address, result.Server)
			domain := &Domain{
				Ip:       answer.Address,
				Ttl:      answer.Ttl,
				Name:     domainName,
				Time:     time.Now().UnixNano(),
				Block:    false,
				Requests: 1,
				Server:   result.Server,
			}
			go func() {
				// Add to cache
				this.store(domain)
				this.stats.LookupRequests++
				this.addMetric(Metric{
					MetricType: "Fetch",
					Time:       0,
					Ip:         answer.Address,
					Server:     result.Server,
					Blocked:    false,
					Domain:     domainName,
				})
				//this.printAllHosts()
			}()
			return domain, nil
		}
	}
}

// Return True if Blocked
func (this *Server) checkBlock(domain string) bool {
	val, ok := lookupBoolInMap(this.config.Blocks, domain)
	return ok && val != nil && *val
}

func (this *Server) ServeDNS(w dns.ResponseWriter, r *dns.Msg) {
	start := time.Now().UnixNano()
	defer func() {
		if recovered := recover(); recovered != nil {
			fmt.Println("Recovering from:", r)
			_ = w.Close()
			this.stats.FailedRequests++
			this.stats.FailedDomains = unique(append(this.stats.FailedDomains, r.Question[0].Name))
		}
	}()
	msg := dns.Msg{}
	msg.SetReply(r)
	switch r.Question[0].Qtype {
	case dns.TypeA:
		msg.Authoritative = true
		for _, question := range msg.Question {
			domain := question.Name
			result, err := this.getIp(domain)
			defer func() {
				this.log.Infof("Lookup %s in %s -> %s",
					domain, util.PrintTimeDiff(start), result.Ip)
				this.addMetric(Metric{
					MetricType: "Answer",
					Time:       (time.Now().UnixNano() - start) / NanoConv,
					Ip:         result.Ip,
					Server:     result.Server,
					Blocked:    false,
					Domain:     result.Name,
				})
			}()
			if err == nil {
				msg.Answer = append(msg.Answer, &dns.A{
					Hdr: dns.RR_Header{Name: domain, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 60},
					A:   net.ParseIP(result.Ip),
				})
			}
		}
	}
	_ = w.WriteMsg(&msg)
}

func New(port int) (*dns.Server, *Server) {
	srv := &dns.Server{Addr: ":" + strconv.Itoa(port), Net: "udp"}
	config, err := readConfig()
	if err != nil {
		fmt.Println("Error Reading the Config", err.Error())
		return nil, nil
	}
	client := &Server{
		resolver:   dns_resolver.New(config.DnsServers, config.DohServer),
		config:     config,
		printMutex: &sync.Mutex{},
		log:        logging.GetLogger(),
		stats: Stats{
			LookupRequests: 0,
			CachedRequests: 0,
			Domains:        make([]*Domain, 0),
			Started:        time.Now().UnixNano(),
			FailedDomains:  make([]string, 0),
			Metrics:        make([]Metric, 0),
		},
	}
	convertMapToMutex(config.Hosts).
		Range(func(key, value interface{}) bool {
			client.store(&Domain{
				Name:  key.(string),
				Ip:    value.(string),
				Time:  time.Now().UnixNano(),
				Block: false,
			})
			return true
		})
	srv.Handler = client
	watcher, err := fsnotify.NewWatcher()
	if err == nil && watcher.Add("/config.json") == nil {
		go func() {
			for {
				select {
				// watch for events
				case _ = <-watcher.Events:
					client.log.Info("Reloading Config File")
					client.loadConfig()
				}
			}
		}()
	}
	return srv, client
}

func (this *Server) loadConfig() {
	newConfig, err := readConfig()
	if err != nil {
		fmt.Println("Error Reading the Config", err.Error())
		return
	} else {
		this.log.Info("Reloading Config File")
	}
	this.config = newConfig
	this.resolver = dns_resolver.New(newConfig.DnsServers, newConfig.DohServer)
	convertMapToMutex(newConfig.Hosts).
		Range(func(key, value interface{}) bool {
			this.domains.Store(key, &Domain{
				Name:  key.(string),
				Ip:    value.(string),
				Time:  math.MaxInt64,
				Block: false,
				Ttl:   math.MaxUint32,
			})
			return true
		})
}

func (this *Server) flushDns() error {
	this.domains.Range(func(key, value interface{}) bool {
		domain := value.(*Domain)
		host := key.(string)
		if !domain.Block {
			// Remove all servers except for the blocked ones
			this.domains.Delete(host)
		}
		return true
	})
	return nil
}

func (this *Server) PreStart() {
	this.startRest(this.flushDns)
	go func() {
		this.loadConfig()
		time.Sleep(time.Second)
		this.pullBlockList()
	}()
}
