package server

import (
	"errors"
	"fmt"
	"github.com/fsnotify/fsnotify"
	"github.com/gin-contrib/cors"
	"github.com/gin-contrib/gzip"
	"github.com/gin-gonic/gin"
	jsoniter "github.com/json-iterator/go"
	"github.com/miekg/dns"
	"github.com/sirupsen/logrus"
	"gitlab.com/kamackay/dns/dns_resolver"
	"gitlab.com/kamackay/dns/logging"
	"gitlab.com/kamackay/dns/util"
	"gitlab.com/kamackay/dns/wildcard"
	"io/ioutil"
	"math"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	TIMEOUT = 360000
)

type Server struct {
	resolver   *dns_resolver.DnsResolver
	domains    sync.Map
	config     *Config
	logger     *logrus.Logger
	printMutex *sync.Mutex
	blockList sync.Map
}

func (this *Server) lookupInMap(domainName string) (string, bool) {
	domainInterface, ok := this.domains.Load(domainName)
	var domain *Domain
	if ok {
		domain = domainInterface.(*Domain)
		if time.Now().UnixNano()-domain.Time <= TIMEOUT {
			return domain.Ip, ok
		}
	}
	this.domains.Range(func(key, value interface{}) bool {
		if wildcard.Match(key.(string), domainName) {
			domain = value.(*Domain)
			ok = true
			return false
		}
		return true
	})

	if domain == nil {
		return "", false
	}
	return domain.Ip, ok
}

func (this *Server) getIp(domainName string) (string, error) {
	if this.checkBlock(domainName) {
		return "", errors.New("blocked domainName")
	}
	address, ok := this.lookupInMap(domainName)
	if ok {
		return address, nil
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
					Ip:   answer.String(),
					Name: domainName,
					Time: time.Now().UnixNano(),
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

func New(port int) *dns.Server {
	srv := &dns.Server{Addr: ":" + strconv.Itoa(port), Net: "udp"}
	config, err := readConfig()
	if err != nil {
		fmt.Println("Error Reading the Config", err.Error())
		return nil
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
				Name: key.(string),
				Ip:   value.(string),
				Time: math.MaxInt64,
			})
			return true
		})
	srv.Handler = client
	client.startRest()
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
								Name: key.(string),
								Ip:   value.(string),
								Time: math.MaxInt64,
							})
							return true
						})
				}
			}
		}()
	}
	return srv
}

func (this *Server) printAllHosts() {
	this.printMutex.Lock()
	hosts := make([]string, 0)
	this.domains.Range(func(key, _ interface{}) bool {
		hosts = append(hosts, key.(string))
		return true
	})
	str := strings.Join(hosts, "\n")
	ioutil.WriteFile("/app/hosts.txt", []byte(str+"\n"), 0644)
	this.printMutex.Unlock()
}

func readConfig() (*Config, error) {
	data, err := ioutil.ReadFile("/config.json")
	var config Config
	if err == nil {
		err = jsoniter.Unmarshal(data, &config)
		if err != nil {
			return nil, err
		}
	}
	return &config, nil
}

type Config struct {
	Hosts   map[string]interface{} `json:"hosts"`
	Blocks  map[string]bool        `json:"blocks"`
	Servers []string               `json:"servers"`
}

type Domain struct {
	Name string `json:"name"`
	Time int64  `json:"time"`
	Ip   string `json:"ip"`
}
