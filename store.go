package main

import (
	"context"
	"database/sql"
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
			FOREIGN KEY(meeting_id) REFERENCES meetings(id)
		);`,
		`CREATE INDEX IF NOT EXISTS idx_state_events_meeting_created ON state_events(meeting_id, created_at);`,
		`CREATE INDEX IF NOT EXISTS idx_utterances_meeting_created ON utterances(meeting_id, created_at);`,
		`CREATE INDEX IF NOT EXISTS idx_decisions_meeting_created ON meeting_decisions(meeting_id, created_at);`,
		`CREATE INDEX IF NOT EXISTS idx_action_items_meeting_created ON meeting_action_items(meeting_id, created_at);`,
		`CREATE INDEX IF NOT EXISTS idx_interaction_events_meeting_created ON interaction_events(meeting_id, created_at);`,
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
	if useCase == "" {
		useCase = LLMUseCaseDefault
	}
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
			BaseURL: envForLLMUseCase(useCase, "BASE_URL"),
			apiKey:  envForLLMUseCase(useCase, "API_KEY"),
			Model:   envForLLMUseCase(useCase, "MODEL"),
		}
	}
	cfg.BaseURL = strings.TrimSpace(cfg.BaseURL)
	cfg.Model = strings.TrimSpace(cfg.Model)
	cfg.HasAPIKey = strings.TrimSpace(cfg.apiKey) != ""
	return cfg, nil
}

func (s *Store) getStoredLLMConfig(ctx context.Context, id string) (*LLMConfig, bool, error) {
	row := s.db.QueryRowContext(ctx, `SELECT base_url, api_key, model, updated_at FROM llm_config WHERE id = ?`, id)
	var cfg LLMConfig
	if err := row.Scan(&cfg.BaseURL, &cfg.apiKey, &cfg.Model, &cfg.UpdatedAt); err != nil {
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
	if useCase == "" {
		useCase = LLMUseCaseDefault
	}
	now := nowString()
	cfg.BaseURL = strings.TrimSpace(cfg.BaseURL)
	cfg.Model = strings.TrimSpace(cfg.Model)
	cfg.apiKey = strings.TrimSpace(cfg.apiKey)
	_, err := s.db.ExecContext(ctx, `INSERT INTO llm_config
		(id, base_url, api_key, model, updated_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			base_url = excluded.base_url,
			api_key = excluded.api_key,
			model = excluded.model,
			updated_at = excluded.updated_at`,
		string(useCase), cfg.BaseURL, cfg.apiKey, cfg.Model, now)
	if err != nil {
		return nil, err
	}
	return s.GetLLMConfigFor(ctx, useCase)
}

func (s *Store) GetIntelligenceSnapshot(ctx context.Context, meetingID string) (*IntelligenceSnapshot, error) {
	summary, err := s.getMeetingSummary(ctx, meetingID)
	if err != nil {
		return nil, err
	}
	decisions, err := s.ListDecisions(ctx, meetingID)
	if err != nil {
		return nil, err
	}
	actionItems, err := s.ListActionItems(ctx, meetingID)
	if err != nil {
		return nil, err
	}
	return &IntelligenceSnapshot{
		Type:        "intelligence_update",
		MeetingID:   meetingID,
		Summary:     summary,
		Decisions:   decisions,
		ActionItems: actionItems,
		Timestamp:   nowString(),
	}, nil
}

func (s *Store) SaveIntelligenceExtraction(ctx context.Context, meetingID string, result IntelligenceExtraction, sourceUtteranceCount int) (*IntelligenceSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Summary is a rolling snapshot, while decisions and action items are stable
	// lists. This is why summary is overwritten but structured items are merged.
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	now := nowString()
	summary := strings.TrimSpace(result.Summary)
	if summary != "" {
		existingID := ""
		row := tx.QueryRowContext(ctx, `SELECT id FROM meeting_summaries WHERE meeting_id = ?`, meetingID)
		if err := row.Scan(&existingID); err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		if existingID == "" {
			_, err = tx.ExecContext(ctx, `INSERT INTO meeting_summaries
				(id, meeting_id, content, source_utterance_count, created_at, updated_at)
				VALUES (?, ?, ?, ?, ?, ?)`,
				newID("sum"), meetingID, summary, sourceUtteranceCount, now, now)
		} else {
			_, err = tx.ExecContext(ctx, `UPDATE meeting_summaries SET
				content = ?, source_utterance_count = ?, updated_at = ?
				WHERE meeting_id = ?`,
				summary, sourceUtteranceCount, now, meetingID)
		}
		if err != nil {
			return nil, err
		}
	}

	existingDecisions, err := decisionsTx(ctx, tx, meetingID)
	if err != nil {
		return nil, err
	}
	existingDecisions, err = mergeExistingDecisionsTx(ctx, tx, existingDecisions, now)
	if err != nil {
		return nil, err
	}
	for _, decision := range result.Decisions {
		content := strings.TrimSpace(decision.Content)
		if content == "" {
			continue
		}
		if normalizeForCompare(content) == "" {
			continue
		}
		if index := matchingDecisionIndex(existingDecisions, content); index >= 0 {
			merged := existingDecisions[index]
			updatedContent := richerText(merged.Content, content)
			updatedSource := richerText(merged.SourceText, strings.TrimSpace(decision.SourceText))
			if updatedContent == merged.Content && updatedSource == merged.SourceText {
				continue
			}
			_, err := tx.ExecContext(ctx, `UPDATE meeting_decisions SET
				content = ?, source_text = ?, updated_at = ?
				WHERE id = ?`,
				updatedContent, updatedSource, now, merged.ID)
			if err != nil {
				return nil, err
			}
			merged.Content = updatedContent
			merged.SourceText = updatedSource
			merged.UpdatedAt = now
			existingDecisions[index] = merged
			continue
		}
		decisionID := newID("dec")
		_, err := tx.ExecContext(ctx, `INSERT INTO meeting_decisions
			(id, meeting_id, content, source_text, is_deleted, created_at, updated_at)
			VALUES (?, ?, ?, ?, 0, ?, ?)`,
			decisionID, meetingID, content, strings.TrimSpace(decision.SourceText), now, now)
		if err != nil {
			return nil, err
		}
		existingDecisions = append(existingDecisions, MeetingDecision{
			ID:         decisionID,
			MeetingID:  meetingID,
			Content:    content,
			SourceText: strings.TrimSpace(decision.SourceText),
			CreatedAt:  now,
			UpdatedAt:  now,
		})
	}

	existingActionItems, err := actionItemsTx(ctx, tx, meetingID)
	if err != nil {
		return nil, err
	}
	existingActionItems, err = mergeExistingActionItemsTx(ctx, tx, existingActionItems, now)
	if err != nil {
		return nil, err
	}
	for _, item := range result.ActionItems {
		task := strings.TrimSpace(item.Task)
		if task == "" {
			continue
		}
		owner := strings.TrimSpace(item.Owner)
		dueDate := strings.TrimSpace(item.DueDate)
		status := strings.TrimSpace(item.Status)
		if status == "" {
			status = "open"
		}
		if normalizeForCompare(task) == "" {
			continue
		}
		if index := matchingActionItemIndex(existingActionItems, task, owner, dueDate); index >= 0 {
			merged := existingActionItems[index]
			updatedTask := richerText(merged.Task, task)
			updatedOwner := preferFilledText(merged.Owner, owner)
			updatedDueDate := preferFilledText(merged.DueDate, dueDate)
			updatedStatus := preferFilledText(merged.Status, status)
			updatedSource := richerText(merged.SourceText, strings.TrimSpace(item.SourceText))
			if updatedTask == merged.Task &&
				updatedOwner == merged.Owner &&
				updatedDueDate == merged.DueDate &&
				updatedStatus == merged.Status &&
				updatedSource == merged.SourceText {
				continue
			}
			_, err := tx.ExecContext(ctx, `UPDATE meeting_action_items SET
				task = ?, owner = ?, due_date = ?, status = ?, source_text = ?, updated_at = ?
				WHERE id = ?`,
				updatedTask, updatedOwner, updatedDueDate, updatedStatus, updatedSource, now, merged.ID)
			if err != nil {
				return nil, err
			}
			merged.Task = updatedTask
			merged.Owner = updatedOwner
			merged.DueDate = updatedDueDate
			merged.Status = updatedStatus
			merged.SourceText = updatedSource
			merged.UpdatedAt = now
			existingActionItems[index] = merged
			continue
		}
		actionItemID := newID("act")
		_, err := tx.ExecContext(ctx, `INSERT INTO meeting_action_items
			(id, meeting_id, task, owner, due_date, status, source_text, is_deleted, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, 0, ?, ?)`,
			actionItemID, meetingID, task, owner, dueDate, status, strings.TrimSpace(item.SourceText), now, now)
		if err != nil {
			return nil, err
		}
		existingActionItems = append(existingActionItems, MeetingActionItem{
			ID:         actionItemID,
			MeetingID:  meetingID,
			Task:       task,
			Owner:      owner,
			DueDate:    dueDate,
			Status:     status,
			SourceText: strings.TrimSpace(item.SourceText),
			CreatedAt:  now,
			UpdatedAt:  now,
		})
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.GetIntelligenceSnapshot(ctx, meetingID)
}

func (s *Store) CleanupIntelligenceDuplicates(ctx context.Context, meetingID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	now := nowString()
	decisions, err := decisionsTx(ctx, tx, meetingID)
	if err != nil {
		return err
	}
	if _, err := mergeExistingDecisionsTx(ctx, tx, decisions, now); err != nil {
		return err
	}
	actionItems, err := actionItemsTx(ctx, tx, meetingID)
	if err != nil {
		return err
	}
	if _, err := mergeExistingActionItemsTx(ctx, tx, actionItems, now); err != nil {
		return err
	}
	return tx.Commit()
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

func (s *Store) getActionItem(ctx context.Context, meetingID, actionItemID string) (*MeetingActionItem, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, meeting_id, task, owner, due_date, status, source_text, is_deleted, created_at, updated_at
		FROM meeting_action_items WHERE meeting_id = ? AND id = ? AND is_deleted = 0`, meetingID, actionItemID)
	return scanActionItem(row)
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
	_, err := s.db.ExecContext(ctx, `INSERT INTO interaction_events
		(id, meeting_id, raw_transcript, wake_word, instruction, intent, target_type, target_index, target_id,
		 command_json, execution_status, message, error_message, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		event.ID, event.MeetingID, event.RawTranscript, event.WakeWord, event.Instruction, event.Intent,
		event.TargetType, event.TargetIndex, event.TargetID, event.CommandJSON, event.ExecutionStatus,
		event.Message, event.ErrorMessage, event.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &event, nil
}

func (s *Store) ListInteractionEvents(ctx context.Context, meetingID string) ([]InteractionEvent, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, meeting_id, raw_transcript, wake_word, instruction, intent,
		target_type, target_index, target_id, command_json, execution_status, message, error_message, created_at
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
	if err := scanner.Scan(&event.ID, &event.MeetingID, &event.RawTranscript, &event.WakeWord,
		&event.Instruction, &event.Intent, &event.TargetType, &event.TargetIndex, &event.TargetID,
		&event.CommandJSON, &event.ExecutionStatus, &event.Message, &event.ErrorMessage, &event.CreatedAt); err != nil {
		return nil, err
	}
	return &event, nil
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
