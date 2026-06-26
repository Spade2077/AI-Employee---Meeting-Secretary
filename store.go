// 本文件负责 SQLite 初始化和核心数据读写。
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"meeting-core-service/internal/stt"
)

func (s *Store) Init(ctx context.Context) error {
	// The MVP keeps every meeting artifact local: state transitions, transcript,
	// LLM settings, and structured intelligence output.
	schema := []string{
		`PRAGMA journal_mode = WAL;`,
		`CREATE TABLE IF NOT EXISTS meetings (
			id TEXT PRIMARY KEY,
			title TEXT NOT NULL DEFAULT '',
			type TEXT NOT NULL DEFAULT 'unknown',
			status TEXT NOT NULL,
			previous_status TEXT NOT NULL DEFAULT '',
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
		`CREATE TABLE IF NOT EXISTS llm_config (
			id TEXT PRIMARY KEY,
			base_url TEXT NOT NULL DEFAULT '',
			api_key TEXT NOT NULL DEFAULT '',
			model TEXT NOT NULL DEFAULT '',
			thinking_mode TEXT NOT NULL DEFAULT 'default',
			updated_at TEXT NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS meeting_summaries (
			id TEXT PRIMARY KEY,
			meeting_id TEXT NOT NULL UNIQUE,
			content TEXT NOT NULL DEFAULT '',
			source_utterance_count INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			FOREIGN KEY(meeting_id) REFERENCES meetings(id)
		);`,
		`CREATE TABLE IF NOT EXISTS meeting_decisions (
			id TEXT PRIMARY KEY,
			meeting_id TEXT NOT NULL,
			content TEXT NOT NULL,
			source_text TEXT NOT NULL DEFAULT '',
			is_deleted INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			FOREIGN KEY(meeting_id) REFERENCES meetings(id)
		);`,
		`CREATE TABLE IF NOT EXISTS meeting_action_items (
			id TEXT PRIMARY KEY,
			meeting_id TEXT NOT NULL,
			task TEXT NOT NULL,
			owner TEXT NOT NULL DEFAULT '',
			due_date TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'open',
			source_text TEXT NOT NULL DEFAULT '',
			is_deleted INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			FOREIGN KEY(meeting_id) REFERENCES meetings(id)
		);`,
		`CREATE TABLE IF NOT EXISTS interaction_events (
			id TEXT PRIMARY KEY,
			meeting_id TEXT NOT NULL,
			raw_transcript TEXT NOT NULL DEFAULT '',
			wake_word TEXT NOT NULL DEFAULT '',
			instruction TEXT NOT NULL DEFAULT '',
			intent TEXT NOT NULL DEFAULT '',
			target_type TEXT NOT NULL DEFAULT '',
			target_index INTEGER NOT NULL DEFAULT 0,
			target_id TEXT NOT NULL DEFAULT '',
			command_json TEXT NOT NULL DEFAULT '',
			execution_status TEXT NOT NULL DEFAULT '',
			message TEXT NOT NULL DEFAULT '',
			error_message TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			completed_at TEXT NOT NULL DEFAULT '',
			duration_ms INTEGER NOT NULL DEFAULT 0,
			timing_json TEXT NOT NULL DEFAULT '',
			FOREIGN KEY(meeting_id) REFERENCES meetings(id)
		);`,
		`CREATE TABLE IF NOT EXISTS interaction_undo_records (
			id TEXT PRIMARY KEY,
			meeting_id TEXT NOT NULL,
			interaction_event_id TEXT NOT NULL,
			undo_type TEXT NOT NULL,
			undo_json TEXT NOT NULL,
			is_used INTEGER NOT NULL DEFAULT 0,
			used_by_event_id TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			used_at TEXT NOT NULL DEFAULT '',
			FOREIGN KEY(meeting_id) REFERENCES meetings(id)
		);`,
		`CREATE INDEX IF NOT EXISTS idx_state_events_meeting_created ON state_events(meeting_id, created_at);`,
		`CREATE INDEX IF NOT EXISTS idx_utterances_meeting_created ON utterances(meeting_id, created_at);`,
		`CREATE INDEX IF NOT EXISTS idx_decisions_meeting_created ON meeting_decisions(meeting_id, created_at);`,
		`CREATE INDEX IF NOT EXISTS idx_action_items_meeting_created ON meeting_action_items(meeting_id, created_at);`,
		`CREATE INDEX IF NOT EXISTS idx_interaction_events_meeting_created ON interaction_events(meeting_id, created_at);`,
		`CREATE INDEX IF NOT EXISTS idx_interaction_undo_meeting_created ON interaction_undo_records(meeting_id, created_at);`,
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
	if err := s.ensureColumn(ctx, "llm_config", "thinking_mode", "TEXT NOT NULL DEFAULT 'default'"); err != nil {
		return err
	}
	if err := s.ensureColumn(ctx, "interaction_events", "completed_at", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := s.ensureColumn(ctx, "interaction_events", "duration_ms", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	if err := s.ensureColumn(ctx, "interaction_events", "timing_json", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE meetings SET status = ?, updated_at = ? WHERE status = 'hard_muted'`, StatusSoftPaused, nowString()); err != nil {
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

func (s *Store) GetLLMConfig(ctx context.Context) (*LLMConfig, error) {
	return s.GetLLMConfigFor(ctx, LLMUseCaseDefault)
}

func (s *Store) GetLLMConfigFor(ctx context.Context, useCase LLMUseCase) (*LLMConfig, error) {
	useCase = normalizeLLMUseCase(string(useCase))
	cfg, found, err := s.getStoredLLMConfig(ctx, string(useCase))
	if err != nil {
		return nil, err
	}
	if !found && useCase != LLMUseCaseDefault {
		cfg, found, err = s.getStoredLLMConfig(ctx, string(LLMUseCaseDefault))
		if err != nil {
			return nil, err
		}
	}
	if !found {
		cfg = &LLMConfig{
			BaseURL:      envForLLMUseCase(useCase, "BASE_URL"),
			apiKey:       envForLLMUseCase(useCase, "API_KEY"),
			Model:        envForLLMUseCase(useCase, "MODEL"),
			ThinkingMode: envForLLMUseCase(useCase, "THINKING_MODE"),
		}
	}
	cfg.BaseURL = strings.TrimSpace(cfg.BaseURL)
	cfg.Model = strings.TrimSpace(cfg.Model)
	cfg.ThinkingMode = normalizeLLMThinkingMode(cfg.ThinkingMode)
	cfg.UseCase = useCase
	cfg.HasAPIKey = strings.TrimSpace(cfg.apiKey) != ""
	return cfg, nil
}

func (s *Store) getStoredLLMConfig(ctx context.Context, id string) (*LLMConfig, bool, error) {
	row := s.db.QueryRowContext(ctx, `SELECT base_url, api_key, model, thinking_mode, updated_at FROM llm_config WHERE id = ?`, id)
	var cfg LLMConfig
	if err := row.Scan(&cfg.BaseURL, &cfg.apiKey, &cfg.Model, &cfg.ThinkingMode, &cfg.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return &cfg, true, nil
}

func (s *Store) SaveLLMConfig(ctx context.Context, cfg LLMConfig) (*LLMConfig, error) {
	return s.SaveLLMConfigFor(ctx, LLMUseCaseDefault, cfg)
}

func (s *Store) SaveLLMConfigFor(ctx context.Context, useCase LLMUseCase, cfg LLMConfig) (*LLMConfig, error) {
	useCase = normalizeLLMUseCase(string(useCase))
	now := nowString()
	cfg.BaseURL = strings.TrimSpace(cfg.BaseURL)
	cfg.Model = strings.TrimSpace(cfg.Model)
	cfg.ThinkingMode = normalizeLLMThinkingMode(cfg.ThinkingMode)
	cfg.apiKey = strings.TrimSpace(cfg.apiKey)
	_, err := s.db.ExecContext(ctx, `INSERT INTO llm_config
		(id, base_url, api_key, model, thinking_mode, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			base_url = excluded.base_url,
			api_key = excluded.api_key,
			model = excluded.model,
			thinking_mode = excluded.thinking_mode,
			updated_at = excluded.updated_at`,
		string(useCase), cfg.BaseURL, cfg.apiKey, cfg.Model, cfg.ThinkingMode, now)
	if err != nil {
		return nil, err
	}
	return s.GetLLMConfigFor(ctx, useCase)
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
	row := s.db.QueryRowContext(ctx, `SELECT id, title, type, status, previous_status,
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
	row := s.db.QueryRowContext(ctx, `SELECT id, title, type, status, previous_status,
		COALESCE(started_at, ''), COALESCE(ended_at, ''), duration_seconds, created_at, updated_at
		FROM meetings WHERE id = ?`, meetingID)
	return scanMeeting(row)
}

func scanMeeting(scanner interface {
	Scan(dest ...any) error
}) (*Meeting, error) {
	var m Meeting
	if err := scanner.Scan(&m.ID, &m.Title, &m.Type, &m.Status, &m.PreviousStatus,
		&m.StartedAt, &m.EndedAt, &m.DurationSeconds, &m.CreatedAt, &m.UpdatedAt); err != nil {
		return nil, err
	}
	if m.EndedAt == "" && m.StartedAt != "" && (m.Status == string(StatusRecording) || m.Status == string(StatusSoftPaused)) {
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

	switch eventType {
	case "start":
		if startedAt == "" {
			startedAt = now
		}
	case "finish":
		endedAt = now
		duration = computeDurationSeconds(startedAt, now)
	}

	if _, err := tx.ExecContext(ctx, `UPDATE meetings SET
		status = ?, previous_status = ?, started_at = NULLIF(?, ''),
		ended_at = NULLIF(?, ''), duration_seconds = ?, updated_at = ?
		WHERE id = ?`,
		to, from, startedAt, endedAt, duration, now, meetingID); err != nil {
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
	case "ending":
		if from == StatusRecording || from == StatusSoftPaused || from == StatusStandby {
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
	row := tx.QueryRowContext(ctx, `SELECT id, title, type, status, previous_status,
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
		text = "manual transcript segment"
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
	s.mu.Lock()
	defer s.mu.Unlock()

	if strings.TrimSpace(result.Text) == "" {
		return nil, fmt.Errorf("transcript text is empty")
	}
	if strings.TrimSpace(result.SpeakerLabel) == "" {
		result.SpeakerLabel = "unknown"
	}
	if strings.TrimSpace(result.Provider) == "" {
		result.Provider = "unknown"
	}
	nowTime := time.Now().UTC()
	now := nowTime.Format(time.RFC3339Nano)

	if result.IsFinal {
		latest, err := s.latestUtteranceForMerge(ctx, result.MeetingID, result.SpeakerLabel, result.Provider)
		if err != nil {
			return nil, err
		}
		if latest != nil && shouldMergeTranscriptUtterance(latest, result.Text, nowTime) {
			updatedText := richerText(latest.Text, result.Text)
			confidence := latest.Confidence
			if result.Confidence > confidence {
				confidence = result.Confidence
			}
			_, err := s.db.ExecContext(ctx, `UPDATE utterances SET text = ?, confidence = ?, is_final = 1 WHERE id = ?`,
				updatedText, confidence, latest.ID)
			if err != nil {
				return nil, err
			}
			latest.Text = updatedText
			latest.Confidence = confidence
			latest.IsFinal = true
			return latest, nil
		}
	}

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

func (s *Store) latestUtteranceForMerge(ctx context.Context, meetingID, speakerLabel, provider string) (*Utterance, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, meeting_id, speaker_label, text, is_final, provider, confidence,
		is_deleted, created_at, COALESCE(deleted_at, '')
		FROM utterances
		WHERE meeting_id = ? AND speaker_label = ? AND provider = ? AND is_deleted = 0
		ORDER BY created_at DESC LIMIT 1`, meetingID, speakerLabel, provider)
	utterance, err := scanUtterance(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return utterance, err
}

func shouldMergeTranscriptUtterance(previous *Utterance, incomingText string, now time.Time) bool {
	if previous == nil {
		return false
	}
	previousKey := normalizeForCompare(previous.Text)
	incomingKey := normalizeForCompare(incomingText)
	if previousKey == "" || incomingKey == "" {
		return false
	}
	if previousKey == incomingKey {
		return true
	}
	if !similarStructuredText(previous.Text, incomingText) {
		return false
	}
	createdAt, err := time.Parse(time.RFC3339Nano, previous.CreatedAt)
	if err != nil {
		return false
	}
	return now.Sub(createdAt) <= 30*time.Second
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

func (s *Store) getMeetingSummary(ctx context.Context, meetingID string) (*MeetingSummary, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, meeting_id, content, source_utterance_count, created_at, updated_at
		FROM meeting_summaries WHERE meeting_id = ?`, meetingID)
	var summary MeetingSummary
	if err := row.Scan(&summary.ID, &summary.MeetingID, &summary.Content, &summary.SourceUtteranceCount, &summary.CreatedAt, &summary.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &summary, nil
}

func (s *Store) ListDecisions(ctx context.Context, meetingID string) ([]MeetingDecision, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, meeting_id, content, source_text, is_deleted, created_at, updated_at
		FROM meeting_decisions WHERE meeting_id = ? AND is_deleted = 0 ORDER BY created_at ASC`, meetingID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var decisions []MeetingDecision
	for rows.Next() {
		d, err := scanDecision(rows)
		if err != nil {
			return nil, err
		}
		decisions = append(decisions, *d)
	}
	return decisions, rows.Err()
}

func (s *Store) ListActionItems(ctx context.Context, meetingID string) ([]MeetingActionItem, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, meeting_id, task, owner, due_date, status, source_text, is_deleted, created_at, updated_at
		FROM meeting_action_items WHERE meeting_id = ? AND is_deleted = 0 ORDER BY created_at ASC`, meetingID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []MeetingActionItem
	for rows.Next() {
		item, err := scanActionItem(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

func (s *Store) DeleteActionItem(ctx context.Context, meetingID, actionItemID string) (*MeetingActionItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	item, err := s.getActionItem(ctx, meetingID, actionItemID)
	if err != nil {
		return nil, err
	}
	now := nowString()
	if _, err := s.db.ExecContext(ctx, `UPDATE meeting_action_items SET is_deleted = 1, updated_at = ? WHERE meeting_id = ? AND id = ?`,
		now, meetingID, actionItemID); err != nil {
		return nil, err
	}
	item.IsDeleted = true
	item.UpdatedAt = now
	return item, nil
}

func (s *Store) RestoreActionItem(ctx context.Context, item MeetingActionItem) (*MeetingActionItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if strings.TrimSpace(item.ID) == "" || strings.TrimSpace(item.MeetingID) == "" {
		return nil, fmt.Errorf("action item id and meeting id are required")
	}
	if strings.TrimSpace(item.Task) == "" {
		return nil, fmt.Errorf("action item task is required")
	}
	now := nowString()
	if item.CreatedAt == "" {
		item.CreatedAt = now
	}
	item.UpdatedAt = now
	item.Status = normalizeActionItemStatus(item.Status)
	_, err := s.db.ExecContext(ctx, `INSERT INTO meeting_action_items
		(id, meeting_id, task, owner, due_date, status, source_text, is_deleted, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, 0, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			task = excluded.task,
			owner = excluded.owner,
			due_date = excluded.due_date,
			status = excluded.status,
			source_text = excluded.source_text,
			is_deleted = 0,
			updated_at = excluded.updated_at`,
		item.ID, item.MeetingID, item.Task, item.Owner, item.DueDate, item.Status, item.SourceText, item.CreatedAt, item.UpdatedAt)
	if err != nil {
		return nil, err
	}
	item.IsDeleted = false
	return &item, nil
}

func (s *Store) CreateActionItem(ctx context.Context, meetingID string, mutation ActionItemMutation) (*MeetingActionItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	task := strings.TrimSpace(mutation.Task)
	if task == "" {
		return nil, fmt.Errorf("action item task is required")
	}
	status := normalizeActionItemStatus(mutation.Status)
	now := nowString()
	item := &MeetingActionItem{
		ID:         newID("act"),
		MeetingID:  meetingID,
		Task:       task,
		Owner:      strings.TrimSpace(mutation.Owner),
		DueDate:    strings.TrimSpace(mutation.DueDate),
		Status:     status,
		SourceText: strings.TrimSpace(mutation.SourceText),
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO meeting_action_items
		(id, meeting_id, task, owner, due_date, status, source_text, is_deleted, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, 0, ?, ?)`,
		item.ID, item.MeetingID, item.Task, item.Owner, item.DueDate, item.Status, item.SourceText, item.CreatedAt, item.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return item, nil
}

func (s *Store) UpdateActionItem(ctx context.Context, meetingID, actionItemID string, mutation ActionItemMutation) (*MeetingActionItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	item, err := s.getActionItem(ctx, meetingID, actionItemID)
	if err != nil {
		return nil, err
	}
	if value := strings.TrimSpace(mutation.Task); value != "" {
		item.Task = value
	}
	if value := strings.TrimSpace(mutation.Owner); value != "" {
		item.Owner = value
	}
	if value := strings.TrimSpace(mutation.DueDate); value != "" {
		item.DueDate = value
	}
	if value := strings.TrimSpace(mutation.Status); value != "" {
		item.Status = normalizeActionItemStatus(value)
	}
	if value := strings.TrimSpace(mutation.SourceText); value != "" {
		item.SourceText = richerText(item.SourceText, value)
	}
	if strings.TrimSpace(item.Task) == "" {
		return nil, fmt.Errorf("action item task cannot be empty")
	}
	item.UpdatedAt = nowString()
	_, err = s.db.ExecContext(ctx, `UPDATE meeting_action_items SET
		task = ?, owner = ?, due_date = ?, status = ?, source_text = ?, updated_at = ?
		WHERE meeting_id = ? AND id = ? AND is_deleted = 0`,
		item.Task, item.Owner, item.DueDate, item.Status, item.SourceText, item.UpdatedAt, meetingID, actionItemID)
	if err != nil {
		return nil, err
	}
	return item, nil
}

func (s *Store) CompleteActionItem(ctx context.Context, meetingID, actionItemID string) (*MeetingActionItem, error) {
	return s.UpdateActionItem(ctx, meetingID, actionItemID, ActionItemMutation{Status: "completed"})
}

func (s *Store) CreateDecision(ctx context.Context, meetingID, content, sourceText string) (*MeetingDecision, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	content = strings.TrimSpace(content)
	if content == "" {
		return nil, fmt.Errorf("decision content is required")
	}
	now := nowString()
	decision := &MeetingDecision{
		ID:         newID("dec"),
		MeetingID:  meetingID,
		Content:    content,
		SourceText: strings.TrimSpace(sourceText),
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO meeting_decisions
		(id, meeting_id, content, source_text, is_deleted, created_at, updated_at)
		VALUES (?, ?, ?, ?, 0, ?, ?)`,
		decision.ID, decision.MeetingID, decision.Content, decision.SourceText, decision.CreatedAt, decision.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return decision, nil
}

func (s *Store) UpdateDecision(ctx context.Context, meetingID, decisionID, content string) (*MeetingDecision, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	decision, err := s.getDecision(ctx, meetingID, decisionID)
	if err != nil {
		return nil, err
	}
	content = strings.TrimSpace(content)
	if content == "" {
		return nil, fmt.Errorf("decision content is required")
	}
	decision.Content = content
	decision.UpdatedAt = nowString()
	_, err = s.db.ExecContext(ctx, `UPDATE meeting_decisions SET content = ?, updated_at = ?
		WHERE meeting_id = ? AND id = ? AND is_deleted = 0`,
		decision.Content, decision.UpdatedAt, meetingID, decisionID)
	if err != nil {
		return nil, err
	}
	return decision, nil
}

func (s *Store) DeleteDecision(ctx context.Context, meetingID, decisionID string) (*MeetingDecision, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	decision, err := s.getDecision(ctx, meetingID, decisionID)
	if err != nil {
		return nil, err
	}
	now := nowString()
	if _, err := s.db.ExecContext(ctx, `UPDATE meeting_decisions SET is_deleted = 1, updated_at = ? WHERE meeting_id = ? AND id = ?`,
		now, meetingID, decisionID); err != nil {
		return nil, err
	}
	decision.IsDeleted = true
	decision.UpdatedAt = now
	return decision, nil
}

func (s *Store) RestoreDecision(ctx context.Context, decision MeetingDecision) (*MeetingDecision, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if strings.TrimSpace(decision.ID) == "" || strings.TrimSpace(decision.MeetingID) == "" {
		return nil, fmt.Errorf("decision id and meeting id are required")
	}
	if strings.TrimSpace(decision.Content) == "" {
		return nil, fmt.Errorf("decision content is required")
	}
	now := nowString()
	if decision.CreatedAt == "" {
		decision.CreatedAt = now
	}
	decision.UpdatedAt = now
	_, err := s.db.ExecContext(ctx, `INSERT INTO meeting_decisions
		(id, meeting_id, content, source_text, is_deleted, created_at, updated_at)
		VALUES (?, ?, ?, ?, 0, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			content = excluded.content,
			source_text = excluded.source_text,
			is_deleted = 0,
			updated_at = excluded.updated_at`,
		decision.ID, decision.MeetingID, decision.Content, decision.SourceText, decision.CreatedAt, decision.UpdatedAt)
	if err != nil {
		return nil, err
	}
	decision.IsDeleted = false
	return &decision, nil
}

func (s *Store) MergeDecisions(ctx context.Context, meetingID string, decisionIDs []string, content string) (*MeetingDecision, []MeetingDecision, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	decisionIDs = uniqueNonEmptyStrings(decisionIDs)
	if len(decisionIDs) < 2 {
		return nil, nil, fmt.Errorf("at least two decisions are required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, err
	}
	defer tx.Rollback()

	decisions := make([]MeetingDecision, 0, len(decisionIDs))
	for _, id := range decisionIDs {
		decision, err := getDecisionTx(ctx, tx, meetingID, id)
		if err != nil {
			return nil, nil, err
		}
		decisions = append(decisions, *decision)
	}
	merged := decisions[0]
	content = strings.TrimSpace(content)
	if content != "" {
		merged.Content = content
	} else {
		values := make([]string, 0, len(decisions))
		for _, decision := range decisions {
			values = append(values, decision.Content)
		}
		merged.Content = mergeDistinctText(values...)
	}
	for _, decision := range decisions[1:] {
		merged.SourceText = richerText(merged.SourceText, decision.SourceText)
	}
	if strings.TrimSpace(merged.Content) == "" {
		return nil, nil, fmt.Errorf("merged decision content cannot be empty")
	}
	now := nowString()
	merged.UpdatedAt = now
	if _, err := tx.ExecContext(ctx, `UPDATE meeting_decisions SET content = ?, source_text = ?, updated_at = ?
		WHERE meeting_id = ? AND id = ? AND is_deleted = 0`,
		merged.Content, merged.SourceText, merged.UpdatedAt, meetingID, merged.ID); err != nil {
		return nil, nil, err
	}
	for _, decision := range decisions[1:] {
		if _, err := tx.ExecContext(ctx, `UPDATE meeting_decisions SET is_deleted = 1, updated_at = ? WHERE meeting_id = ? AND id = ?`,
			now, meetingID, decision.ID); err != nil {
			return nil, nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, nil, err
	}
	return &merged, decisions[1:], nil
}

func (s *Store) MoveDecisionToActionItem(ctx context.Context, meetingID, decisionID string, mutation ActionItemMutation) (*MeetingDecision, *MeetingActionItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, err
	}
	defer tx.Rollback()

	decision, err := getDecisionTx(ctx, tx, meetingID, decisionID)
	if err != nil {
		return nil, nil, err
	}
	task := strings.TrimSpace(mutation.Task)
	if task == "" {
		task = decision.Content
	}
	if task == "" {
		return nil, nil, fmt.Errorf("action item task is required")
	}
	now := nowString()
	item := &MeetingActionItem{
		ID:         newID("act"),
		MeetingID:  meetingID,
		Task:       task,
		Owner:      strings.TrimSpace(mutation.Owner),
		DueDate:    strings.TrimSpace(mutation.DueDate),
		Status:     normalizeActionItemStatus(mutation.Status),
		SourceText: richerText(decision.SourceText, decision.Content),
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO meeting_action_items
		(id, meeting_id, task, owner, due_date, status, source_text, is_deleted, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, 0, ?, ?)`,
		item.ID, item.MeetingID, item.Task, item.Owner, item.DueDate, item.Status, item.SourceText, item.CreatedAt, item.UpdatedAt); err != nil {
		return nil, nil, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE meeting_decisions SET is_deleted = 1, updated_at = ? WHERE meeting_id = ? AND id = ?`,
		now, meetingID, decisionID); err != nil {
		return nil, nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, nil, err
	}
	decision.IsDeleted = true
	decision.UpdatedAt = now
	return decision, item, nil
}

func (s *Store) MoveActionItemToDecision(ctx context.Context, meetingID, actionItemID, content string) (*MeetingActionItem, *MeetingDecision, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, err
	}
	defer tx.Rollback()

	item, err := getActionItemTx(ctx, tx, meetingID, actionItemID)
	if err != nil {
		return nil, nil, err
	}
	decisionContent := strings.TrimSpace(content)
	if decisionContent == "" {
		decisionContent = item.Task
	}
	if decisionContent == "" {
		return nil, nil, fmt.Errorf("decision content is required")
	}
	now := nowString()
	decision := &MeetingDecision{
		ID:         newID("dec"),
		MeetingID:  meetingID,
		Content:    decisionContent,
		SourceText: richerText(item.SourceText, item.Task),
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO meeting_decisions
		(id, meeting_id, content, source_text, is_deleted, created_at, updated_at)
		VALUES (?, ?, ?, ?, 0, ?, ?)`,
		decision.ID, decision.MeetingID, decision.Content, decision.SourceText, decision.CreatedAt, decision.UpdatedAt); err != nil {
		return nil, nil, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE meeting_action_items SET is_deleted = 1, updated_at = ? WHERE meeting_id = ? AND id = ?`,
		now, meetingID, actionItemID); err != nil {
		return nil, nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, nil, err
	}
	item.IsDeleted = true
	item.UpdatedAt = now
	return item, decision, nil
}

func (s *Store) MergeActionItems(ctx context.Context, meetingID string, actionItemIDs []string, mutation ActionItemMutation) (*MeetingActionItem, []MeetingActionItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	actionItemIDs = uniqueNonEmptyStrings(actionItemIDs)
	if len(actionItemIDs) < 2 {
		return nil, nil, fmt.Errorf("at least two action items are required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, err
	}
	defer tx.Rollback()

	items := make([]MeetingActionItem, 0, len(actionItemIDs))
	for _, id := range actionItemIDs {
		item, err := getActionItemTx(ctx, tx, meetingID, id)
		if err != nil {
			return nil, nil, err
		}
		items = append(items, *item)
	}
	target := items[0]
	merged := target
	if value := strings.TrimSpace(mutation.Task); value != "" {
		merged.Task = value
	} else {
		values := make([]string, 0, len(items))
		for _, item := range items {
			values = append(values, item.Task)
		}
		merged.Task = mergeDistinctText(values...)
	}
	if value := strings.TrimSpace(mutation.Owner); value != "" {
		merged.Owner = value
	} else {
		for _, item := range items[1:] {
			merged.Owner = preferFilledText(merged.Owner, item.Owner)
		}
	}
	if value := strings.TrimSpace(mutation.DueDate); value != "" {
		merged.DueDate = value
	} else {
		for _, item := range items[1:] {
			merged.DueDate = preferFilledText(merged.DueDate, item.DueDate)
		}
	}
	if value := strings.TrimSpace(mutation.Status); value != "" {
		merged.Status = normalizeActionItemStatus(value)
	} else {
		merged.Status = normalizeActionItemStatus(merged.Status)
	}
	if value := strings.TrimSpace(mutation.SourceText); value != "" {
		merged.SourceText = richerText(merged.SourceText, value)
	}
	for _, item := range items[1:] {
		merged.SourceText = richerText(merged.SourceText, item.SourceText)
	}
	if strings.TrimSpace(merged.Task) == "" {
		return nil, nil, fmt.Errorf("merged action item task cannot be empty")
	}
	now := nowString()
	merged.UpdatedAt = now
	if _, err := tx.ExecContext(ctx, `UPDATE meeting_action_items SET
		task = ?, owner = ?, due_date = ?, status = ?, source_text = ?, updated_at = ?
		WHERE meeting_id = ? AND id = ? AND is_deleted = 0`,
		merged.Task, merged.Owner, merged.DueDate, merged.Status, merged.SourceText, merged.UpdatedAt, meetingID, merged.ID); err != nil {
		return nil, nil, err
	}
	for _, item := range items[1:] {
		if _, err := tx.ExecContext(ctx, `UPDATE meeting_action_items SET is_deleted = 1, updated_at = ? WHERE meeting_id = ? AND id = ?`,
			now, meetingID, item.ID); err != nil {
			return nil, nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, nil, err
	}
	return &merged, items[1:], nil
}

func (s *Store) RewriteSummary(ctx context.Context, meetingID, content string) (*MeetingSummary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	content = strings.TrimSpace(content)
	if content == "" {
		return nil, fmt.Errorf("summary content is required")
	}
	now := nowString()
	existing, err := s.getMeetingSummary(ctx, meetingID)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		summary := &MeetingSummary{
			ID:        newID("sum"),
			MeetingID: meetingID,
			Content:   content,
			CreatedAt: now,
			UpdatedAt: now,
		}
		_, err := s.db.ExecContext(ctx, `INSERT INTO meeting_summaries
			(id, meeting_id, content, source_utterance_count, created_at, updated_at)
			VALUES (?, ?, ?, 0, ?, ?)`,
			summary.ID, meetingID, summary.Content, summary.CreatedAt, summary.UpdatedAt)
		if err != nil {
			return nil, err
		}
		return summary, nil
	}
	existing.Content = content
	existing.UpdatedAt = now
	_, err = s.db.ExecContext(ctx, `UPDATE meeting_summaries SET content = ?, updated_at = ? WHERE meeting_id = ?`,
		existing.Content, existing.UpdatedAt, meetingID)
	if err != nil {
		return nil, err
	}
	return existing, nil
}

func (s *Store) getActionItem(ctx context.Context, meetingID, actionItemID string) (*MeetingActionItem, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, meeting_id, task, owner, due_date, status, source_text, is_deleted, created_at, updated_at
		FROM meeting_action_items WHERE meeting_id = ? AND id = ? AND is_deleted = 0`, meetingID, actionItemID)
	return scanActionItem(row)
}

func (s *Store) getDecision(ctx context.Context, meetingID, decisionID string) (*MeetingDecision, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, meeting_id, content, source_text, is_deleted, created_at, updated_at
		FROM meeting_decisions WHERE meeting_id = ? AND id = ? AND is_deleted = 0`, meetingID, decisionID)
	return scanDecision(row)
}

func getActionItemTx(ctx context.Context, tx *sql.Tx, meetingID, actionItemID string) (*MeetingActionItem, error) {
	row := tx.QueryRowContext(ctx, `SELECT id, meeting_id, task, owner, due_date, status, source_text, is_deleted, created_at, updated_at
		FROM meeting_action_items WHERE meeting_id = ? AND id = ? AND is_deleted = 0`, meetingID, actionItemID)
	return scanActionItem(row)
}

func getDecisionTx(ctx context.Context, tx *sql.Tx, meetingID, decisionID string) (*MeetingDecision, error) {
	row := tx.QueryRowContext(ctx, `SELECT id, meeting_id, content, source_text, is_deleted, created_at, updated_at
		FROM meeting_decisions WHERE meeting_id = ? AND id = ? AND is_deleted = 0`, meetingID, decisionID)
	return scanDecision(row)
}

func normalizeActionItemStatus(value string) string {
	key := strings.ToLower(strings.TrimSpace(value))
	switch key {
	case "", "open", "todo", "pending":
		return "open"
	case "done", "complete", "completed", "closed":
		return "completed"
	case "blocked":
		return "blocked"
	default:
		return strings.TrimSpace(value)
	}
}

func mergeDistinctText(values ...string) string {
	parts := mergeDistinctFields(values...)
	return strings.Join(parts, "；")
}

func mergeDistinctFields(values ...string) []string {
	parts := []string{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		shouldAppend := true
		for index, existing := range parts {
			if sameMergeText(existing, value) {
				shouldAppend = false
				break
			}
			if strings.Contains(existing, value) {
				shouldAppend = false
				break
			}
			if strings.Contains(value, existing) {
				parts[index] = value
				shouldAppend = false
				break
			}
		}
		if shouldAppend {
			parts = append(parts, value)
		}
	}
	return parts
}

func sameMergeText(left, right string) bool {
	leftKey := normalizeForCompare(left)
	rightKey := normalizeForCompare(right)
	return leftKey != "" && leftKey == rightKey
}

func uniqueNonEmptyStrings(values []string) []string {
	seen := map[string]bool{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	return result
}

func (s *Store) SaveInteractionEvent(ctx context.Context, event InteractionEvent) (*InteractionEvent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if event.ID == "" {
		event.ID = newID("ie")
	}
	if event.CreatedAt == "" {
		event.CreatedAt = nowString()
	}
	timingJSON, err := marshalInteractionTiming(event.Timing)
	if err != nil {
		return nil, err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO interaction_events
		(id, meeting_id, raw_transcript, wake_word, instruction, intent, target_type, target_index, target_id,
		 command_json, execution_status, message, error_message, created_at, completed_at, duration_ms, timing_json)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		event.ID, event.MeetingID, event.RawTranscript, event.WakeWord, event.Instruction, event.Intent,
		event.TargetType, event.TargetIndex, event.TargetID, event.CommandJSON, event.ExecutionStatus,
		event.Message, event.ErrorMessage, event.CreatedAt, event.CompletedAt, event.DurationMs, timingJSON)
	if err != nil {
		return nil, err
	}
	return &event, nil
}

func (s *Store) ListInteractionEvents(ctx context.Context, meetingID string) ([]InteractionEvent, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, meeting_id, raw_transcript, wake_word, instruction, intent,
		target_type, target_index, target_id, command_json, execution_status, message, error_message, created_at,
		completed_at, duration_ms, timing_json
		FROM interaction_events WHERE meeting_id = ? ORDER BY created_at ASC`, meetingID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	events := []InteractionEvent{}
	for rows.Next() {
		event, err := scanInteractionEvent(rows)
		if err != nil {
			return nil, err
		}
		events = append(events, *event)
	}
	return events, rows.Err()
}

func (s *Store) UpdateInteractionEventTiming(ctx context.Context, eventID string, timing *InteractionTiming) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	timingJSON, err := marshalInteractionTiming(timing)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `UPDATE interaction_events SET timing_json = ? WHERE id = ?`, timingJSON, eventID)
	return err
}

func (s *Store) SaveUndoRecord(ctx context.Context, record InteractionUndoRecord) (*InteractionUndoRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if record.ID == "" {
		record.ID = newID("undo")
	}
	if record.CreatedAt == "" {
		record.CreatedAt = nowString()
	}
	isUsed := 0
	if record.IsUsed {
		isUsed = 1
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO interaction_undo_records
		(id, meeting_id, interaction_event_id, undo_type, undo_json, is_used, used_by_event_id, created_at, used_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		record.ID, record.MeetingID, record.InteractionEventID, record.UndoType, record.UndoJSON,
		isUsed, record.UsedByEventID, record.CreatedAt, record.UsedAt)
	if err != nil {
		return nil, err
	}
	return &record, nil
}

func (s *Store) LatestUnusedUndoRecord(ctx context.Context, meetingID string) (*InteractionUndoRecord, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, meeting_id, interaction_event_id, undo_type, undo_json,
		is_used, used_by_event_id, created_at, used_at
		FROM interaction_undo_records
		WHERE meeting_id = ? AND is_used = 0
		ORDER BY created_at DESC
		LIMIT 1`, meetingID)
	return scanUndoRecord(row)
}

func (s *Store) MarkUndoRecordUsed(ctx context.Context, undoID, usedByEventID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.ExecContext(ctx, `UPDATE interaction_undo_records
		SET is_used = 1, used_by_event_id = ?, used_at = ?
		WHERE id = ? AND is_used = 0`,
		usedByEventID, nowString(), undoID)
	return err
}

func scanDecision(scanner interface {
	Scan(dest ...any) error
}) (*MeetingDecision, error) {
	var d MeetingDecision
	var deleted int
	if err := scanner.Scan(&d.ID, &d.MeetingID, &d.Content, &d.SourceText, &deleted, &d.CreatedAt, &d.UpdatedAt); err != nil {
		return nil, err
	}
	d.IsDeleted = deleted != 0
	return &d, nil
}

func scanActionItem(scanner interface {
	Scan(dest ...any) error
}) (*MeetingActionItem, error) {
	var item MeetingActionItem
	var deleted int
	if err := scanner.Scan(&item.ID, &item.MeetingID, &item.Task, &item.Owner, &item.DueDate, &item.Status, &item.SourceText, &deleted, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return nil, err
	}
	item.IsDeleted = deleted != 0
	return &item, nil
}

func scanInteractionEvent(scanner interface {
	Scan(dest ...any) error
}) (*InteractionEvent, error) {
	var event InteractionEvent
	var timingJSON string
	if err := scanner.Scan(&event.ID, &event.MeetingID, &event.RawTranscript, &event.WakeWord,
		&event.Instruction, &event.Intent, &event.TargetType, &event.TargetIndex, &event.TargetID,
		&event.CommandJSON, &event.ExecutionStatus, &event.Message, &event.ErrorMessage, &event.CreatedAt,
		&event.CompletedAt, &event.DurationMs, &timingJSON); err != nil {
		return nil, err
	}
	timing, err := unmarshalInteractionTiming(timingJSON)
	if err != nil {
		return nil, err
	}
	event.Timing = timing
	return &event, nil
}

func marshalInteractionTiming(timing *InteractionTiming) (string, error) {
	if timing == nil {
		return "", nil
	}
	bytes, err := json.Marshal(timing)
	if err != nil {
		return "", err
	}
	return string(bytes), nil
}

func unmarshalInteractionTiming(value string) (*InteractionTiming, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	var timing InteractionTiming
	if err := json.Unmarshal([]byte(value), &timing); err != nil {
		return nil, err
	}
	return &timing, nil
}

func scanUndoRecord(scanner interface {
	Scan(dest ...any) error
}) (*InteractionUndoRecord, error) {
	var record InteractionUndoRecord
	var used int
	if err := scanner.Scan(&record.ID, &record.MeetingID, &record.InteractionEventID, &record.UndoType,
		&record.UndoJSON, &used, &record.UsedByEventID, &record.CreatedAt, &record.UsedAt); err != nil {
		return nil, err
	}
	record.IsUsed = used != 0
	return &record, nil
}

func decisionsTx(ctx context.Context, tx *sql.Tx, meetingID string) ([]MeetingDecision, error) {
	rows, err := tx.QueryContext(ctx, `SELECT id, meeting_id, content, source_text, is_deleted, created_at, updated_at
		FROM meeting_decisions WHERE meeting_id = ? AND is_deleted = 0 ORDER BY created_at ASC`, meetingID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var decisions []MeetingDecision
	for rows.Next() {
		decision, err := scanDecision(rows)
		if err != nil {
			return nil, err
		}
		decisions = append(decisions, *decision)
	}
	return decisions, rows.Err()
}

func actionItemsTx(ctx context.Context, tx *sql.Tx, meetingID string) ([]MeetingActionItem, error) {
	rows, err := tx.QueryContext(ctx, `SELECT id, meeting_id, task, owner, due_date, status, source_text, is_deleted, created_at, updated_at
		FROM meeting_action_items WHERE meeting_id = ? AND is_deleted = 0 ORDER BY created_at ASC`, meetingID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []MeetingActionItem
	for rows.Next() {
		item, err := scanActionItem(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

func mergeExistingDecisionsTx(ctx context.Context, tx *sql.Tx, decisions []MeetingDecision, now string) ([]MeetingDecision, error) {
	merged := make([]MeetingDecision, 0, len(decisions))
	for _, decision := range decisions {
		if index := matchingDecisionIndex(merged, decision.Content); index >= 0 {
			target := merged[index]
			updatedContent := richerText(target.Content, decision.Content)
			updatedSource := richerText(target.SourceText, decision.SourceText)
			if updatedContent != target.Content || updatedSource != target.SourceText {
				if _, err := tx.ExecContext(ctx, `UPDATE meeting_decisions SET
					content = ?, source_text = ?, updated_at = ?
					WHERE id = ?`,
					updatedContent, updatedSource, now, target.ID); err != nil {
					return nil, err
				}
				target.Content = updatedContent
				target.SourceText = updatedSource
				target.UpdatedAt = now
				merged[index] = target
			}
			if _, err := tx.ExecContext(ctx, `UPDATE meeting_decisions SET is_deleted = 1, updated_at = ? WHERE id = ?`, now, decision.ID); err != nil {
				return nil, err
			}
			continue
		}
		merged = append(merged, decision)
	}
	return merged, nil
}

func mergeExistingActionItemsTx(ctx context.Context, tx *sql.Tx, items []MeetingActionItem, now string) ([]MeetingActionItem, error) {
	merged := make([]MeetingActionItem, 0, len(items))
	for _, item := range items {
		if index := matchingActionItemIndex(merged, item.Task, item.Owner, item.DueDate); index >= 0 {
			target := merged[index]
			updatedTask := richerText(target.Task, item.Task)
			updatedOwner := preferFilledText(target.Owner, item.Owner)
			updatedDueDate := preferFilledText(target.DueDate, item.DueDate)
			updatedStatus := preferFilledText(target.Status, item.Status)
			updatedSource := richerText(target.SourceText, item.SourceText)
			if updatedTask != target.Task ||
				updatedOwner != target.Owner ||
				updatedDueDate != target.DueDate ||
				updatedStatus != target.Status ||
				updatedSource != target.SourceText {
				if _, err := tx.ExecContext(ctx, `UPDATE meeting_action_items SET
					task = ?, owner = ?, due_date = ?, status = ?, source_text = ?, updated_at = ?
					WHERE id = ?`,
					updatedTask, updatedOwner, updatedDueDate, updatedStatus, updatedSource, now, target.ID); err != nil {
					return nil, err
				}
				target.Task = updatedTask
				target.Owner = updatedOwner
				target.DueDate = updatedDueDate
				target.Status = updatedStatus
				target.SourceText = updatedSource
				target.UpdatedAt = now
				merged[index] = target
			}
			if _, err := tx.ExecContext(ctx, `UPDATE meeting_action_items SET is_deleted = 1, updated_at = ? WHERE id = ?`, now, item.ID); err != nil {
				return nil, err
			}
			continue
		}
		merged = append(merged, item)
	}
	return merged, nil
}

func matchingDecisionIndex(existing []MeetingDecision, content string) int {
	for index, decision := range existing {
		if similarStructuredText(decision.Content, content) {
			return index
		}
	}
	return -1
}

func matchingActionItemIndex(existing []MeetingActionItem, task, owner, dueDate string) int {
	for index, item := range existing {
		if !similarStructuredText(item.Task, task) {
			continue
		}
		taskScore := structuredTextSimilarity(item.Task, task)
		if !sameOptionalField(item.Owner, owner) && taskScore < 0.78 {
			continue
		}
		if !sameOptionalField(item.DueDate, dueDate) && normalizeForCompare(item.DueDate) != "" && normalizeForCompare(dueDate) != "" {
			continue
		}
		return index
	}
	return -1
}

func sameOptionalField(left, right string) bool {
	leftKey := normalizeForCompare(left)
	rightKey := normalizeForCompare(right)
	return leftKey == "" || rightKey == "" || leftKey == rightKey
}

func richerText(existing, incoming string) string {
	existing = strings.TrimSpace(existing)
	incoming = strings.TrimSpace(incoming)
	if incoming == "" {
		return existing
	}
	if existing == "" {
		return incoming
	}
	existingLen := len([]rune(normalizeForCompare(existing)))
	incomingLen := len([]rune(normalizeForCompare(incoming)))
	if incomingLen > existingLen {
		return incoming
	}
	return existing
}

func preferFilledText(existing, incoming string) string {
	existing = strings.TrimSpace(existing)
	incoming = strings.TrimSpace(incoming)
	if existing == "" && incoming != "" {
		return incoming
	}
	return existing
}
