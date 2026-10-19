//go:build !translate

package main

import (
	"fmt"
	"log"
	"net/http"
)

func main() {
	loadOrCreateSecret()

	if err := openDatabase(); err != nil {
		log.Fatalf("opening database: %v", err)
	}
	defer db.Close()

	broker := newSSEBroker()
	go scheduleChangePruning()

	mux := newServerMux(broker)

	log.Printf("Server running on http://localhost:%d", port)
	if err := http.ListenAndServe(fmt.Sprintf(":%d", port), mux); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
