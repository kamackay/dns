package server

import (
	"github.com/sirupsen/logrus"
	"gitlab.com/kamackay/dns/dns_resolver"
	"sync"
)

type Server struct {
	resolver   *dns_resolver.DnsResolver
	domains    sync.Map
	config     *Config
	log        *logrus.Logger
	printMutex *sync.Mutex
	stats      Stats
}

type Stats struct {
	Started         int64
	Running         *string   `json:"running"`
	LookupRequests  int64     `json:"lookupRequests"`
	CachedRequests  int64     `json:"cachedRequests"`
	BlockedRequests int64     `json:"blockedRequests"`
	FailedRequests  int64     `json:"failedRequests"`
	Domains         []*Domain `json:"domains"`
	FailedDomains   []string  `json:"failedDomains"`
	Metrics         []Metric  `json:"metrics"`
}

type Config struct {
	Hosts      map[string]interface{} `json:"hosts"`
	Blocks     map[string]bool        `json:"blocks"`
	DnsServers []string               `json:"servers"`
	DohServer  *string                `json:"dohServer"`
}

type Domain struct {
	Name     string `json:"name"`
	Time     int64  `json:"time"`
	Ip       string `json:"ip"`
	Block    bool   `json:"block"`
	Requests int64  `json:"requests"`
	Server   string `json:"server"`
	Ttl      uint32 `json:"ttl"`
}

type Metric struct {
	MetricType string `json:"type"`
	Time       int64  `json:"time"`
	Ip         string `json:"ip"`
	Server     string `json:"server"`
	Blocked    bool   `json:"blocked"`
	Domain     string `json:"domain"`
}
