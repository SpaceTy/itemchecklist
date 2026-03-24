package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

type sseClient struct {
	ListID string
	Ch     chan string
}

type sseBroker struct {
	mu      sync.Mutex
	clients map[int]sseClient
	nextID  int
}

func newSSEBroker() *sseBroker {
	return &sseBroker{clients: make(map[int]sseClient)}
}

func (b *sseBroker) addClient(listID string, ch chan string) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	id := b.nextID
	b.nextID++
	b.clients[id] = sseClient{ListID: listID, Ch: ch}
	return id
}

func (b *sseBroker) removeClient(id int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.clients, id)
}

func (b *sseBroker) broadcast(listID, msg string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for id, client := range b.clients {
		if client.ListID != listID {
			continue
		}
		select {
		case client.Ch <- msg:
		default:
			delete(b.clients, id)
		}
	}
}

func broadcastListUpdate(b *sseBroker, list itemList) {
	payload, _ := json.Marshal(sseMessage{
		Type:   "update",
		ListID: list.ID,
		Items:  list.Items,
	})
	b.broadcast(list.ID, string(payload))
}

func broadcastAllLists(b *sseBroker, lists []itemList) {
	for _, list := range lists {
		broadcastListUpdate(b, list)
	}
}

func sseHandler(b *sseBroker) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming unsupported", http.StatusInternalServerError)
			return
		}

		listID := r.URL.Query().Get("list_id")
		if listID == "" {
			http.Error(w, `{"error":"list_id is required"}`, http.StatusBadRequest)
			return
		}

		username := r.Context().Value(usernameKey).(string)
		user, err := findUser(username)
		if err != nil || user == nil {
			http.Error(w, `{"error":"Unauthorized"}`, http.StatusUnauthorized)
			return
		}

		lists, idx, err := readListsWithIndex(listID)
		if err != nil {
			http.Error(w, `{"error":"Could not read lists"}`, http.StatusInternalServerError)
			return
		}
		if idx == -1 {
			http.Error(w, `{"error":"List not found"}`, http.StatusNotFound)
			return
		}
		if !canAccessList(lists[idx], user.Username, user.Admin) {
			http.Error(w, `{"error":"Forbidden"}`, http.StatusForbidden)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")

		ch := make(chan string, 4)
		id := b.addClient(listID, ch)
		defer b.removeClient(id)

		initial, _ := json.Marshal(sseMessage{
			Type:   "update",
			ListID: listID,
			Items:  lists[idx].Items,
		})
		if _, err := fmt.Fprintf(w, "data: %s\n\n", initial); err == nil {
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
