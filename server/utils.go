package server

import "sync"

func convertMapToMutex(slice map[string]interface{}) *sync.Map {
	var mutexMap sync.Map
	for key, value := range slice {
		mutexMap.Store(key, value)
	}
	return &mutexMap
}