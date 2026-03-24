//go:build !translate

package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
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

	"golang.org/x/crypto/bcrypt"
)

const (
	port       = 3001
	usersPath  = "users.json"
	itemsPath  = "items.json"
	backupsDir = "backups"
)

// ── User types ───────────────────────────────────────────────────────────────

type user struct {
	Username     string `json:"username"`
	PasswordHash string `json:"password_hash"`
	Admin        bool   `json:"admin"`
}

// userInfo is the safe public view of a user (no password hash).
type userInfo struct {
	Username string `json:"username"`
	Admin    bool   `json:"admin"`
}

// ── Request types ─────────────────────────────────────────────────────────────

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type registerRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type adminUserRequest struct {
	Username string `json:"username"`
	Action   string `json:"action"` // "delete" | "toggle_admin"
}

// ── Item types ────────────────────────────────────────────────────────────────

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

type updateItemRequest struct {
	Name     string `json:"name"`
	Gathered int    `json:"gathered"`
}

type claimItemRequest struct {
	Name    string `json:"name"`
	Claimed int    `json:"claimed"`
	// Claimer is read from the session, not the request body.
}

// ── SSE types ─────────────────────────────────────────────────────────────────

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
	return &sseBroker{clients: make(map[int]chan string)}
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
			delete(b.clients, id)
		}
	}
}

// ── Session management ────────────────────────────────────────────────────────

var (
	sessionsMu sync.RWMutex
	sessions   = make(map[string]string) // token → username
)

func generateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func setSession(token, username string) {
	sessionsMu.Lock()
	defer sessionsMu.Unlock()
	sessions[token] = username
}

func getSession(token string) (string, bool) {
	sessionsMu.RLock()
	defer sessionsMu.RUnlock()
	u, ok := sessions[token]
	return u, ok
}

func deleteSession(token string) {
	sessionsMu.Lock()
	defer sessionsMu.Unlock()
	delete(sessions, token)
}

// ── Context key ───────────────────────────────────────────────────────────────

type contextKey string

const usernameKey contextKey = "username"

// ── Auth middleware ───────────────────────────────────────────────────────────

func requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("auth_token")
		if err != nil {
			http.Error(w, `{"error":"Unauthorized"}`, http.StatusUnauthorized)
			return
		}
		username, ok := getSession(cookie.Value)
		if !ok {
			http.Error(w, `{"error":"Unauthorized"}`, http.StatusUnauthorized)
			return
		}
		ctx := context.WithValue(r.Context(), usernameKey, username)
		next(w, r.WithContext(ctx))
	}
}

func requireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return requireAuth(func(w http.ResponseWriter, r *http.Request) {
		username := r.Context().Value(usernameKey).(string)
		u, err := findUser(username)
		if err != nil || u == nil || !u.Admin {
			http.Error(w, `{"error":"Forbidden"}`, http.StatusForbidden)
			return
		}
		next(w, r)
	})
}

// ── main ──────────────────────────────────────────────────────────────────────

func main() {
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

	mux := http.NewServeMux()

	register := func(pattern string, h http.HandlerFunc) {
		mux.HandleFunc(pattern, h)
		mux.HandleFunc("/itemchecklist"+pattern, h)
	}

	// Public
	register("/api/login", loginHandler)
	register("/api/register", registerHandler)
	register("/api/logout", logoutHandler)

	// Authenticated
	register("/api/check-auth", requireAuth(checkAuthHandler))
	register("/api/items", requireAuth(getItemsHandler))
	register("/api/items/update", requireAuth(updateItemHandler(broker)))
	register("/api/items/claim", requireAuth(claimItemHandler(broker)))
	register("/events", requireAuth(sseHandler(broker)))

	// Admin only
	register("/api/admin/users", requireAdmin(adminUsersHandler))

	mux.HandleFunc("/", staticFileHandler)

	log.Printf("Server running on http://localhost:%d 🚀", port)
	if err := http.ListenAndServe(fmt.Sprintf(":%d", port), mux); err != nil {
		log.Fatalf("server error: %v", err)
	}
}

// ── Static file handler ───────────────────────────────────────────────────────

func staticFileHandler(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	path = strings.TrimPrefix(path, "/itemchecklist")

	if path == "/" || path == "" {
		path = "/index.html"
	}

	filePath := filepath.Join("public", path)
	data, err := os.ReadFile(filePath)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	contentType := "text/plain"
	switch filepath.Ext(filePath) {
	case ".html":
		contentType = "text/html; charset=utf-8"
	case ".css":
		contentType = "text/css; charset=utf-8"
	case ".js":
		contentType = "application/javascript; charset=utf-8"
	case ".json":
		contentType = "application/json; charset=utf-8"
	case ".png":
		contentType = "image/png"
	case ".jpg", ".jpeg":
		contentType = "image/jpeg"
	case ".gif":
		contentType = "image/gif"
	case ".svg":
		contentType = "image/svg+xml"
	case ".ico":
		contentType = "image/x-icon"
	case ".woff":
		contentType = "font/woff"
	case ".woff2":
		contentType = "font/woff2"
	case ".ttf":
		contentType = "font/ttf"
	case ".eot":
		contentType = "application/vnd.ms-fontobject"
	}

	w.Header().Set("Content-Type", contentType)
	w.Write(data)
}

// ── Auth handlers ─────────────────────────────────────────────────────────────

func loginHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"Invalid body"}`, http.StatusBadRequest)
		return
	}

	req.Username = strings.TrimSpace(req.Username)
	if req.Username == "" || req.Password == "" {
		http.Error(w, `{"error":"Username and password required"}`, http.StatusBadRequest)
		return
	}

	u, err := findUser(req.Username)
	if err != nil {
		http.Error(w, `{"error":"Server error"}`, http.StatusInternalServerError)
		return
	}
	if u == nil || bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(req.Password)) != nil {
		http.Error(w, `{"error":"Invalid username or password"}`, http.StatusUnauthorized)
		return
	}

	token, err := generateToken()
	if err != nil {
		http.Error(w, `{"error":"Server error"}`, http.StatusInternalServerError)
		return
	}
	setSession(token, u.Username)

	http.SetCookie(w, &http.Cookie{
		Name:     "auth_token",
		Value:    token,
		Path:     "/itemchecklist/",
		MaxAge:   30 * 24 * 60 * 60,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})

	writeJSON(w, map[string]any{"success": true, "username": u.Username, "admin": u.Admin})
}

func registerHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req registerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"Invalid body"}`, http.StatusBadRequest)
		return
	}

	req.Username = strings.TrimSpace(req.Username)

	if req.Username == "" {
		http.Error(w, `{"error":"Username is required"}`, http.StatusBadRequest)
		return
	}
	if strings.ContainsAny(req.Username, " \t\n\r") {
		http.Error(w, `{"error":"Username must not contain spaces"}`, http.StatusBadRequest)
		return
	}
	if len(req.Username) > 32 {
		http.Error(w, `{"error":"Username must be 32 characters or fewer"}`, http.StatusBadRequest)
		return
	}
	if len(req.Password) < 6 {
		http.Error(w, `{"error":"Password must be at least 6 characters"}`, http.StatusBadRequest)
		return
	}

	users, err := readUsers()
	if err != nil {
		http.Error(w, `{"error":"Server error"}`, http.StatusInternalServerError)
		return
	}
	for _, u := range users {
		if strings.EqualFold(u.Username, req.Username) {
			http.Error(w, `{"error":"Username already taken"}`, http.StatusConflict)
			return
		}
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		http.Error(w, `{"error":"Server error"}`, http.StatusInternalServerError)
		return
	}

	newUser := user{Username: req.Username, PasswordHash: string(hash), Admin: false}
	users = append(users, newUser)
	if err := writeUsers(users); err != nil {
		http.Error(w, `{"error":"Server error"}`, http.StatusInternalServerError)
		return
	}

	// Auto-login after registration.
	token, err := generateToken()
	if err != nil {
		http.Error(w, `{"error":"Server error"}`, http.StatusInternalServerError)
		return
	}
	setSession(token, newUser.Username)

	http.SetCookie(w, &http.Cookie{
		Name:     "auth_token",
		Value:    token,
		Path:     "/itemchecklist/",
		MaxAge:   30 * 24 * 60 * 60,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})

	writeJSON(w, map[string]any{"success": true, "username": newUser.Username, "admin": false})
}

func logoutHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if cookie, err := r.Cookie("auth_token"); err == nil {
		deleteSession(cookie.Value)
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "auth_token",
		Value:    "",
		Path:     "/itemchecklist/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})

	writeJSON(w, map[string]bool{"success": true})
}

func checkAuthHandler(w http.ResponseWriter, r *http.Request) {
	username := r.Context().Value(usernameKey).(string)
	u, err := findUser(username)
	isAdmin := err == nil && u != nil && u.Admin
	writeJSON(w, map[string]any{
		"success":  true,
		"username": username,
		"admin":    isAdmin,
	})
}

// ── Admin handlers ────────────────────────────────────────────────────────────

func adminUsersHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		users, err := readUsers()
		if err != nil {
			http.Error(w, `{"error":"Server error"}`, http.StatusInternalServerError)
			return
		}
		info := make([]userInfo, len(users))
		for i, u := range users {
			info[i] = userInfo{Username: u.Username, Admin: u.Admin}
		}
		writeJSON(w, info)

	case http.MethodPost:
		var req adminUserRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error":"Invalid body"}`, http.StatusBadRequest)
			return
		}

		callerUsername := r.Context().Value(usernameKey).(string)

		users, err := readUsers()
		if err != nil {
			http.Error(w, `{"error":"Server error"}`, http.StatusInternalServerError)
			return
		}

		idx := -1
		for i, u := range users {
			if u.Username == req.Username {
				idx = i
				break
			}
		}
		if idx == -1 {
			http.Error(w, `{"error":"User not found"}`, http.StatusNotFound)
			return
		}

		switch req.Action {
		case "delete":
			if users[idx].Username == callerUsername {
				http.Error(w, `{"error":"Cannot delete your own account"}`, http.StatusBadRequest)
				return
			}
			users = append(users[:idx], users[idx+1:]...)
		case "toggle_admin":
			users[idx].Admin = !users[idx].Admin
		default:
			http.Error(w, `{"error":"Unknown action"}`, http.StatusBadRequest)
			return
		}

		if err := writeUsers(users); err != nil {
			http.Error(w, `{"error":"Server error"}`, http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]bool{"success": true})

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// ── Item handlers ─────────────────────────────────────────────────────────────

func getItemsHandler(w http.ResponseWriter, r *http.Request) {
	items, err := readItems()
	if err != nil {
		http.Error(w, `{"error":"Could not read items"}`, http.StatusInternalServerError)
		return
	}
	writeJSON(w, items)
}

func updateItemHandler(b *sseBroker) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req updateItemRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error":"Invalid body"}`, http.StatusBadRequest)
			return
		}

		items, err := readItems()
		if err != nil {
			http.Error(w, `{"error":"Could not read items"}`, http.StatusInternalServerError)
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
			http.Error(w, `{"error":"Could not write items"}`, http.StatusInternalServerError)
			return
		}

		payload, _ := json.Marshal(sseMessage{Type: "update", Items: items})
		b.broadcast(string(payload))

		writeJSON(w, map[string]bool{"success": true})
	}
}

func claimItemHandler(b *sseBroker) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req claimItemRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error":"Invalid body"}`, http.StatusBadRequest)
			return
		}

		// Claimer is always the authenticated user.
		claimer := r.Context().Value(usernameKey).(string)

		items, err := readItems()
		if err != nil {
			http.Error(w, `{"error":"Could not read items"}`, http.StatusInternalServerError)
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
					removeClaimByName(&items[i], claimer)
				} else {
					existingClaim := getClaimByName(&items[i], claimer)
					if existingClaim == nil {
						items[i].Claims = append(items[i].Claims, claim{
							Claimer:    claimer,
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
			http.Error(w, `{"error":"Could not write items"}`, http.StatusInternalServerError)
			return
		}

		payload, _ := json.Marshal(sseMessage{Type: "update", Items: items})
		b.broadcast(string(payload))

		writeJSON(w, map[string]bool{"success": true})
	}
}

func removeClaimByName(it *item, name string) {
	var newClaims []claim
	for _, c := range it.Claims {
		if c.Claimer != name {
			newClaims = append(newClaims, c)
		}
	}
	it.Claims = newClaims
}

func getClaimByName(it *item, name string) *claim {
	for i := range it.Claims {
		if it.Claims[i].Claimer == name {
			return &it.Claims[i]
		}
	}
	return nil
}

// ── SSE handler ───────────────────────────────────────────────────────────────

func sseHandler(b *sseBroker) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
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

// ── User file helpers ─────────────────────────────────────────────────────────

func readUsers() ([]user, error) {
	var users []user
	if _, err := os.Stat(usersPath); os.IsNotExist(err) {
		return []user{}, nil
	}
	if err := readJSONFile(usersPath, &users); err != nil {
		return nil, err
	}
	return users, nil
}

func writeUsers(users []user) error {
	return writeJSONFile(usersPath, users)
}

func findUser(username string) (*user, error) {
	users, err := readUsers()
	if err != nil {
		return nil, err
	}
	for i := range users {
		if users[i].Username == username {
			return &users[i], nil
		}
	}
	return nil, nil
}

// ── Item file helpers ─────────────────────────────────────────────────────────

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

// ── Generic JSON helpers ──────────────────────────────────────────────────────

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

// ── Backup helpers ────────────────────────────────────────────────────────────

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

	type backupFile struct {
		name string
		time time.Time
	}

	var backups []backupFile
	for _, e := range entries {
		if !e.IsDir() && strings.HasPrefix(e.Name(), "items-") && strings.HasSuffix(e.Name(), ".json") {
			timestampStr := strings.TrimPrefix(e.Name(), "items-")
			timestampStr = strings.TrimSuffix(timestampStr, ".json")
			timestampStr = strings.ReplaceAll(timestampStr, "-", ":")
			timestampStr = strings.Replace(timestampStr, ":", "-", 2)
			timestampStr = strings.Replace(timestampStr, ":", "-", 1)
			t, err := time.Parse("2006-01-02T15-04-05Z07:00", timestampStr)
			if err != nil {
				continue
			}
			backups = append(backups, backupFile{name: e.Name(), time: t})
		}
	}

	if len(backups) == 0 {
		return
	}

	sort.Slice(backups, func(i, j int) bool {
		return backups[i].time.Before(backups[j].time)
	})

	now := time.Now()
	toKeep := make(map[string]bool)
	toKeep[backups[0].name] = true

	var (
		recent  = 2 * time.Hour
		hourly  = 24 * time.Hour
		daily   = 7 * 24 * time.Hour
		weekly  = 30 * 24 * time.Hour
		monthly = 365 * 24 * time.Hour
	)

	for _, b := range backups {
		if now.Sub(b.time) <= recent {
			toKeep[b.name] = true
		}
	}

	hourlyBuckets := make(map[string]backupFile)
	for _, b := range backups {
		age := now.Sub(b.time)
		if age > recent && age <= hourly {
			bucket := b.time.Truncate(time.Hour).Format(time.RFC3339)
			if existing, exists := hourlyBuckets[bucket]; !exists || b.time.After(existing.time) {
				hourlyBuckets[bucket] = b
			}
		}
	}
	for _, b := range hourlyBuckets {
		toKeep[b.name] = true
	}

	dailyBuckets := make(map[string]backupFile)
	for _, b := range backups {
		age := now.Sub(b.time)
		if age > hourly && age <= daily {
			bucket := b.time.Truncate(24 * time.Hour).Format(time.RFC3339)
			if existing, exists := dailyBuckets[bucket]; !exists || b.time.After(existing.time) {
				dailyBuckets[bucket] = b
			}
		}
	}
	for _, b := range dailyBuckets {
		toKeep[b.name] = true
	}

	weeklyBuckets := make(map[string]backupFile)
	for _, b := range backups {
		age := now.Sub(b.time)
		if age > daily && age <= weekly {
			_, week := b.time.ISOWeek()
			bucket := fmt.Sprintf("%d-W%d", b.time.Year(), week)
			if existing, exists := weeklyBuckets[bucket]; !exists || b.time.After(existing.time) {
				weeklyBuckets[bucket] = b
			}
		}
	}
	for _, b := range weeklyBuckets {
		toKeep[b.name] = true
	}

	monthlyBuckets := make(map[string]backupFile)
	for _, b := range backups {
		age := now.Sub(b.time)
		if age > weekly && age <= monthly {
			bucket := b.time.Format("2006-01")
			if existing, exists := monthlyBuckets[bucket]; !exists || b.time.After(existing.time) {
				monthlyBuckets[bucket] = b
			}
		}
	}
	for _, b := range monthlyBuckets {
		toKeep[b.name] = true
	}

	yearlyBuckets := make(map[string]backupFile)
	for _, b := range backups {
		age := now.Sub(b.time)
		if age > monthly {
			bucket := b.time.Format("2006")
			if existing, exists := yearlyBuckets[bucket]; !exists || b.time.After(existing.time) {
				yearlyBuckets[bucket] = b
			}
		}
	}
	for _, b := range yearlyBuckets {
		toKeep[b.name] = true
	}

	for _, b := range backups {
		if !toKeep[b.name] {
			_ = os.Remove(filepath.Join(backupsDir, b.name))
		}
	}
}
