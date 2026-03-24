package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

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

func broadcastItemsUpdate(b *sseBroker, items []item) {
	payload, _ := json.Marshal(sseMessage{Type: "update", Items: items})
	b.broadcast(string(payload))
}

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
