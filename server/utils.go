package server

import (
	"encoding/json"
	"fmt"
	jsoniter "github.com/json-iterator/go"
	"github.com/sirupsen/logrus"
	"gitlab.com/kamackay/dns/dns_resolver"
	"gitlab.com/kamackay/dns/wildcard"
	"io/ioutil"
	"math"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	Timeout        = 360000
	BlockedIp      = "Blocked!"
	Ok        int8 = 0
	Block     int8 = 1
	NotFound  int8 = 2
)

type Server struct {
	resolver   *dns_resolver.DnsResolver
	domains    sync.Map
	config     *Config
	logger     *logrus.Logger
	printMutex *sync.Mutex
	stats      Stats
}

func convertMapToMutex(slice map[string]interface{}) *sync.Map {
	var mutexMap sync.Map
	for key, value := range slice {
		mutexMap.Store(key, value)
	}
	return &mutexMap
}

func convertMutexToMap(mutex *sync.Map) map[string]*Domain {
	slice := make(map[string]*Domain)
	mutex.Range(func(key, value interface{}) bool {
		slice[key.(string)] = value.(*Domain)
		return true
	})
	return slice
}

func (this *Server) pullBlockList() {
	var list []string
	err := getJson("https://api.keith.sh/ls.json", &list)
	if err == nil {
		this.logger.Infof("Pulled %d Servers to Block", len(list))
		for _, server := range list {
			name := fmt.Sprintf("*%s.", server)
			this.domains.Store(name, &Domain{
				Name:  name,
				Time:  math.MaxInt64,
				Ip:    BlockedIp,
				Block: true,
			})
		}
	}
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

func getJson(url string, target interface{}) error {
	client := &http.Client{Timeout: 10 * time.Second}
	r, err := client.Get(url)
	if err != nil {
		return err
	}
	defer r.Body.Close()

	return json.NewDecoder(r.Body).Decode(target)
}

func getResultFromDomain(domain *Domain) int8 {
	if domain.Block {
		return Block
	}
	return Ok
}

func lookupInMapAndUpdate(items map[string]*Domain, lookup string, updater func(*Domain)) (interface{}, bool) {
	exact, ok := items[lookup]
	if ok {
		updater(exact)
		return exact, true
	}
	for key, val := range items {
		if wildcard.Match(key, lookup) {
			updater(val)
			return val, true
		}
	}
	return nil, false
}
func lookupBoolInMap(items map[string]bool, lookup string) (*bool, bool) {
	exact, ok := items[lookup]
	if ok {
		return &exact, true
	}
	for key, val := range items {
		if wildcard.Match(key, lookup) {
			return &val, true
		}
	}
	return nil, false
}
