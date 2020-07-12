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
	"gitlab.com/kamackay/dns/wildcard"
	"gitlab.com/kamackay/dns/util"
	"io/ioutil"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Server struct {
	resolver   *dns_resolver.DnsResolver
	domains    sync.Map
	config     *Config
	logger     *logrus.Logger
	printMutex *sync.Mutex
}

func (this *Server) lookupInMap(domain string) (string, bool) {
	address, ok := this.domains.Load(domain)
	if ok {
		return address.(string), ok
	} else {
		this.domains.Range(func(key, value interface{}) bool {
			if wildcard.Match(key.(string), domain) {
				address = value
				ok = true
				return false
			}
			return true
		})
	}
	if address == nil {
		return "", false
	}
	return address.(string), ok
}

func (this *Server) getIp(domain string) (string, error) {
	if this.checkBlock(domain) {
		return "", errors.New("blocked domain")
	}
	address, ok := this.lookupInMap(domain)
	if ok {
		return address, nil
	} else {
		this.logger.Infof("Looking up %s", domain)
		if ips, err := this.resolver.LookupHost(strings.TrimRight(domain, "."));
			err != nil || len(ips) == 0 {
			this.logger.Error(err)
			return "", err
		} else {
			answer := ips[0]
			go func() {
				// Add to cache
				this.domains.Store(domain, answer.String())
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
			fmt.Println("Recovered in f", r)
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
			address, err := this.getIp(domain)
			defer func() {
				this.logger.Infof("{%s} Request for domain '%s' -> %s",
					util.PrintTimeDiff(start),
					domain, address)
			}()
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
			hosts := map[string]string{}
			this.domains.Range(func(key, value interface{}) bool {
				if key != nil && value != nil {
					hosts[key.(string)] = value.(string)
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
		resolver:   dns_resolver.New([]string{"1.1.1.1"}),
		config:     config,
		printMutex: &sync.Mutex{},
		logger:     logging.GetLogger(),
	}
	convertMapToMutex(config.Hosts).
		Range(func(key, value interface{}) bool {
			client.domains.Store(key, value)
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
					convertMapToMutex(newConfig.Hosts).
						Range(func(key, value interface{}) bool {
							client.domains.Store(key, value)
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
	Hosts  map[string]interface{} `json:"hosts"`
	Blocks map[string]bool        `json:"blocks"`
}
