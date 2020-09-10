// Package dns_resolver is a simple dns resolver
// based on miekg/dns
package dns_resolver

import (
	"errors"
	"fmt"
	jsoniter "github.com/json-iterator/go"
	"github.com/miekg/dns"
	"github.com/sirupsen/logrus"
	"gitlab.com/kamackay/dns/logging"
	"io/ioutil"
	"math/rand"
	"net"
	"net/http"
	"os"
	"strings"
	"time"
)

// DnsResolver represents a dns resolver
type DnsResolver struct {
	Servers    []string
	RetryTimes int
	r          *rand.Rand
	log        *logrus.Logger
	DohServer  *string
	httpClient *http.Client
}

type DnsResult struct {
	Ips    []Ip
	Server string
}

type Ip struct {
	Address string
	Ttl     uint32
	Name    string
}

// New initializes DnsResolver.
func New(servers []string, dohServer *string) *DnsResolver {
	for i := range servers {
		servers[i] = net.JoinHostPort(servers[i], "53")
	}

	return &DnsResolver{
		Servers:    servers,
		RetryTimes: len(servers) * 2,
		log:        logging.GetLogger(),
		DohServer:  dohServer,
		httpClient: &http.Client{},
		r:          rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

// NewFromResolvConf initializes DnsResolver from resolv.conf like file.
func NewFromResolvConf(path string) (*DnsResolver, error) {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return &DnsResolver{}, errors.New("no such file or directory: " + path)
	}
	config, err := dns.ClientConfigFromFile(path)
	servers := make([]string, 0)
	for _, ipAddress := range config.Servers {
		servers = append(servers, net.JoinHostPort(ipAddress, "53"))
	}
	return &DnsResolver{
		Servers:    servers,
		RetryTimes: len(servers) * 2,
		log:        logging.GetLogger(),
		r:          rand.New(rand.NewSource(time.Now().UnixNano())),
	}, err
}

// LookupHost returns IP addresses of provied host.
// In case of timeout retries query RetryTimes times.
func (r *DnsResolver) LookupHost(host string) (*DnsResult, error) {
	// Start by attempting a DNS-over-Https query
	dohResponse := r.lookupHostDoh(host)
	if dohResponse != nil {
		return dohResponse, nil
	}
	return r.lookupHost(host, r.RetryTimes)
}

func (r *DnsResolver) lookupHostDoh(host string) *DnsResult {
	if r.DohServer == nil {
		return nil
	}
	r.log.Debug("Attempting DohRequest")
	req, err := http.NewRequest(http.MethodGet,
		fmt.Sprintf("https://%s/dns-query?name=%s&type=A", *r.DohServer, host),
		nil)
	if err != nil {
		r.log.Warn("Error Building Request", err)
		return nil
	}
	req.Header.Add("accept", "application/dns-json")
	resp, err := r.httpClient.Do(req)
	if err != nil {
		r.log.Warn("Error Sending Request", err)
		return nil
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		r.log.Warn("Error Reading Request", err)
		return nil
	}
	var response DohResponse
	err = jsoniter.Unmarshal(body, &response)
	if err != nil {
		r.log.Warn("Error Parsing Request", err)
		return nil
	}
	ips := make([]Ip, 0)
	for _, answer := range response.Answer {
		ips = append(ips, Ip{
			Address: answer.Data,
			Ttl:     answer.TTL,
			Name:    answer.Name,
		})
	}
	return &DnsResult{
		Ips:    ips,
		Server: fmt.Sprintf("https://%s", *r.DohServer),
	}
}

func (r *DnsResolver) lookupHost(host string, triesLeft int) (*DnsResult, error) {
	m1 := new(dns.Msg)
	m1.Id = dns.Id()
	m1.RecursionDesired = true
	m1.Question = make([]dns.Question, 1)
	m1.Question[0] = dns.Question{
		Name:   dns.Fqdn(host),
		Qtype:  dns.TypeA,
		Qclass: dns.ClassINET,
	}
	server := r.Servers[(r.RetryTimes-triesLeft)%len(r.Servers)]
	in, err := dns.Exchange(m1, server)

	result := &DnsResult{
		Ips:    make([]Ip, 0),
		Server: server,
	}

	if err != nil {
		if strings.HasSuffix(err.Error(), "i/o timeout") && triesLeft > 0 {
			triesLeft--
			return r.lookupHost(host, triesLeft)
		}
		return result, err
	}

	if in != nil && in.Rcode != dns.RcodeSuccess {
		return result, errors.New(dns.RcodeToString[in.Rcode])
	}

	for _, record := range in.Answer {
		if t, ok := record.(*dns.A); ok {
			result.Ips = append(result.Ips, Ip{
				Address: t.A.String(),
				Ttl:     t.Hdr.Ttl,
				Name:    t.Hdr.Name,
			})
		}
	}
	return result, err
}

type DohResponse struct {
	Status   int           `json:"Status"`
	TC       bool          `json:"TC"`
	RD       bool          `json:"RD"`
	RA       bool          `json:"RA"`
	AD       bool          `json:"AD"`
	CD       bool          `json:"CD"`
	Question []DohQuestion `json:"Question"`
	Answer   []DohAnswer   `json:"Answer"`
}

type DohQuestion struct {
	Name string `json:"name"`
	Type int    `json:"type"`
}

type DohAnswer struct {
	Name string `json:"name"`
	Type uint16 `json:"type"`
	TTL  uint32 `json:"TTL"`
	Data string `json:"data"`
}
