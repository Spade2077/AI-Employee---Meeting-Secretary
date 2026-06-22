package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"meeting-core-service/internal/stt"
	_ "modernc.org/sqlite"
)

type MeetingStatus string

const (
	StatusStandby    MeetingStatus = "standby"
	StatusRecording  MeetingStatus = "recording"
	StatusSoftPaused MeetingStatus = "soft_paused"
	StatusHardMuted  MeetingStatus = "hard_muted"
	StatusEnding     MeetingStatus = "ending"
	StatusEnded      MeetingStatus = "ended"
)

func CanUploadAudio(status MeetingStatus) bool {
	return status == StatusRecording
}

type Config struct {
	Addr        string
	DB          string
	STTProvider string
}

type Meeting struct {
	ID                string `json:"id"`
	Title             string `json:"title"`
	Type              string `json:"type"`
	Status            string `json:"status"`
	PreviousStatus    string `json:"previousStatus,omitempty"`
	PreHardMuteStatus string `json:"preHardMuteStatus,omitempty"`
	StartedAt         string `json:"startedAt,omitempty"`
	EndedAt           string `json:"endedAt,omitempty"`
	DurationSeconds   int64  `json:"durationSeconds"`
	CreatedAt         string `json:"createdAt"`
	UpdatedAt         string `json:"updatedAt"`
}

type StateEvent struct {
	ID         string `json:"id"`
	MeetingID  string `json:"meetingId"`
	FromStatus string `json:"fromStatus"`
	ToStatus   string `json:"toStatus"`
	EventType  string `json:"eventType"`
	Operator   string `json:"operator"`
	Note       string `json:"note,omitempty"`
	CreatedAt  string `json:"createdAt"`
}

type Utterance struct {
	ID           string  `json:"id"`
	MeetingID    string  `json:"meetingId"`
	SpeakerLabel string  `json:"speakerLabel"`
	Text         string  `json:"text"`
	IsFinal      bool    `json:"isFinal"`
	Provider     string  `json:"provider"`
	Confidence   float64 `json:"confidence"`
	IsDeleted    bool    `json:"isDeleted"`
	CreatedAt    string  `json:"createdAt"`
	DeletedAt    string  `json:"deletedAt,omitempty"`
}

type TranscriptDelta struct {
	Type      string     `json:"type"`
	MeetingID string     `json:"meetingId"`
	Utterance *Utterance `json:"utterance"`
	Timestamp string     `json:"timestamp"`
}

type STTErrorEvent struct {
	Type      string `json:"type"`
	MeetingID string `json:"meetingId,omitempty"`
	Provider  string `json:"provider"`
	Message   string `json:"message"`
	Timestamp string `json:"timestamp"`
}

type StateUpdate struct {
	Type            string      `json:"type"`
	MeetingID       string      `json:"meetingId,omitempty"`
	Status          string      `json:"status"`
	PreviousStatus  string      `json:"previousStatus,omitempty"`
	DurationSeconds int64       `json:"durationSeconds"`
	Meeting         *Meeting    `json:"meeting,omitempty"`
	Event           *StateEvent `json:"event,omitempty"`
	Timestamp       string      `json:"timestamp"`
}

type Store struct {
	db *sql.DB
	mu sync.Mutex
}

type Hub struct {
	mu      sync.Mutex
	clients map[*websocket.Conn]struct{}
}

type Server struct {
	cfg        Config
	store      *Store
	hub        *Hub
	sttAdapter stt.Adapter
	upgrader   websocket.Upgrader
}

func main() {
	loadDotEnv(".env")
	loadDotEnv(filepath.Join("..", "volcano-asr-demo", ".env"))

	cfg := Config{
		Addr:        envOrDefault("MEETING_CORE_ADDR", ":8090"),
		DB:          envOrDefault("MEETING_CORE_DB", "./data/meeting-core.db"),
		STTProvider: envOrDefault("STT_PROVIDER", "mock"),
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
		cfg:        cfg,
		store:      store,
		hub:        NewHub(),
		sttAdapter: sttAdapter,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		},
	}
	go server.consumeSTT(context.Background())

	mux := http.NewServeMux()
	mux.HandleFunc("/", server.handleIndex)
	mux.HandleFunc("/ws", server.handleWebSocket)
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

func loadDotEnv(path string) {
	file, err := os.Open(path)
	if err != nil {
		return
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || !strings.Contains(line, "=") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		key := strings.TrimSpace(parts[0])
		value := strings.Trim(strings.TrimSpace(parts[1]), `"'`)
		if key != "" && os.Getenv(key) == "" {
			_ = os.Setenv(key, value)
		}
	}
}

func envOrDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("%s %s %s", r.Method, r.URL.Path, time.Since(start).Truncate(time.Millisecond))
	})
}

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

func (s *Store) Init(ctx context.Context) error {
	schema := []string{
		`PRAGMA journal_mode = WAL;`,
		`CREATE TABLE IF NOT EXISTS meetings (
			id TEXT PRIMARY KEY,
			title TEXT NOT NULL DEFAULT '',
			type TEXT NOT NULL DEFAULT 'unknown',
			status TEXT NOT NULL,
			previous_status TEXT NOT NULL DEFAULT '',
			pre_hard_mute_status TEXT NOT NULL DEFAULT '',
			started_at TEXT,
			ended_at TEXT,
			duration_seconds INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS state_events (
			id TEXT PRIMARY KEY,
			meeting_id TEXT NOT NULL,
			from_status TEXT NOT NULL DEFAULT '',
			to_status TEXT NOT NULL DEFAULT '',
			event_type TEXT NOT NULL,
			operator TEXT NOT NULL DEFAULT 'system',
			note TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			FOREIGN KEY(meeting_id) REFERENCES meetings(id)
		);`,
		`CREATE TABLE IF NOT EXISTS utterances (
			id TEXT PRIMARY KEY,
			meeting_id TEXT NOT NULL,
			speaker_label TEXT NOT NULL DEFAULT 'unknown',
			text TEXT NOT NULL,
			is_final INTEGER NOT NULL DEFAULT 1,
			provider TEXT NOT NULL DEFAULT 'manual',
			confidence REAL NOT NULL DEFAULT 0,
			is_deleted INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL,
			deleted_at TEXT,
			FOREIGN KEY(meeting_id) REFERENCES meetings(id)
		);`,
		`CREATE INDEX IF NOT EXISTS idx_state_events_meeting_created ON state_events(meeting_id, created_at);`,
		`CREATE INDEX IF NOT EXISTS idx_utterances_meeting_created ON utterances(meeting_id, created_at);`,
	}
	for _, stmt := range schema {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	if err := s.ensureColumn(ctx, "utterances", "is_final", "INTEGER NOT NULL DEFAULT 1"); err != nil {
		return err
	}
	if err := s.ensureColumn(ctx, "utterances", "provider", "TEXT NOT NULL DEFAULT 'manual'"); err != nil {
		return err
	}
	if err := s.ensureColumn(ctx, "utterances", "confidence", "REAL NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	return nil
}

func (s *Store) ensureColumn(ctx context.Context, table, column, definition string) error {
	rows, err := s.db.QueryContext(ctx, "PRAGMA table_info("+table+")")
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var cid int
		var name, colType string
		var notNull int
		var defaultValue any
		var pk int
		if err := rows.Scan(&cid, &name, &colType, &notNull, &defaultValue, &pk); err != nil {
			return err
		}
		if name == column {
			return rows.Err()
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", table, column, definition))
	return err
}

func (s *Store) CreateMeeting(ctx context.Context, title, meetingType, operator string) (*Meeting, *StateEvent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	active, err := s.currentMeeting(ctx)
	if err != nil {
		return nil, nil, err
	}
	if active != nil && active.Status != string(StatusEnded) {
		return nil, nil, fmt.Errorf("active meeting already exists: %s (%s)", active.ID, active.Status)
	}

	now := nowString()
	if strings.TrimSpace(title) == "" {
		title = "未命名会议"
	}
	if strings.TrimSpace(meetingType) == "" {
		meetingType = "unknown"
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, err
	}
	defer tx.Rollback()

	meetingID := newID("m")
	if _, err := tx.ExecContext(ctx, `INSERT INTO meetings
		(id, title, type, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		meetingID, title, meetingType, StatusStandby, now, now); err != nil {
		return nil, nil, err
	}

	event := StateEvent{
		ID:         newID("e"),
		MeetingID:  meetingID,
		FromStatus: "",
		ToStatus:   string(StatusStandby),
		EventType:  "create_meeting",
		Operator:   defaultOperator(operator),
		CreatedAt:  now,
	}
	if err := insertEvent(ctx, tx, event); err != nil {
		return nil, nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, nil, err
	}

	meeting, err := s.GetMeeting(ctx, meetingID)
	return meeting, &event, err
}

func (s *Store) CurrentMeeting(ctx context.Context) (*Meeting, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.currentMeeting(ctx)
}

func (s *Store) currentMeeting(ctx context.Context) (*Meeting, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, title, type, status, previous_status, pre_hard_mute_status,
		COALESCE(started_at, ''), COALESCE(ended_at, ''), duration_seconds, created_at, updated_at
		FROM meetings ORDER BY created_at DESC LIMIT 1`)
	meeting, err := scanMeeting(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return meeting, nil
}

func (s *Store) GetMeeting(ctx context.Context, meetingID string) (*Meeting, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, title, type, status, previous_status, pre_hard_mute_status,
		COALESCE(started_at, ''), COALESCE(ended_at, ''), duration_seconds, created_at, updated_at
		FROM meetings WHERE id = ?`, meetingID)
	return scanMeeting(row)
}

func scanMeeting(scanner interface {
	Scan(dest ...any) error
}) (*Meeting, error) {
	var m Meeting
	if err := scanner.Scan(&m.ID, &m.Title, &m.Type, &m.Status, &m.PreviousStatus, &m.PreHardMuteStatus,
		&m.StartedAt, &m.EndedAt, &m.DurationSeconds, &m.CreatedAt, &m.UpdatedAt); err != nil {
		return nil, err
	}
	if m.EndedAt == "" && m.StartedAt != "" && (m.Status == string(StatusRecording) || m.Status == string(StatusSoftPaused) || m.Status == string(StatusHardMuted)) {
		if startedAt, err := time.Parse(time.RFC3339Nano, m.StartedAt); err == nil {
			m.DurationSeconds = int64(time.Since(startedAt).Seconds())
		}
	}
	return &m, nil
}

func (s *Store) Transition(ctx context.Context, meetingID, eventType, operator string) (*Meeting, *StateEvent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, err
	}
	defer tx.Rollback()

	meeting, err := getMeetingTx(ctx, tx, meetingID)
	if err != nil {
		return nil, nil, err
	}

	from := MeetingStatus(meeting.Status)
	to, note, err := nextStatus(meeting, eventType)
	if err != nil {
		return nil, nil, err
	}

	now := nowString()
	startedAt := meeting.StartedAt
	endedAt := meeting.EndedAt
	duration := meeting.DurationSeconds
	preHardMute := meeting.PreHardMuteStatus

	switch eventType {
	case "start":
		if startedAt == "" {
			startedAt = now
		}
	case "hard_mute_toggle":
		if from != StatusHardMuted {
			preHardMute = string(from)
		} else {
			preHardMute = ""
		}
	case "finish":
		endedAt = now
		duration = computeDurationSeconds(startedAt, now)
	}

	if _, err := tx.ExecContext(ctx, `UPDATE meetings SET
		status = ?, previous_status = ?, pre_hard_mute_status = ?, started_at = NULLIF(?, ''),
		ended_at = NULLIF(?, ''), duration_seconds = ?, updated_at = ?
		WHERE id = ?`,
		to, from, preHardMute, startedAt, endedAt, duration, now, meetingID); err != nil {
		return nil, nil, err
	}

	event := StateEvent{
		ID:         newID("e"),
		MeetingID:  meetingID,
		FromStatus: string(from),
		ToStatus:   string(to),
		EventType:  eventType,
		Operator:   defaultOperator(operator),
		Note:       note,
		CreatedAt:  now,
	}
	if err := insertEvent(ctx, tx, event); err != nil {
		return nil, nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, nil, err
	}

	updated, err := s.GetMeeting(ctx, meetingID)
	return updated, &event, err
}

func nextStatus(meeting *Meeting, eventType string) (MeetingStatus, string, error) {
	from := MeetingStatus(meeting.Status)
	switch eventType {
	case "start":
		if from != StatusStandby {
			return "", "", fmt.Errorf("cannot start from %s", from)
		}
		return StatusRecording, "", nil
	case "pause":
		if from != StatusRecording {
			return "", "", fmt.Errorf("cannot pause from %s", from)
		}
		return StatusSoftPaused, "", nil
	case "resume":
		if from != StatusSoftPaused {
			return "", "", fmt.Errorf("cannot resume from %s", from)
		}
		return StatusRecording, "", nil
	case "hard_mute_toggle":
		if from == StatusHardMuted {
			restore := MeetingStatus(meeting.PreHardMuteStatus)
			if restore == "" || restore == StatusHardMuted || restore == StatusEnding || restore == StatusEnded {
				restore = StatusStandby
			}
			return restore, "hard mute restored", nil
		}
		if from == StatusStandby || from == StatusRecording || from == StatusSoftPaused {
			return StatusHardMuted, "hard mute enabled", nil
		}
		return "", "", fmt.Errorf("cannot hard mute from %s", from)
	case "ending":
		if from == StatusRecording || from == StatusSoftPaused || from == StatusHardMuted || from == StatusStandby {
			return StatusEnding, "", nil
		}
		return "", "", fmt.Errorf("cannot end from %s", from)
	case "finish":
		if from != StatusEnding {
			return "", "", fmt.Errorf("cannot finish from %s", from)
		}
		return StatusEnded, "final minutes generation is stubbed in MVP core", nil
	default:
		return "", "", fmt.Errorf("unknown transition event: %s", eventType)
	}
}

func getMeetingTx(ctx context.Context, tx *sql.Tx, meetingID string) (*Meeting, error) {
	row := tx.QueryRowContext(ctx, `SELECT id, title, type, status, previous_status, pre_hard_mute_status,
		COALESCE(started_at, ''), COALESCE(ended_at, ''), duration_seconds, created_at, updated_at
		FROM meetings WHERE id = ?`, meetingID)
	return scanMeeting(row)
}

func insertEvent(ctx context.Context, tx *sql.Tx, event StateEvent) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO state_events
		(id, meeting_id, from_status, to_status, event_type, operator, note, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		event.ID, event.MeetingID, event.FromStatus, event.ToStatus, event.EventType, event.Operator, event.Note, event.CreatedAt)
	return err
}

func (s *Store) AddUtterance(ctx context.Context, meetingID, speakerLabel, text string) (*Utterance, error) {
	if strings.TrimSpace(text) == "" {
		text = "mock transcript segment"
	}
	if strings.TrimSpace(speakerLabel) == "" {
		speakerLabel = "unknown"
	}
	return s.AddTranscript(ctx, stt.TranscriptResult{
		MeetingID:    meetingID,
		SpeakerLabel: speakerLabel,
		Text:         text,
		IsFinal:      true,
		Provider:     "manual",
		Confidence:   1,
	})
}

func (s *Store) AddTranscript(ctx context.Context, result stt.TranscriptResult) (*Utterance, error) {
	if strings.TrimSpace(result.Text) == "" {
		return nil, fmt.Errorf("transcript text is empty")
	}
	if strings.TrimSpace(result.SpeakerLabel) == "" {
		result.SpeakerLabel = "unknown"
	}
	if strings.TrimSpace(result.Provider) == "" {
		result.Provider = "unknown"
	}
	now := nowString()
	u := &Utterance{
		ID:           newID("u"),
		MeetingID:    result.MeetingID,
		SpeakerLabel: result.SpeakerLabel,
		Text:         result.Text,
		IsFinal:      result.IsFinal,
		Provider:     result.Provider,
		Confidence:   result.Confidence,
		CreatedAt:    now,
	}
	isFinal := 0
	if u.IsFinal {
		isFinal = 1
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO utterances
		(id, meeting_id, speaker_label, text, is_final, provider, confidence, is_deleted, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, 0, ?)`,
		u.ID, u.MeetingID, u.SpeakerLabel, u.Text, isFinal, u.Provider, u.Confidence, u.CreatedAt)
	return u, err
}

func (s *Store) DeleteRecentUtterance(ctx context.Context, meetingID, operator string) (*Meeting, *StateEvent, *Utterance, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, nil, err
	}
	defer tx.Rollback()

	meeting, err := getMeetingTx(ctx, tx, meetingID)
	if err != nil {
		return nil, nil, nil, err
	}
	if meeting.Status == string(StatusEnded) || meeting.Status == string(StatusEnding) {
		return nil, nil, nil, fmt.Errorf("cannot delete recent segment from %s", meeting.Status)
	}

	row := tx.QueryRowContext(ctx, `SELECT id, meeting_id, speaker_label, text, is_final, provider, confidence,
		is_deleted, created_at, COALESCE(deleted_at, '')
		FROM utterances
		WHERE meeting_id = ? AND is_deleted = 0
		ORDER BY created_at DESC LIMIT 1`, meetingID)
	u, err := scanUtterance(row)
	note := "no undeleted utterance found"
	if err == nil {
		deletedAt := nowString()
		if _, err := tx.ExecContext(ctx, `UPDATE utterances SET is_deleted = 1, deleted_at = ? WHERE id = ?`, deletedAt, u.ID); err != nil {
			return nil, nil, nil, err
		}
		u.IsDeleted = true
		u.DeletedAt = deletedAt
		note = "deleted utterance " + u.ID
	} else if !errors.Is(err, sql.ErrNoRows) {
		return nil, nil, nil, err
	} else {
		u = nil
	}

	now := nowString()
	event := StateEvent{
		ID:         newID("e"),
		MeetingID:  meetingID,
		FromStatus: meeting.Status,
		ToStatus:   meeting.Status,
		EventType:  "delete_recent",
		Operator:   defaultOperator(operator),
		Note:       note,
		CreatedAt:  now,
	}
	if err := insertEvent(ctx, tx, event); err != nil {
		return nil, nil, nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, nil, nil, err
	}

	updated, err := s.GetMeeting(ctx, meetingID)
	return updated, &event, u, err
}

func scanUtterance(scanner interface {
	Scan(dest ...any) error
}) (*Utterance, error) {
	var u Utterance
	var isFinal int
	var deleted int
	if err := scanner.Scan(&u.ID, &u.MeetingID, &u.SpeakerLabel, &u.Text, &isFinal, &u.Provider, &u.Confidence, &deleted, &u.CreatedAt, &u.DeletedAt); err != nil {
		return nil, err
	}
	u.IsFinal = isFinal != 0
	u.IsDeleted = deleted != 0
	return &u, nil
}

func (s *Store) ListEvents(ctx context.Context, meetingID string) ([]StateEvent, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, meeting_id, from_status, to_status, event_type, operator, note, created_at
		FROM state_events WHERE meeting_id = ? ORDER BY created_at ASC`, meetingID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []StateEvent
	for rows.Next() {
		var e StateEvent
		if err := rows.Scan(&e.ID, &e.MeetingID, &e.FromStatus, &e.ToStatus, &e.EventType, &e.Operator, &e.Note, &e.CreatedAt); err != nil {
			return nil, err
		}
		events = append(events, e)
	}
	return events, rows.Err()
}

func (s *Store) ListUtterances(ctx context.Context, meetingID string) ([]Utterance, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, meeting_id, speaker_label, text, is_final, provider, confidence,
		is_deleted, created_at, COALESCE(deleted_at, '')
		FROM utterances WHERE meeting_id = ? ORDER BY created_at ASC`, meetingID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var utterances []Utterance
	for rows.Next() {
		u, err := scanUtterance(rows)
		if err != nil {
			return nil, err
		}
		utterances = append(utterances, *u)
	}
	return utterances, rows.Err()
}

func (s *Server) handleState(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	meeting, err := s.store.CurrentMeeting(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, s.stateUpdate(meeting, nil))
}

func (s *Server) handleMeetings(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/api/meetings" {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req struct {
		Title    string `json:"title"`
		Type     string `json:"type"`
		Operator string `json:"operator"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)

	meeting, event, err := s.store.CreateMeeting(r.Context(), req.Title, req.Type, req.Operator)
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	update := s.stateUpdate(meeting, event)
	s.hub.Broadcast(update)
	writeJSON(w, http.StatusCreated, update)
}

func (s *Server) handleMeetingAction(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/meetings/")
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		writeError(w, http.StatusNotFound, "missing meeting id")
		return
	}
	meetingID := parts[0]

	if len(parts) == 1 && r.Method == http.MethodGet {
		meeting, err := s.store.GetMeeting(r.Context(), meetingID)
		if err != nil {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, meeting)
		return
	}

	if len(parts) < 2 {
		writeError(w, http.StatusNotFound, "missing action")
		return
	}

	action := parts[1]
	switch {
	case r.Method == http.MethodPost && action == "start":
		s.transitionHTTP(w, r, meetingID, "start")
	case r.Method == http.MethodPost && action == "pause":
		s.transitionHTTP(w, r, meetingID, "pause")
	case r.Method == http.MethodPost && action == "resume":
		s.transitionHTTP(w, r, meetingID, "resume")
	case r.Method == http.MethodPost && action == "hard-mute" && len(parts) == 3 && parts[2] == "toggle":
		s.transitionHTTP(w, r, meetingID, "hard_mute_toggle")
	case r.Method == http.MethodPost && action == "delete-recent":
		s.deleteRecentHTTP(w, r, meetingID)
	case r.Method == http.MethodPost && action == "end":
		s.endMeetingHTTP(w, r, meetingID)
	case r.Method == http.MethodPost && action == "utterances":
		s.addUtteranceHTTP(w, r, meetingID)
	case r.Method == http.MethodPost && action == "audio" && len(parts) == 3 && parts[2] == "mock":
		s.mockAudioHTTP(w, r, meetingID)
	case r.Method == http.MethodGet && action == "audio" && len(parts) == 3 && parts[2] == "ws":
		s.audioWebSocket(w, r, meetingID)
	case r.Method == http.MethodGet && action == "events":
		events, err := s.store.ListEvents(r.Context(), meetingID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, events)
	case r.Method == http.MethodGet && action == "utterances":
		utterances, err := s.store.ListUtterances(r.Context(), meetingID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, utterances)
	default:
		writeError(w, http.StatusNotFound, "unknown action")
	}
}

func (s *Server) transitionHTTP(w http.ResponseWriter, r *http.Request, meetingID, eventType string) {
	current, err := s.store.GetMeeting(r.Context(), meetingID)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	target, _, err := nextStatus(current, eventType)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	currentStatus := MeetingStatus(current.Status)
	shouldStopSTT := currentStatus == StatusRecording && target != StatusRecording

	meeting, event, err := s.store.Transition(r.Context(), meetingID, eventType, operatorFromRequest(r))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	update := s.stateUpdate(meeting, event)
	s.hub.Broadcast(update)
	if shouldStopSTT {
		if err := s.sttAdapter.StopSession(r.Context(), meetingID); err != nil {
			log.Printf("stop stt session failed meeting=%s transition=%s: %v", meetingID, eventType, err)
		}
	}
	writeJSON(w, http.StatusOK, update)
}

func (s *Server) endMeetingHTTP(w http.ResponseWriter, r *http.Request, meetingID string) {
	meeting, event, err := s.store.Transition(r.Context(), meetingID, "ending", operatorFromRequest(r))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.hub.Broadcast(s.stateUpdate(meeting, event))
	if err := s.sttAdapter.StopSession(r.Context(), meetingID); err != nil {
		log.Printf("stop stt session failed meeting=%s: %v", meetingID, err)
	}

	meeting, event, err = s.store.Transition(r.Context(), meetingID, "finish", "system")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	update := s.stateUpdate(meeting, event)
	s.hub.Broadcast(update)
	writeJSON(w, http.StatusOK, update)
}

func (s *Server) deleteRecentHTTP(w http.ResponseWriter, r *http.Request, meetingID string) {
	meeting, event, utterance, err := s.store.DeleteRecentUtterance(r.Context(), meetingID, operatorFromRequest(r))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	update := s.stateUpdate(meeting, event)
	s.hub.Broadcast(update)
	writeJSON(w, http.StatusOK, map[string]any{
		"state":     update,
		"utterance": utterance,
	})
}

func (s *Server) addUtteranceHTTP(w http.ResponseWriter, r *http.Request, meetingID string) {
	var req struct {
		Text         string `json:"text"`
		SpeakerLabel string `json:"speakerLabel"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	u, err := s.store.AddUtterance(r.Context(), meetingID, req.SpeakerLabel, req.Text)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	delta := s.transcriptDelta(meetingID, u)
	s.hub.Broadcast(delta)
	writeJSON(w, http.StatusCreated, delta)
}

func (s *Server) mockAudioHTTP(w http.ResponseWriter, r *http.Request, meetingID string) {
	meeting, err := s.store.GetMeeting(r.Context(), meetingID)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	if !CanUploadAudio(MeetingStatus(meeting.Status)) {
		writeError(w, http.StatusConflict, fmt.Sprintf("audio upload is blocked while meeting status is %s", meeting.Status))
		return
	}

	var req struct {
		Text string `json:"text"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	if err := s.sendAudioToSTT(r.Context(), stt.AudioChunk{
		MeetingID: meetingID,
		Data:      []byte(req.Text),
		Format:    "mock/text",
		Timestamp: time.Now().UTC(),
	}); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{
		"accepted":  true,
		"meetingId": meetingID,
	})
}

func (s *Server) audioWebSocket(w http.ResponseWriter, r *http.Request, meetingID string) {
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("audio websocket upgrade: %v", err)
		return
	}
	defer conn.Close()

	for {
		msgType, data, err := conn.ReadMessage()
		if err != nil {
			return
		}

		switch msgType {
		case websocket.BinaryMessage:
			meeting, err := s.store.GetMeeting(r.Context(), meetingID)
			if err != nil {
				_ = conn.WriteJSON(map[string]any{
					"type":  "error",
					"error": err.Error(),
				})
				continue
			}
			if !CanUploadAudio(MeetingStatus(meeting.Status)) {
				_ = conn.WriteJSON(map[string]any{
					"type":   "audio_blocked",
					"status": meeting.Status,
				})
				continue
			}
			if err := s.sendAudioToSTT(r.Context(), stt.AudioChunk{
				MeetingID: meetingID,
				Data:      data,
				Format:    "pcm_s16le_16k_mono",
				Timestamp: time.Now().UTC(),
			}); err != nil {
				_ = conn.WriteJSON(map[string]any{
					"type":  "error",
					"error": err.Error(),
				})
			}

		case websocket.TextMessage:
			var msg map[string]any
			if err := json.Unmarshal(data, &msg); err != nil {
				continue
			}
			if msg["type"] == "end" {
				return
			}
		}
	}
}

func (s *Server) sendAudioToSTT(ctx context.Context, chunk stt.AudioChunk) error {
	if err := s.sttAdapter.SendAudio(ctx, chunk); err == nil {
		return nil
	}

	if err := s.sttAdapter.StartSession(ctx, chunk.MeetingID); err != nil {
		return err
	}
	if err := s.sttAdapter.SendAudio(ctx, chunk); err != nil {
		_ = s.sttAdapter.StopSession(context.Background(), chunk.MeetingID)
		return err
	}
	return nil
}

func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("websocket upgrade: %v", err)
		return
	}
	s.hub.Add(conn)
	defer s.hub.Remove(conn)

	meeting, err := s.store.CurrentMeeting(r.Context())
	if err == nil {
		_ = conn.WriteJSON(s.stateUpdate(meeting, nil))
	}

	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			return
		}
	}
}

func (s *Server) consumeSTT(ctx context.Context) {
	for {
		select {
		case result, ok := <-s.sttAdapter.Results():
			if !ok {
				return
			}
			if result.MeetingID == "" || strings.TrimSpace(result.Text) == "" {
				continue
			}
			utterance, err := s.store.AddTranscript(ctx, result)
			if err != nil {
				log.Printf("store transcript failed meeting=%s provider=%s: %v", result.MeetingID, result.Provider, err)
				continue
			}
			s.hub.Broadcast(s.transcriptDelta(result.MeetingID, utterance))

		case adapterErr, ok := <-s.sttAdapter.Errors():
			if !ok {
				return
			}
			log.Printf("stt error provider=%s meeting=%s: %s", adapterErr.Provider, adapterErr.MeetingID, adapterErr.Message)
			s.hub.Broadcast(STTErrorEvent{
				Type:      "stt_error",
				MeetingID: adapterErr.MeetingID,
				Provider:  adapterErr.Provider,
				Message:   adapterErr.Message,
				Timestamp: nowString(),
			})

		case <-ctx.Done():
			return
		}
	}
}

func (s *Server) stateUpdate(meeting *Meeting, event *StateEvent) StateUpdate {
	update := StateUpdate{
		Type:      "state_update",
		Status:    string(StatusStandby),
		Meeting:   meeting,
		Event:     event,
		Timestamp: nowString(),
	}
	if meeting != nil {
		update.MeetingID = meeting.ID
		update.Status = meeting.Status
		update.PreviousStatus = meeting.PreviousStatus
		update.DurationSeconds = meeting.DurationSeconds
	}
	return update
}

func (s *Server) transcriptDelta(meetingID string, utterance *Utterance) TranscriptDelta {
	return TranscriptDelta{
		Type:      "transcript_delta",
		MeetingID: meetingID,
		Utterance: utterance,
		Timestamp: nowString(),
	}
}

func operatorFromRequest(r *http.Request) string {
	operator := r.URL.Query().Get("operator")
	if operator == "" {
		operator = r.Header.Get("X-Operator")
	}
	return defaultOperator(operator)
}

func defaultOperator(operator string) string {
	if strings.TrimSpace(operator) == "" {
		return "screen"
	}
	return operator
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]any{
		"error": message,
	})
}

func nowString() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}

func computeDurationSeconds(startedAt, endedAt string) int64 {
	if startedAt == "" || endedAt == "" {
		return 0
	}
	start, err := time.Parse(time.RFC3339Nano, startedAt)
	if err != nil {
		return 0
	}
	end, err := time.Parse(time.RFC3339Nano, endedAt)
	if err != nil {
		return 0
	}
	if end.Before(start) {
		return 0
	}
	return int64(end.Sub(start).Seconds())
}

func newID(prefix string) string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%s_%d", prefix, time.Now().UnixNano())
	}
	return prefix + "_" + hex.EncodeToString(b[:])
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(indexHTML))
}

const indexHTML = `<!doctype html>
<html lang="zh-CN">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Meeting Core Debug</title>
  <style>
    body { font-family: system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif; margin: 32px; background: #f6f7f9; color: #172033; }
    main { max-width: 980px; margin: 0 auto; }
    .panel { background: #fff; border: 1px solid #d9dee8; border-radius: 8px; padding: 20px; margin-bottom: 16px; }
    h1 { margin: 0 0 18px; font-size: 24px; }
    h2 { margin: 0 0 12px; font-size: 16px; }
    .status { display: inline-flex; align-items: center; gap: 8px; padding: 8px 10px; border-radius: 6px; background: #eef3ff; font-weight: 700; }
    .dot { width: 10px; height: 10px; border-radius: 50%; background: #8792a8; }
    .recording .dot { background: #1d9a57; }
    .soft_paused .dot { background: #d97706; }
    .hard_muted .dot { background: #dc2626; }
    .ended .dot { background: #4b5563; }
    .buttons { display: flex; gap: 8px; flex-wrap: wrap; }
    button { border: 1px solid #cbd3df; background: #fff; color: #172033; border-radius: 6px; padding: 9px 12px; cursor: pointer; }
    button:hover { background: #eef3ff; }
    button.primary { background: #1f6feb; border-color: #1f6feb; color: #fff; }
    input { border: 1px solid #cbd3df; border-radius: 6px; padding: 9px 10px; min-width: 280px; }
    pre { background: #0f172a; color: #e5e7eb; border-radius: 8px; padding: 14px; overflow: auto; max-height: 320px; }
    .transcript-box { background: #0f172a; color: #e5e7eb; border: 1px solid #263244; border-radius: 8px; padding: 18px 22px; min-height: 180px; max-height: 420px; overflow-y: auto; line-height: 2; font-size: 15px; white-space: pre-wrap; word-break: break-word; }
    .transcript-placeholder { color: #64748b; }
    .transcript-segment { margin-right: 0; }
    .transcript-deleted { color: #94a3b8; text-decoration: line-through; opacity: 0.7; }
    .grid { display: grid; grid-template-columns: 1fr 1fr; gap: 16px; }
    @media (max-width: 780px) { body { margin: 16px; } .grid { grid-template-columns: 1fr; } input { min-width: 0; width: 100%; } }
  </style>
</head>
<body>
<main>
  <h1>会议核心服务调试页</h1>

  <section class="panel">
    <h2>当前状态</h2>
    <div id="statusBadge" class="status">
      <span class="dot"></span>
      <span id="statusText">standby</span>
    </div>
    <p>会议 ID：<code id="meetingId">无</code></p>
    <p>时长：<code id="duration">0s</code></p>
  </section>

  <section class="panel">
    <h2>操作</h2>
    <div class="buttons">
      <button class="primary" onclick="createMeeting()">创建会议</button>
      <button onclick="action('start')">开始</button>
      <button onclick="action('pause')">暂停</button>
      <button onclick="action('resume')">恢复</button>
      <button onclick="action('hard-mute/toggle')">硬静音切换</button>
      <button onclick="action('delete-recent')">删除最近片段</button>
      <button onclick="action('end')">结束</button>
      <button onclick="refreshAll()">刷新</button>
    </div>
  </section>

  <section class="panel">
    <h2>转写占位数据</h2>
    <input id="utteranceText" value="张三下周五前完成测试环境联调">
    <button onclick="addUtterance()">添加占位转写</button>
    <button onclick="sendMockAudio()">发送 Mock 音频</button>
    <button onclick="startBrowserAudio()">Start Browser Mic</button>
    <button onclick="stopBrowserAudio()">Stop Browser Mic</button>
  </section>

  <section class="panel">
    <h2>Full Transcript</h2>
    <div id="fullTranscript" class="transcript-box">
      <span class="transcript-placeholder">Waiting for transcript...</span>
    </div>
  </section>

  <div class="grid">
    <section class="panel">
      <h2>状态事件</h2>
      <pre id="events">[]</pre>
    </section>
    <section class="panel">
      <h2>转写占位</h2>
      <pre id="utterances">[]</pre>
    </section>
  </div>
</main>

<script>
let currentMeetingId = "";
let audioWS = null;
let audioContext = null;
let micStream = null;
let processor = null;
let browserAudioActive = false;

function renderState(update) {
  if (!update) return;
  const status = update.status || "standby";
  const badge = document.getElementById("statusBadge");
  badge.className = "status " + status;
  document.getElementById("statusText").textContent = status;
  currentMeetingId = update.meetingId || (update.meeting && update.meeting.id) || currentMeetingId;
  document.getElementById("meetingId").textContent = currentMeetingId || "无";
  document.getElementById("duration").textContent = (update.durationSeconds || 0) + "s";
  if (currentMeetingId) refreshLists();
}

async function createMeeting() {
  const res = await fetch("/api/meetings", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ title: "调试会议", type: "general", operator: "screen" })
  });
  await handleResponse(res);
}

async function action(name) {
  if (!currentMeetingId) {
    alert("请先创建会议");
    return;
  }
  const res = await fetch("/api/meetings/" + currentMeetingId + "/" + name, { method: "POST" });
  await handleResponse(res);
}

async function addUtterance() {
  if (!currentMeetingId) {
    alert("请先创建会议");
    return;
  }
  const text = document.getElementById("utteranceText").value;
  const res = await fetch("/api/meetings/" + currentMeetingId + "/utterances", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ speakerLabel: "speaker_1", text })
  });
  await handleResponse(res);
  await refreshLists();
}

async function sendMockAudio() {
  if (!currentMeetingId) {
    alert("请先创建会议");
    return;
  }
  const text = document.getElementById("utteranceText").value;
  const res = await fetch("/api/meetings/" + currentMeetingId + "/audio/mock", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ text })
  });
  await handleResponse(res);
  await refreshLists();
}

async function startBrowserAudio() {
  if (!currentMeetingId) {
    alert("请先创建会议");
    return;
  }
  if (browserAudioActive) return;

  audioWS = new WebSocket("ws://" + location.host + "/api/meetings/" + currentMeetingId + "/audio/ws");
  audioWS.binaryType = "arraybuffer";
  audioWS.onmessage = (event) => {
    try {
      const msg = JSON.parse(event.data);
      if (msg.type === "audio_blocked") console.warn("audio blocked", msg.status);
      if (msg.type === "error") console.error(msg.error);
    } catch(e) {}
  };
  await new Promise((resolve, reject) => {
    audioWS.onopen = resolve;
    audioWS.onerror = reject;
  });

  const stream = await navigator.mediaDevices.getUserMedia({
    audio: {
      channelCount: 1,
      sampleRate: 16000,
      echoCancellation: false,
      noiseSuppression: false,
      autoGainControl: false
    }
  });
  audioContext = new AudioContext({ sampleRate: 16000 });
  micStream = audioContext.createMediaStreamSource(stream);
  processor = audioContext.createScriptProcessor(4096, 1, 1);

  let lastSendTime = 0;
  processor.onaudioprocess = (e) => {
    if (!browserAudioActive || !audioWS || audioWS.readyState !== WebSocket.OPEN) return;
    const now = Date.now();
    if (now - lastSendTime < 160) return;
    lastSendTime = now;

    const input = e.inputBuffer.getChannelData(0);
    const buf = new ArrayBuffer(input.length * 2);
    const view = new DataView(buf);
    for (let i = 0; i < input.length; i++) {
      const s = Math.max(-1, Math.min(1, input[i]));
      view.setInt16(i * 2, s < 0 ? s * 0x8000 : s * 0x7fff, true);
    }
    audioWS.send(buf);
  };

  browserAudioActive = true;
  micStream.connect(processor);
  processor.connect(audioContext.destination);
}

function stopBrowserAudio() {
  browserAudioActive = false;
  if (audioWS && audioWS.readyState === WebSocket.OPEN) {
    audioWS.send(JSON.stringify({ type: "end" }));
    audioWS.close();
  }
  audioWS = null;
  if (processor) { try { processor.disconnect(); } catch(e) {} processor = null; }
  if (micStream) { try { micStream.disconnect(); } catch(e) {} micStream = null; }
  if (audioContext) { try { audioContext.close(); } catch(e) {} audioContext = null; }
}

async function handleResponse(res) {
  const body = await res.json().catch(() => ({}));
  if (!res.ok) {
    alert(body.error || "请求失败");
    return;
  }
  if (body.type === "state_update") renderState(body);
  if (body.type === "transcript_delta") refreshLists();
  if (body.state) renderState(body.state);
}

async function refreshAll() {
  const res = await fetch("/api/state");
  const body = await res.json();
  renderState(body);
}

async function refreshLists() {
  if (!currentMeetingId) return;
  const [eventsRes, utterRes] = await Promise.all([
    fetch("/api/meetings/" + currentMeetingId + "/events"),
    fetch("/api/meetings/" + currentMeetingId + "/utterances")
  ]);
  const events = await eventsRes.json().catch(() => []);
  const utterances = await utterRes.json().catch(() => []);
  document.getElementById("events").textContent = JSON.stringify(events, null, 2);
  document.getElementById("utterances").textContent = JSON.stringify(utterances, null, 2);
  renderFullTranscript(utterances);
}

function renderFullTranscript(utterances) {
  const transcript = document.getElementById("fullTranscript");
  if (!transcript) return;

  const items = Array.isArray(utterances) ? utterances.filter(item => !item.isDeleted) : [];
  if (!items.length) {
    transcript.innerHTML = '<span class="transcript-placeholder">Waiting for transcript...</span>';
    return;
  }

  const paragraphs = rebuildArticleTranscript(items);
  const html = paragraphs
    .map(text => '<span class="transcript-segment">' + escapeHtml(text) + '</span>')
    .join('');

  transcript.innerHTML = html || '<span class="transcript-placeholder">Waiting for transcript...</span>';
  transcript.scrollTop = transcript.scrollHeight;
}

function rebuildArticleTranscript(utterances) {
  const completed = [];
  let currentText = "";
  let lastLen = 0;

  const THRESHOLD = 0.6;
  const MIN_OLD = 8;
  const MIN_NEW = 5;

  for (const item of utterances) {
    const text = normalizeTranscriptText(item.text || "");
    if (!text) continue;

    const textLen = text.length;
    const shouldCommitPrevious =
      (textLen < MIN_NEW && lastLen >= MIN_OLD && textLen < lastLen * THRESHOLD) ||
      (lastLen > 0 && textLen < lastLen * 0.6) ||
      isLikelyNewIndependentSegment(currentText, text);

    if (shouldCommitPrevious && currentText) {
      pushArticleSegment(completed, currentText);
    }

    currentText = text;
    lastLen = textLen;
  }

  if (currentText) {
    pushArticleSegment(completed, currentText);
  }

  return completed;
}

function pushArticleSegment(segments, text) {
  const normalized = normalizeTranscriptText(text);
  if (!normalized) return;

  const last = segments[segments.length - 1] || "";
  if (last === normalized) return;
  if (last && normalized.startsWith(last)) {
    segments[segments.length - 1] = normalized;
    return;
  }
  if (last && last.startsWith(normalized)) {
    return;
  }

  segments.push(needsLeadingSpace(last, normalized) ? " " + normalized : normalized);
}

function isLikelyNewIndependentSegment(prev, next) {
  if (!prev || !next) return false;
  if (next.startsWith(prev) || prev.startsWith(next)) return false;
  if (prev.length < 8 || next.length < 8) return false;

  const prefixLen = commonPrefixLength(prev, next);
  const similarity = prefixLen / Math.min(prev.length, next.length);
  return similarity < 0.35;
}

function commonPrefixLength(a, b) {
  const limit = Math.min(a.length, b.length);
  let i = 0;
  while (i < limit && a[i] === b[i]) i++;
  return i;
}

function normalizeTranscriptText(text) {
  return String(text).replace(/\s+/g, " ").trim();
}

function needsLeadingSpace(prev, next) {
  if (!prev || !next) return false;
  if (/^[，。！？、；：,.!?;:）)]/.test(next)) return false;
  if (/[（(]$/.test(prev)) return false;
  return !/[，。！？、；：,.!?;:\s]$/.test(prev);
}

function escapeHtml(value) {
  const div = document.createElement("div");
  div.textContent = value;
  return div.innerHTML;
}

function connectWS() {
  const ws = new WebSocket("ws://" + location.host + "/ws");
  ws.onmessage = (event) => {
    const msg = JSON.parse(event.data);
    if (msg.type === "state_update") renderState(msg);
    if (msg.type === "transcript_delta") refreshLists();
    if (msg.type === "stt_error") console.error(msg.message);
  };
  ws.onclose = () => setTimeout(connectWS, 1000);
}

refreshAll();
connectWS();
</script>
</body>
</html>`
