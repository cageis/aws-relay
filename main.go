package main

import (
	"log"
	"net/http"
	"os"

	"aws-relay/internal/dashboard"
	"aws-relay/internal/proxy"
	"aws-relay/internal/store"
)

func main() {
	upstreamURL := os.Getenv("AWS_UPSTREAM_URL")
	if upstreamURL == "" {
		upstreamURL = "http://localstack:4566"
	}

	listenAddr := os.Getenv("AWS_RELAY_ADDR")
	if listenAddr == "" {
		listenAddr = ":4567"
	}

	dashboardAddr := os.Getenv("AWS_DASHBOARD_ADDR")
	if dashboardAddr == "" {
		dashboardAddr = ":4568"
	}

	messageStore := store.New()
	sqsProxy := proxy.New(upstreamURL, messageStore)
	dashboardServer := dashboard.New(messageStore)

	// Start dashboard server in background
	go func() {
		log.Printf("Dashboard listening on %s", dashboardAddr)
		if err := http.ListenAndServe(dashboardAddr, dashboardServer); err != nil {
			log.Fatalf("Dashboard server error: %v", err)
		}
	}()

	// Start proxy
	log.Printf("AWS Relay listening on %s -> %s", listenAddr, upstreamURL)
	if err := http.ListenAndServe(listenAddr, sqsProxy); err != nil {
		log.Fatalf("Proxy server error: %v", err)
	}
}
