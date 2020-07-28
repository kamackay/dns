package server

import (
	"errors"
	"fmt"
	"github.com/fsnotify/fsnotify"
	"github.com/gin-contrib/cors"
	"github.com/gin-contrib/gzip"
	"github.com/gin-gonic/gin"
	"github.com/miekg/dns"
	"github.com/sirupsen/logrus"
	"gitlab.com/kamackay/dns/dns_resolver"
	"gitlab.com/kamackay/dns/logging"
	"gitlab.com/kamackay/dns/util"
	"gitlab.com/kamackay/dns/wildcard"
	"math"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	Timeout       = 360000
	Ok       int8 = 0
	Block    int8 = 1
	NotFound int8 = 2
)

type Server struct {
	resolver   *dns_resolver.DnsResolver
	domains    sync.Map
	config     *Config
	logger     *logrus.Logger
	printMutex *sync.Mutex
}

func (this *Server) lookupInMap(domainName string) (string, int8) {
	domainInterface, ok := this.domains.Load(domainName)
	var domain *Domain
	var result int8
	if ok {
		result = Ok
		domain = domainInterface.(*Domain)
		if time.Now().UnixNano()-domain.Time <= Timeout {
			return domain.Ip, result
		}
	} else {
		result = NotFound
	}
	this.domains.Range(func(key, value interface{}) bool {
		if wildcard.Match(key.(string), domainName) {
			domain = value.(*Domain)
			if domain.Block {
				result = Block
			} else {
				result = Ok
			}
			return false
		}
		return true
	})

	if domain == nil {
		return "", result
	}
	return domain.Ip, result
}

func (this *Server) getIp(domainName string) (string, error) {
	if this.checkBlock(domainName) {
		return "", errors.New("blocked domainName")
	}
	address, result := this.lookupInMap(domainName)
	if result == Ok {
		return address, nil
	} else if result == Block {
		this.logger.Warnf("Blocking %s", domainName)
		return "", errors.New("blocked")
	} else {
		this.logger.Infof("Looking up %s", domainName)
		if ips, err := this.resolver.LookupHost(strings.TrimRight(domainName, "."));
			err != nil || len(ips) == 0 {
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
				this.printAllHosts()
			}()
			return answer.String(), nil
		}
	}
}

// Return True if Blocked
func (this *Server) checkBlock(domain string) bool {
	for key, val := range this.config.Blocks {
		if wildcard.Match(key, domain) && val {
			return true
		}
	}
	return false
}

func (this *Server) ServeDNS(w dns.ResponseWriter, r *dns.Msg) {
	start := time.Now().UnixNano()
	defer func() {
		if r := recover(); r != nil {
			fmt.Println("Recovering from:", r)
			w.Close()
		}
	}()
	msg := dns.Msg{}
	msg.SetReply(r)
	switch r.Question[0].Qtype {
	case dns.TypeA:
		msg.Authoritative = true
		for _, question := range msg.Question {
			domain := question.Name
			defer func() {
				this.logger.Infof("Processed %s in %s",
					domain, util.PrintTimeDiff(start))
			}()
			address, err := this.getIp(domain)
			this.logger.Infof("Request for domain '%s' -> %s",
				domain, address)
			if err == nil {
				msg.Answer = append(msg.Answer, &dns.A{
					Hdr: dns.RR_Header{Name: domain, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 60},
					A:   net.ParseIP(address),
				})
			}
		}
	}
	w.WriteMsg(&msg)
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
			hosts := map[string]*Domain{}
			this.domains.Range(func(key, value interface{}) bool {
				if key != nil && value != nil {
					hosts[key.(string)] = value.(*Domain)
				}
				return true
			})
			c.JSON(200, hosts)
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
		resolver:   dns_resolver.New(config.Servers),
		config:     config,
		printMutex: &sync.Mutex{},
		logger:     logging.GetLogger(),
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
					client.resolver = dns_resolver.New(newConfig.Servers)
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

type Config struct {
	Hosts   map[string]interface{} `json:"hosts"`
	Blocks  map[string]bool        `json:"blocks"`
	Servers []string               `json:"servers"`
}

type Domain struct {
	Name  string `json:"name"`
	Time  int64  `json:"time"`
	Ip    string `json:"ip"`
	Block bool   `json:"block"`
}
