package server

import (
	"encoding/json"
	"net/http"
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
	err := getJson("https://api.keith.sh/ls.json", list)
	if err == nil {
		
	}
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
