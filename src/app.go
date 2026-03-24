package main

import (
	"net/http"
	"time"
)

const (
	port         = 3001
	usersPath    = "users.json"
	settingsPath = "settings.json"
	itemsPath    = "items.json"
	listsPath    = "lists.json"
	backupsDir   = "backups"
	secretPath   = "secret.key"
	tokenTTL     = 30 * 24 * time.Hour
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
	register("/api/lists", requireAuth(listsCollectionHandler))
	register("/api/lists/import", requireAuth(importListHandler))
	register("/api/lists/join", requireAuth(joinListHandler))
	register("/api/lists/", requireAuth(listResourceHandler(broker)))
	register("/events", requireAuth(sseHandler(broker)))

	register("/api/admin/users", requireAdmin(adminUsersHandler(broker)))
	register("/api/admin/settings", requireAdmin(adminSettingsHandler))

	mux.HandleFunc("/", staticFileHandler)
	return mux
}
