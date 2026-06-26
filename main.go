// 本文件负责启动本地会议核心服务并装配运行依赖。
package main

import (
	"context"
	"database/sql"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/gorilla/websocket"
	"meeting-core-service/internal/stt"
	_ "modernc.org/sqlite"
)

func main() {
	loadDotEnv(".env")
	loadDotEnv(filepath.Join("..", "volcano-asr-demo", ".env"))

	cfg := Config{
		Addr:          envOrDefault("MEETING_CORE_ADDR", ":8090"),
		DB:            envOrDefault("MEETING_CORE_DB", "./data/meeting-core.db"),
		STTProvider:   envOrDefault("STT_PROVIDER", "volcano"),
		PublicBaseURL: envOrDefault("MEETING_PUBLIC_BASE_URL", ""),
	}

	if err := os.MkdirAll(filepath.Dir(cfg.DB), 0755); err != nil {
		log.Fatalf("create data directory: %v", err)
	}

	db, err := sql.Open("sqlite", cfg.DB)
	if err != nil {
		log.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	store := &Store{db: db}
	if err := store.Init(context.Background()); err != nil {
		log.Fatalf("init database: %v", err)
	}

	sttAdapter, err := stt.NewAdapter(cfg.STTProvider)
	if err != nil {
		log.Fatalf("init stt adapter: %v", err)
	}
	defer sttAdapter.Close()

	server := &Server{
		cfg:                 cfg,
		store:               store,
		hub:                 NewHub(),
		sttAdapter:          sttAdapter,
		llmClient:           OpenAICompatibleLLMClient{},
		intelBusy:           map[string]bool{},
		intelLastRun:        map[string]time.Time{},
		intelLastLen:        map[string]int{},
		intelTimers:         map[string]*time.Timer{},
		pendingInteractions: map[string]*PendingInteraction{},
		sttSeenAt:           map[string]time.Time{},
		sttResultAt:         map[string]time.Time{},
		sttRestartAt:        map[string]time.Time{},
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		},
	}
	go server.consumeSTT(context.Background())

	mux := http.NewServeMux()
	mux.HandleFunc("/share/", server.handleSharePage)
	mux.HandleFunc("/", server.handleIndex)
	mux.HandleFunc("/ws", server.handleWebSocket)
	mux.HandleFunc("/api/llm-config", server.handleLLMConfig)
	mux.HandleFunc("/api/state", server.handleState)
	mux.HandleFunc("/api/meetings", server.handleMeetings)
	mux.HandleFunc("/api/meetings/", server.handleMeetingAction)

	log.Printf("meeting core service listening on http://localhost%s", cfg.Addr)
	log.Printf("sqlite database: %s", cfg.DB)
	log.Printf("stt provider: %s", sttAdapter.Provider())
	if err := http.ListenAndServe(cfg.Addr, logRequests(mux)); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
