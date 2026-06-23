package main

import "github.com/gorilla/websocket"

func NewHub() *Hub {
	return &Hub{clients: map[*websocket.Conn]struct{}{}}
}

func (h *Hub) Add(conn *websocket.Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.clients[conn] = struct{}{}
}

func (h *Hub) Remove(conn *websocket.Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.clients, conn)
	_ = conn.Close()
}

func (h *Hub) Broadcast(message any) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for conn := range h.clients {
		if err := conn.WriteJSON(message); err != nil {
			delete(h.clients, conn)
			_ = conn.Close()
		}
	}
}
