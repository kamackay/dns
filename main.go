package main

import (
	"gitlab.com/kamackay/dns/logging"
	"gitlab.com/kamackay/dns/server"
	"log"
)

func main() {
	logger := logging.GetLogger()
	logger.Infof("Starting...")
	port := 53
	srv := server.New(port)
	if err := srv.ListenAndServe(); err != nil {
		log.Fatalf("Failed to set udp listener %s\n", err.Error())
	} else {
		logger.Infof("Started on Port %d", port)
	}
}
