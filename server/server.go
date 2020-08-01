package server

import (
	"errors"
	"fmt"
	"github.com/fsnotify/fsnotify"
	"github.com/gin-contrib/cors"
	"github.com/gin-contrib/gzip"
	"github.com/gin-gonic/gin"
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

func (this *Server) lookupInMap(domainName string) (string, int8) {
	domainInterface, ok := lookupInMap(convertMutexToMap(&this.domains), domainName)
	var domain *Domain
	var result int8
	if ok {
		domain = domainInterface.(*Domain)
		result = getResultFromDomain(domain)
		if time.Now().UnixNano()-domain.Time <= Timeout {
			return domain.Ip, result
		}
	} else {
		result = NotFound
	}

	if domain != nil && domain.Block {
		// If the domain is blocked, add it to the map so that the next lookup is faster
		this.domains.Store(domainName, &Domain{
			Name:  domainName,
			Time:  math.MaxInt64,
			Ip:    BlockedIp,
			Block: true,
		})
	}

	if domain == nil {
		return "", result
	}
	this.stats.CachedRequests++
	return domain.Ip, result
}

func (this *Server) getIp(domainName string) (string, error) {
	if this.checkBlock(domainName) {
		return BlockedIp, errors.New("blocked " + domainName)
	}
	address, result := this.lookupInMap(domainName)
	if result == Ok {
		return address, nil
	} else if result == Block {
		this.logger.Warnf("Blocking %s", domainName)
		this.stats.BlockedRequests++
		return BlockedIp, errors.New("blocked " + domainName)
	} else {
		this.logger.Infof("Fetching %s", domainName)
		if ips, err := this.resolver.LookupHost(strings.TrimRight(domainName, "."));
			err != nil || len(ips) == 0 {
			this.stats.FailedRequests++
			this.logger.Error(err)
			return "", err
		} else {
			answer := ips[0]
			go func() {
				// Add to cache
				this.domains.Store(domainName, &Domain{
					Ip:    answer.String(),
					Name:  domainName,
					Time:  time.Now().UnixNano(),
					Block: false,
				})
				this.stats.LookupRequests++
				//this.printAllHosts()
			}()
			return answer.String(), nil
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
		if r := recover(); r != nil {
			fmt.Println("Recovering from:", r)
			_ = w.Close()
		}
	}()
	msg := dns.Msg{}
	msg.SetReply(r)
	switch r.Question[0].Qtype {
	case dns.TypeA:
		msg.Authoritative = true
		for _, question := range msg.Question {
			domain := question.Name
			address, err := this.getIp(domain)
			defer func() {
				this.logger.Infof("Lookup %s in %s -> %s",
					domain, util.PrintTimeDiff(start), address)
			}()
			if err == nil {
				msg.Answer = append(msg.Answer, &dns.A{
					Hdr: dns.RR_Header{Name: domain, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 60},
					A:   net.ParseIP(address),
				})
			}
		}
	}
	_ = w.WriteMsg(&msg)
}

func (this *Server) startRest() {
	go func() {
		// Instantiate a new router
		gin.SetMode(gin.ReleaseMode)
		engine := gin.Default()
		engine.Use(gzip.Gzip(gzip.BestCompression))
		engine.Use(cors.Default())
		//engine.Use(logger.SetLogger())
		engine.GET("/", func(c *gin.Context) {
			this.domains.Range(func(key, value interface{}) bool {
				running := util.PrintTimeDiff(this.stats.Started)
				this.stats.Running = &running
				if key != nil && value != nil {
					this.stats.Domains[key.(string)] = value.(*Domain)
				}
				return true
			})
			c.JSON(200, this.stats)
		})

		if err := engine.Run(":9999"); err != nil {
			panic(err)
		} else {
			fmt.Printf("Successfully Started Server")
		}
	}()
}

func New(port int) (*dns.Server, *Server) {
	srv := &dns.Server{Addr: ":" + strconv.Itoa(port), Net: "udp"}
	config, err := readConfig()
	if err != nil {
		fmt.Println("Error Reading the Config", err.Error())
		return nil, nil
	}
	client := &Server{
		resolver:   dns_resolver.New(config.DnsServers),
		config:     config,
		printMutex: &sync.Mutex{},
		logger:     logging.GetLogger(),
		stats: Stats{
			LookupRequests: 0,
			CachedRequests: 0,
			Domains:        make(map[string]*Domain),
			Started:        time.Now().UnixNano(),
		},
	}
	convertMapToMutex(config.Hosts).
		Range(func(key, value interface{}) bool {
			client.domains.Store(key, &Domain{
				Name:  key.(string),
				Ip:    value.(string),
				Time:  math.MaxInt64,
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
					fmt.Println("Reloading Config File")
					newConfig, err := readConfig()
					if err != nil {
						fmt.Println("Error Reading the Config", err.Error())
					}
					client.config = newConfig
					client.resolver = dns_resolver.New(newConfig.DnsServers)
					convertMapToMutex(newConfig.Hosts).
						Range(func(key, value interface{}) bool {
							client.domains.Store(key, &Domain{
								Name:  key.(string),
								Ip:    value.(string),
								Time:  math.MaxInt64,
								Block: false,
							})
							return true
						})
				}
			}
		}()
	}
	return srv, client
}

func (this *Server) PreStart() {
	this.startRest()
	go func() {
		time.Sleep(time.Second)
		this.pullBlockList()
	}()
}

type Stats struct {
	Started         int64
	Running         *string            `json:"running"`
	LookupRequests  int64              `json:"lookupRequests"`
	CachedRequests  int64              `json:"cachedRequests"`
	BlockedRequests int64              `json:"blockedRequests"`
	FailedRequests  int64              `json:"failedRequests"`
	Domains         map[string]*Domain `json:"domains"`
}

type Config struct {
	Hosts      map[string]interface{} `json:"hosts"`
	Blocks     map[string]bool        `json:"blocks"`
	DnsServers []string               `json:"servers"`
}

type Domain struct {
	Name  string `json:"name"`
	Time  int64  `json:"time"`
	Ip    string `json:"ip"`
	Block bool   `json:"block"`
}
