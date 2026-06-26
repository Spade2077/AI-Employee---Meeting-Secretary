// 本文件负责保存会议结构化结果并处理去重合并。
package main

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

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
	if err := saveSummaryExtractionTx(ctx, tx, meetingID, result.Summary, sourceUtteranceCount, now); err != nil {
		return nil, err
	}

	existingDecisions, err := decisionsTx(ctx, tx, meetingID)
	if err != nil {
		return nil, err
	}
	existingDecisions, err = mergeExistingDecisionsTx(ctx, tx, existingDecisions, now)
	if err != nil {
		return nil, err
	}
	if err := saveDecisionExtractionsTx(ctx, tx, meetingID, existingDecisions, result.Decisions, now); err != nil {
		return nil, err
	}

	existingActionItems, err := actionItemsTx(ctx, tx, meetingID)
	if err != nil {
		return nil, err
	}
	existingActionItems, err = mergeExistingActionItemsTx(ctx, tx, existingActionItems, now)
	if err != nil {
		return nil, err
	}
	if err := saveActionItemExtractionsTx(ctx, tx, meetingID, existingActionItems, result.ActionItems, now); err != nil {
		return nil, err
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

func saveSummaryExtractionTx(ctx context.Context, tx *sql.Tx, meetingID, summary string, sourceUtteranceCount int, now string) error {
	summary = strings.TrimSpace(summary)
	if summary == "" {
		return nil
	}

	existingID := ""
	row := tx.QueryRowContext(ctx, `SELECT id FROM meeting_summaries WHERE meeting_id = ?`, meetingID)
	if err := row.Scan(&existingID); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if existingID == "" {
		_, err := tx.ExecContext(ctx, `INSERT INTO meeting_summaries
			(id, meeting_id, content, source_utterance_count, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?)`,
			newID("sum"), meetingID, summary, sourceUtteranceCount, now, now)
		return err
	}
	_, err := tx.ExecContext(ctx, `UPDATE meeting_summaries SET
		content = ?, source_utterance_count = ?, updated_at = ?
		WHERE meeting_id = ?`,
		summary, sourceUtteranceCount, now, meetingID)
	return err
}

func saveDecisionExtractionsTx(ctx context.Context, tx *sql.Tx, meetingID string, existingDecisions []MeetingDecision, decisions []DecisionExtraction, now string) error {
	for _, decision := range decisions {
		content := strings.TrimSpace(decision.Content)
		if content == "" || normalizeForCompare(content) == "" {
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
				return err
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
			return err
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
	return nil
}

func saveActionItemExtractionsTx(ctx context.Context, tx *sql.Tx, meetingID string, existingActionItems []MeetingActionItem, items []ActionItemExtraction, now string) error {
	for _, item := range items {
		task := strings.TrimSpace(item.Task)
		if task == "" || normalizeForCompare(task) == "" {
			continue
		}
		owner := strings.TrimSpace(item.Owner)
		dueDate := strings.TrimSpace(item.DueDate)
		status := strings.TrimSpace(item.Status)
		if status == "" {
			status = "open"
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
				return err
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
			return err
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
	return nil
}
