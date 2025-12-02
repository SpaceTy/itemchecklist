package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	port       = 3001
	configPath = "config.json"
	itemsPath  = "items.json"
	backupsDir = "backups"
)

type config struct {
	Passwords []string `json:"passwords"`
}

type item struct {
	Name     string  `json:"name"`
	Target   int     `json:"target"`
	Gathered int     `json:"gathered"`
	Claims   []claim `json:"claims"`
}

type claim struct {
	Claimer    string `json:"claimer"`
	ClaimStart int    `json:"claim_start"`
	ClaimEnd   int    `json:"claim_end"`
}

type loginRequest struct {
	Password string `json:"password"`
}

type updateItemRequest struct {
	Name     string `json:"name"`
	Gathered int    `json:"gathered"`
}

type claimItemRequest struct {
	Name    string `json:"name"`
	Claimed int    `json:"claimed"`
	Claimer string `json:"claimer"`
}

type passwordRequest struct {
	Password string `json:"password"`
	Action   string `json:"action"`
}

type sseMessage struct {
	Type  string `json:"type"`
	Items []item `json:"items"`
}

type sseBroker struct {
	mu      sync.Mutex
	clients map[int]chan string
	nextID  int
}

func newSSEBroker() *sseBroker {
	return &sseBroker{
		clients: make(map[int]chan string),
	}
}

func (b *sseBroker) addClient(ch chan string) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	id := b.nextID
	b.nextID++
	b.clients[id] = ch
	return id
}

func (b *sseBroker) removeClient(id int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.clients, id)
}

func (b *sseBroker) broadcast(msg string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for id, ch := range b.clients {
		select {
		case ch <- msg:
		default:
			// Drop stale client that cannot keep up.
			delete(b.clients, id)
		}
	}
}

func main() {
	if err := os.MkdirAll(backupsDir, 0755); err != nil {
		log.Fatalf("creating backups dir: %v", err)
	}

	broker := newSSEBroker()

	go scheduleBackups()

	mux := http.NewServeMux()
	mux.HandleFunc("/api/login", loginHandler)
	mux.HandleFunc("/api/check-auth", requireAuth(checkAuthHandler))
	mux.HandleFunc("/api/items", requireAuth(getItemsHandler))
	mux.HandleFunc("/api/items/update", requireAuth(updateItemHandler(broker)))
	mux.HandleFunc("/api/items/claim", requireAuth(claimItemHandler(broker)))
	mux.HandleFunc("/api/config/passwords", requireAuth(passwordsHandler))
	mux.HandleFunc("/events", requireAuth(sseHandler(broker)))

	fileServer := http.FileServer(http.Dir("public"))
	mux.Handle("/", fileServer)

	log.Printf("Server running on http://localhost:%d 🚀", port)
	if err := http.ListenAndServe(fmt.Sprintf(":%d", port), mux); err != nil {
		log.Fatalf("server error: %v", err)
	}
}

func loginHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}

	cfg, err := readConfig()
	if err != nil {
		http.Error(w, "config not available", http.StatusInternalServerError)
		return
	}

	if !contains(cfg.Passwords, req.Password) {
		http.Error(w, `{"error":"Invalid password"}`, http.StatusUnauthorized)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "auth_token",
		Value:    req.Password,
		Path:     "/",
		MaxAge:   30 * 24 * 60 * 60,
		HttpOnly: false,
	})

	writeJSON(w, map[string]bool{"success": true})
}

func checkAuthHandler(w http.ResponseWriter, r *http.Request, _ config) {
	writeJSON(w, map[string]bool{"success": true})
}

func getItemsHandler(w http.ResponseWriter, r *http.Request, _ config) {
	items, err := readItems()
	if err != nil {
		http.Error(w, "could not read items", http.StatusInternalServerError)
		return
	}
	writeJSON(w, items)
}

func updateItemHandler(b *sseBroker) func(http.ResponseWriter, *http.Request, config) {
	return func(w http.ResponseWriter, r *http.Request, _ config) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req updateItemRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid body", http.StatusBadRequest)
			return
		}

		items, err := readItems()
		if err != nil {
			http.Error(w, "could not read items", http.StatusInternalServerError)
			return
		}

		updated := false
		for i := range items {
			if items[i].Name == req.Name {
				if req.Gathered < 0 {
					req.Gathered = 0
				}
				if req.Gathered > items[i].Target {
					req.Gathered = items[i].Target
				}

				items[i].Gathered = req.Gathered
				updated = true
				break
			}
		}

		if !updated {
			http.Error(w, `{"error":"Item not found"}`, http.StatusNotFound)
			return
		}

		if err := writeItems(items); err != nil {
			http.Error(w, "could not write items", http.StatusInternalServerError)
			return
		}

		payload, _ := json.Marshal(sseMessage{
			Type:  "update",
			Items: items,
		})
		b.broadcast(string(payload))

		writeJSON(w, map[string]bool{"success": true})
	}
}

func claimItemHandler(b *sseBroker) func(http.ResponseWriter, *http.Request, config) {
	return func(w http.ResponseWriter, r *http.Request, _ config) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req claimItemRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid body", http.StatusBadRequest)
			return
		}

		req.Claimer = strings.TrimSpace(req.Claimer)
		if req.Claimer == "" {
			http.Error(w, `{"error":"Claimer required"}`, http.StatusBadRequest)
			return
		}

		items, err := readItems()
		if err != nil {
			http.Error(w, "could not read items", http.StatusInternalServerError)
			return
		}

		updated := false
		for i := range items {
			if items[i].Name == req.Name {
				if req.Claimed < 0 {
					req.Claimed = 0
				}
				remaining := items[i].Target - items[i].Gathered
				if remaining < 0 {
					remaining = 0
				}
				if req.Claimed > remaining {
					req.Claimed = remaining
				}

				if req.Claimed == 0 {
					removeClaimByName(&items[i], req.Claimer)
				} else {
					existingClaim := getClaimByName(&items[i], req.Claimer)
					if existingClaim == nil {
						items[i].Claims = append(items[i].Claims, claim{
							Claimer:    req.Claimer,
							ClaimStart: items[i].Gathered,
							ClaimEnd:   items[i].Gathered + req.Claimed,
						})
					} else {
						existingClaim.ClaimStart = items[i].Gathered
						existingClaim.ClaimEnd = items[i].Gathered + req.Claimed
					}
				}
				updated = true
				break
			}
		}

		if !updated {
			http.Error(w, `{"error":"Item not found"}`, http.StatusNotFound)
			return
		}

		if err := writeItems(items); err != nil {
			http.Error(w, "could not write items", http.StatusInternalServerError)
			return
		}

		payload, _ := json.Marshal(sseMessage{
			Type:  "update",
			Items: items,
		})
		b.broadcast(string(payload))

		writeJSON(w, map[string]bool{"success": true})
	}
}

func removeClaimByName(item *item, name string) {
	var newClaims []claim
	for _, claim := range item.Claims {
		if claim.Claimer != name {
			newClaims = append(newClaims, claim)
		}
	}
	item.Claims = newClaims
}

func getClaimByName(item *item, name string) *claim {
	for i, claim := range item.Claims {
		if claim.Claimer == name {
			return &item.Claims[i]
		}
	}
	return nil
}

func passwordsHandler(w http.ResponseWriter, r *http.Request, cfg config) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, cfg.Passwords)
	case http.MethodPost:
		var req passwordRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid body", http.StatusBadRequest)
			return
		}

		switch strings.ToLower(req.Action) {
		case "add":
			if contains(cfg.Passwords, req.Password) {
				http.Error(w, `{"error":"Password already exists"}`, http.StatusBadRequest)
				return
			}
			cfg.Passwords = append(cfg.Passwords, req.Password)
			if err := writeConfig(cfg); err != nil {
				http.Error(w, "could not write config", http.StatusInternalServerError)
				return
			}
			writeJSON(w, map[string]any{"success": true, "passwords": cfg.Passwords})
		case "remove":
			if !contains(cfg.Passwords, req.Password) {
				http.Error(w, `{"error":"Password not found"}`, http.StatusNotFound)
				return
			}
			if len(cfg.Passwords) <= 1 {
				http.Error(w, `{"error":"Cannot remove the last password"}`, http.StatusBadRequest)
				return
			}
			cfg.Passwords = remove(cfg.Passwords, req.Password)
			if err := writeConfig(cfg); err != nil {
				http.Error(w, "could not write config", http.StatusInternalServerError)
				return
			}
			writeJSON(w, map[string]any{"success": true, "passwords": cfg.Passwords})
		default:
			http.Error(w, `{"error":"Invalid action"}`, http.StatusBadRequest)
		}
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func sseHandler(b *sseBroker) func(http.ResponseWriter, *http.Request, config) {
	return func(w http.ResponseWriter, r *http.Request, cfg config) {
		if !isAuthorized(r, cfg) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming unsupported", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")

		ch := make(chan string, 4)
		id := b.addClient(ch)
		defer b.removeClient(id)

		// Flush headers immediately so the client establishes the stream, even before any events.
		if _, err := fmt.Fprint(w, ": connected\n\n"); err == nil {
			flusher.Flush()
		}

		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case msg := <-ch:
				if _, err := fmt.Fprintf(w, "data: %s\n\n", msg); err != nil {
					return
				}
				flusher.Flush()
			case <-ticker.C:
				if _, err := fmt.Fprint(w, ": keep-alive\n\n"); err != nil {
					return
				}
				flusher.Flush()
			case <-r.Context().Done():
				return
			}
		}
	}
}

func requireAuth(next func(http.ResponseWriter, *http.Request, config)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cfg, err := readConfig()
		if err != nil {
			http.Error(w, "config not available", http.StatusInternalServerError)
			return
		}

		if !isAuthorized(r, cfg) {
			http.Error(w, `{"error":"Unauthorized"}`, http.StatusUnauthorized)
			return
		}

		next(w, r, cfg)
	}
}

func isAuthorized(r *http.Request, cfg config) bool {
	cookie, err := r.Cookie("auth_token")
	if err != nil {
		return false
	}
	return contains(cfg.Passwords, cookie.Value)
}

func readConfig() (config, error) {
	var cfg config
	if err := readJSONFile(configPath, &cfg); err != nil {
		return cfg, err
	}
	return cfg, nil
}

func writeConfig(cfg config) error {
	return writeJSONFile(configPath, cfg)
}

func readItems() ([]item, error) {
	var items []item
	if _, err := os.Stat(itemsPath); err != nil {
		if os.IsNotExist(err) {
			return []item{}, nil
		}
		return nil, err
	}
	if err := readJSONFile(itemsPath, &items); err != nil {
		return nil, err
	}
	return items, nil
}

func writeItems(items []item) error {
	return writeJSONFile(itemsPath, items)
}

func readJSONFile(path string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}

func writeJSONFile(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func contains(list []string, value string) bool {
	for _, v := range list {
		if v == value {
			return true
		}
	}
	return false
}

func remove(list []string, value string) []string {
	out := list[:0]
	for _, v := range list {
		if v != value {
			out = append(out, v)
		}
	}
	return out
}

func scheduleBackups() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		performBackup()
		<-ticker.C
	}
}

func performBackup() {
	items, err := readItems()
	if err != nil || len(items) == 0 {
		return
	}

	if err := os.MkdirAll(backupsDir, 0755); err != nil {
		log.Printf("backup mkdir error: %v", err)
		return
	}

	timestamp := time.Now().UTC().Format("2006-01-02T15-04-05Z07-00")
	filename := fmt.Sprintf("items-%s.json", timestamp)
	path := filepath.Join(backupsDir, filename)

	if err := writeJSONFile(path, items); err != nil {
		log.Printf("backup write error: %v", err)
		return
	}

	cleanupBackups()
}

func cleanupBackups() {
	entries, err := os.ReadDir(backupsDir)
	if err != nil {
		return
	}

	var backups []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasPrefix(e.Name(), "items-") && strings.HasSuffix(e.Name(), ".json") {
			backups = append(backups, e.Name())
		}
	}

	sort.Strings(backups)
	for len(backups) > 50 {
		toDelete := backups[0]
		backups = backups[1:]
		_ = os.Remove(filepath.Join(backupsDir, toDelete))
	}
}
