package main

import (
	"github.com/bogdanovich/dns_resolver"
	"github.com/miekg/dns"
	"log"
	"gitlab.com/kamackay/dns/logging"
	"gitlab.com/kamackay/dns/server"
	"strconv"
)

func main() {
	logger := logging.GetLogger()
	logger.Infof("Starting...")
	port := 53
	srv := server.New()
	if err := srv.ListenAndServe(); err != nil {
		log.Fatalf("Failed to set udp listener %s\n", err.Error())
	} else {
		logger.Infof("Started on Port %d", port)
	}
}
