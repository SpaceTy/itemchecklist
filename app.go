package main

import (
	"net/http"
	"time"
)

const (
	port       = 3001
	usersPath  = "users.json"
	itemsPath  = "items.json"
	backupsDir = "backups"
	secretPath = "secret.key"
	tokenTTL   = 30 * 24 * time.Hour
)

func newServerMux(broker *sseBroker) *http.ServeMux {
	mux := http.NewServeMux()

	register := func(pattern string, h http.HandlerFunc) {
		mux.HandleFunc(pattern, h)
		mux.HandleFunc("/itemchecklist"+pattern, h)
	}

	register("/api/login", loginHandler)
	register("/api/register", registerHandler)
	register("/api/logout", logoutHandler)

	register("/api/check-auth", requireAuth(checkAuthHandler))
	register("/api/items", requireAuth(getItemsHandler))
	register("/api/items/update", requireAuth(updateItemHandler(broker)))
	register("/api/items/claim", requireAuth(claimItemHandler(broker)))
	register("/events", requireAuth(sseHandler(broker)))

	register("/api/admin/users", requireAdmin(adminUsersHandler))

	mux.HandleFunc("/", staticFileHandler)
	return mux
}
