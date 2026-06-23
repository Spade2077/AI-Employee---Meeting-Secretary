package main

import (
	"database/sql"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"meeting-core-service/internal/stt"
)

// Store owns the local SQLite persistence layer.
// Keep database writes behind this type so state changes and intelligence data
// stay auditable in one place.
type Store struct {
	db *sql.DB
	mu sync.Mutex
}

// Hub fans server-side events out to every open browser dashboard.
type Hub struct {
	mu      sync.Mutex
	clients map[*websocket.Conn]struct{}
}

// Server wires the HTTP API, WebSocket pushes, STT adapter, and intelligence pipeline.
type Server struct {
	cfg                 Config
	store               *Store
	hub                 *Hub
	sttAdapter          stt.Adapter
	llmClient           LLMClient
	upgrader            websocket.Upgrader
	intelMu             sync.Mutex
	intelBusy           map[string]bool
	intelLastRun        map[string]time.Time
	intelLastLen        map[string]int
	intelTimers         map[string]*time.Timer
	interactionMu       sync.Mutex
	pendingInteractions map[string]*PendingInteraction
	sttMu               sync.Mutex
	sttSeenAt           map[string]time.Time
	sttResultAt         map[string]time.Time
	sttRestartAt        map[string]time.Time
}
