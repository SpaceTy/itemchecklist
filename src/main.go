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

	if _, err := os.Stat(settingsPath); os.IsNotExist(err) {
		if err := writeJSONFile(settingsPath, appSettings{}); err != nil {
			log.Fatalf("creating settings file: %v", err)
		}
		log.Printf("Created default %s", settingsPath)
	}

	if _, err := os.Stat(listsPath); os.IsNotExist(err) {
		if err := initializeListsStorage(); err != nil {
			log.Fatalf("creating lists file: %v", err)
		}
	}

	broker := newSSEBroker()
	go scheduleBackups()

	mux := newServerMux(broker)

	log.Printf("Server running on http://localhost:%d", port)
	if err := http.ListenAndServe(fmt.Sprintf(":%d", port), mux); err != nil {
		log.Fatalf("server error: %v", err)
	}
}

func initializeListsStorage() error {
	lists := []itemList{}

	legacyItems, err := readLegacyItems()
	if err != nil {
		return err
	}

	if len(legacyItems) > 0 {
		owner, ownerErr := findDefaultListOwner()
		if ownerErr != nil {
			return ownerErr
		}
		if owner != "" {
			lists = append(lists, itemList{
				ID:            newID("list"),
				Name:          "Imported Checklist",
				OwnerUsername: owner,
				Items:         legacyItems,
				CreatedAt:     nowRFC3339(),
				UpdatedAt:     nowRFC3339(),
			})
			log.Printf("Imported legacy %s into %s", itemsPath, listsPath)
		}
	}

	if err := writeJSONFile(listsPath, lists); err != nil {
		return err
	}

	log.Printf("Created %s", listsPath)
	return nil
}

func findDefaultListOwner() (string, error) {
	users, err := readUsers()
	if err != nil {
		return "", err
	}
	for _, u := range users {
		if u.Admin {
			return u.Username, nil
		}
	}
	if len(users) > 0 {
		return users[0].Username, nil
	}
	return "", nil
}
