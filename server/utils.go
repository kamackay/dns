package server

import (
	"encoding/json"
	"fmt"
	jsoniter "github.com/json-iterator/go"
	"gitlab.com/kamackay/dns/wildcard"
	"io/ioutil"
	"math"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
)

const (
	Timeout        = 360000
	NanoConv       = 1_000_000
	BlockedIp      = "Blocked!"
	NoServer       = "127.0.0.1"
	Ok        int8 = 0
	Block     int8 = 1
	NotFound  int8 = 2
)

func (this *Server) addMetric(metric Metric) {
	this.stats.Metrics = append(this.stats.Metrics, metric)
}

func convertMapToMutex(slice map[string]interface{}) *sync.Map {
	var mutexMap sync.Map
	for key, value := range slice {
		mutexMap.Store(key, value)
	}
	return &mutexMap
}

func unique(slice []string) []string {
	keys := make(map[string]bool)
	list := make([]string, 0)
	for _, entry := range slice {
		if _, value := keys[entry]; !value {
			keys[entry] = true
			list = append(list, entry)
		}
	}
	return list
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
		this.log.Infof("Pulled %d Servers to Block", len(list))
		for _, server := range list {
			name := fmt.Sprintf("^(.*\\.)?%s\\.$", server)
			this.domains.Store(name, &Domain{
				Name:     name,
				Time:     time.Now().UnixNano(),
				Ip:       BlockedIp,
				Block:    true,
				Server:   NoServer,
				Requests: 0,
				Ttl:      math.MaxUint32,
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
		pattern := key
		if !strings.HasPrefix(pattern, "^") {
			// Format to be a regex for usage
			pattern = fmt.Sprintf("^%s$", pattern)
		}
		regex, err := regexp.Compile(pattern)
		if err != nil {
			continue
		}
		if regex.MatchString(lookup) {
			updater(val)
			//fmt.Printf("Found Match for %s: %s\n", lookup, key)
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

func getBlockedDomainObj(domainName string) *Domain {
	return &Domain{
		Ip:       BlockedIp,
		Name:     domainName,
		Time:     time.Now().UnixNano(),
		Block:    true,
		Requests: 1,
		Server:   NoServer,
	}
}

func getFailedDomainObj(domainName string) *Domain {
	return &Domain{
		Ip:       "",
		Name:     domainName,
		Time:     time.Now().UnixNano(),
		Block:    false,
		Requests: 1,
		Server:   NoServer,
	}
}
