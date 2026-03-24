//go:build !translate

package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
)

func main() {
	loadOrCreateSecret()

	if err := os.MkdirAll(backupsDir, 0755); err != nil {
		log.Fatalf("creating backups dir: %v", err)
	}

	if _, err := os.Stat(usersPath); os.IsNotExist(err) {
		if err := writeJSONFile(usersPath, []user{}); err != nil {
			log.Fatalf("creating users file: %v", err)
		}
		log.Printf("Created empty %s", usersPath)
	}

	broker := newSSEBroker()
	go scheduleBackups()

	mux := newServerMux(broker)

	log.Printf("Server running on http://localhost:%d 🚀", port)
	if err := http.ListenAndServe(fmt.Sprintf(":%d", port), mux); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
