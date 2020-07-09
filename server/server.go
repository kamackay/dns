package server

import (
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
	"io/ioutil"
	"net"
	"strconv"
	"strings"
	"sync"
)

type Server struct {
	resolver   *dns_resolver.DnsResolver
	domains    sync.Map
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

func (this *Server) getIp(domain string, logger *logrus.Logger) (string, error) {
	address, ok := this.lookupInMap(domain)
	if ok {
		return address, nil
	} else {
		logger.Infof("Looking up %s", domain)
		if ips, err := this.resolver.LookupHost(strings.TrimRight(domain, "."));
			err != nil || len(ips) == 0 {
			logger.Error(err)
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

func (this *Server) ServeDNS(w dns.ResponseWriter, r *dns.Msg) {
	logger := logging.GetLogger()
	msg := dns.Msg{}
	msg.SetReply(r)
	switch r.Question[0].Qtype {
	case dns.TypeA:
		msg.Authoritative = true
		for _, question := range msg.Question {
			domain := question.Name
			address, err := this.getIp(domain, logger)
			logger.Infof("Request for domain '%s' -> %s", domain, address)
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
	server := &Server{
		resolver:   dns_resolver.New([]string{"1.1.1.1"}),
		domains:    *readConfig(),
		printMutex: &sync.Mutex{},
	}
	srv.Handler = server
	server.startRest()
	watcher, err := fsnotify.NewWatcher()
	if err == nil && watcher.Add("/config.json") == nil {
		go func() {
			for {
				select {
				// watch for events
				case _ = <-watcher.Events:
					fmt.Println("Reloading Config File")
					readConfig().Range(func(key, value interface{}) bool {
						server.domains.Store(key, value)
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
 	ioutil.WriteFile("/app/hosts.txt", []byte(str + "\n"), 0644)
 	this.printMutex.Unlock()
}

func readConfig() *sync.Map {
	var hosts sync.Map
	data, err := ioutil.ReadFile("/config.json")
	if err == nil {
		var config map[string]interface{}
		err = jsoniter.Unmarshal(data, &config)
		if err == nil {
			for key, value := range config["hosts"].(map[string]interface{}) {
				hosts.Store(key, value)
			}
		}
	}
	return &hosts
}
