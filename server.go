package main

import (
	"github.com/bogdanovich/dns_resolver"
	"github.com/miekg/dns"
	"github.com/sirupsen/logrus"
	"gitlab.com/kamackay/dns/logging"
	"log"
	"net"
	"strconv"
	"strings"
)

type handler struct {
	resolver *dns_resolver.DnsResolver
	domains  map[string]string
}

func (this *handler) getIp(domain string, logger *logrus.Logger) (string, error) {
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
			go func() {
				// Add to cache

			}()
			return ips[0].String(), nil
		}
	}
}

func (this *handler) ServeDNS(w dns.ResponseWriter, r *dns.Msg) {
	logger := logging.GetLogger()
	msg := dns.Msg{}
	msg.SetReply(r)
	switch r.Question[0].Qtype {
	case dns.TypeA:
		msg.Authoritative = true
		for _, question := range msg.Question {
			domain := question.Name
			address, err := this.getIp(domain, logger)
			logger.Infof("Request for domain '%s' --> %s", domain, address)
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

func main() {
	logger := logging.GetLogger()
	logger.Infof("Starting...")
	port := 53
	srv := &dns.Server{Addr: ":" + strconv.Itoa(port), Net: "udp"}
	srv.Handler = &handler{
		resolver: dns_resolver.New([]string{"1.1.1.1"}),
		domains: map[string]string{
			"cloudflare.com.": "1.1.1.1",
		},
	}
	if err := srv.ListenAndServe(); err != nil {
		log.Fatalf("Failed to set udp listener %s\n", err.Error())
	} else {
		logger.Infof("Started on Port %d", port)
	}
}
