package main

import (
	"gitlab.com/kamackay/dns/logging"
	"github.com/miekg/dns"
	"log"
	"net"
	"strconv"
)

var domainsToAddresses map[string]string = map[string]string{
	"google.com.": "1.2.3.4",
	"cloudflare.com.": "1.1.1.1",
}

type handler struct{}
func (this *handler) ServeDNS(w dns.ResponseWriter, r *dns.Msg) {
	log := logging.GetLogger()
	msg := dns.Msg{}
	msg.SetReply(r)
	switch r.Question[0].Qtype {
	case dns.TypeA:
		msg.Authoritative = true
		domain := msg.Question[0].Name
		log.Infof("Request for name %s", domain)
		address, ok := domainsToAddresses[domain]
		if ok {
			msg.Answer = append(msg.Answer, &dns.A{
				Hdr: dns.RR_Header{ Name: domain, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 60 },
				A: net.ParseIP(address),
			})
		}
	}
	w.WriteMsg(&msg)
}

func main() {
	srv := &dns.Server{Addr: ":" + strconv.Itoa(53), Net: "udp"}
	srv.Handler = &handler{}
	if err := srv.ListenAndServe(); err != nil {
		log.Fatalf("Failed to set udp listener %s\n", err.Error())
	}
}
