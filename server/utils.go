package server

import (
	"encoding/json"
	"fmt"
	jsoniter "github.com/json-iterator/go"
	"io/ioutil"
	"math"
	"net/http"
	"strings"
	"sync"
	"time"
)

func convertMapToMutex(slice map[string]interface{}) *sync.Map {
	var mutexMap sync.Map
	for key, value := range slice {
		mutexMap.Store(key, value)
	}
	return &mutexMap
}

func (this *Server) pullBlockList() {
	var list []string
	err := getJson("https://api.keith.sh/ls.json", &list)
	if err == nil {
		for _, server := range list {
			name := fmt.Sprintf("*%s.", server)
			this.domains.Store(name, &Domain{
				Name:  name,
				Time:  math.MaxInt64,
				Ip:    "0.0.0.0",
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
