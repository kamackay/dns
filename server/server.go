package server

import (
	"fmt"
	"github.com/gin-contrib/cors"
	"github.com/gin-contrib/gzip"
	"github.com/gin-gonic/gin"
	jsoniter "github.com/json-iterator/go"
	"github.com/miekg/dns"
	"gitlab.com/kamackay/dns/wildcard"
	"github.com/sirupsen/logrus"
	"gitlab.com/kamackay/dns/dns_resolver"
	"gitlab.com/kamackay/dns/logging"
	"io/ioutil"
	"net"
	"strconv"
	"strings"
)

type Server struct {
	resolver *dns_resolver.DnsResolver
	domains  map[string]string
	logger   *logrus.Logger
}

func (this *Server) getIp(domain string, logger *logrus.Logger) (string, error) {
	address, ok := this.domains[domain]
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
				this.domains[domain] = answer.String()
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
			c.JSON(200, this.domains)
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
		resolver: dns_resolver.New([]string{"1.1.1.1"}),
		domains: readConfig(),
	}
	srv.Handler = server
	server.startRest()
	return srv
}


func readConfig() map[string]string {
	hosts := map[string]string {}
	data, err := ioutil.ReadFile("/config.json")
	if err == nil {
		var config map[string]string
		err = jsoniter.Unmarshal(data, &config)
		if err == nil {
			for key, value := range config {
				hosts[key] = value
			}
		}
	}
	return hosts
}