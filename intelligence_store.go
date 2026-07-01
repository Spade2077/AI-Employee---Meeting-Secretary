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
	risks, err := s.ListRisks(ctx, meetingID)
	if err != nil {
		return nil, err
	}
	openQuestions, err := s.ListOpenQuestions(ctx, meetingID)
	if err != nil {
		return nil, err
	}
	meeting, err := s.GetMeeting(ctx, meetingID)
	if err != nil {
		return nil, err
	}
	frameworkSections, err := s.ListFrameworkSections(ctx, meetingID, meeting.MeetingType)
	if err != nil {
		return nil, err
	}
	dynamicSections, err := s.ListDynamicSections(ctx, meetingID)
	if err != nil {
		return nil, err
	}
	missingAlerts := computeMissingFieldAlerts(actionItems, risks, openQuestions)
	return &IntelligenceSnapshot{
		Type:               "intelligence_update",
		MeetingID:          meetingID,
		Title:              meeting.Title,
		MeetingType:        meeting.MeetingType,
		Summary:            summary,
		Decisions:          decisions,
		ActionItems:        actionItems,
		Risks:              risks,
		OpenQuestions:      openQuestions,
		FrameworkSections:  frameworkSections,
		DynamicSections:    dynamicSections,
		MissingFieldAlerts: missingAlerts,
		Timestamp:          nowString(),
	}, nil
}

func (s *Store) SaveIntelligenceExtraction(ctx context.Context, meetingID string, result IntelligenceExtraction, sourceUtteranceCount int) (*IntelligenceSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// 摘要和会议信息是滚动快照；决策、待办、风险和待确认问题是稳定列表，
	// 入库时必须合并相似项，避免 LLM 每轮结构化都新增重复数据。
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	now := nowString()
	if err := saveMeetingMetaExtractionTx(ctx, tx, meetingID, result.Title, result.MeetingType, now); err != nil {
		return nil, err
	}
	meeting, err := getMeetingTx(ctx, tx, meetingID)
	if err != nil {
		return nil, err
	}
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

	existingRisks, err := risksTx(ctx, tx, meetingID)
	if err != nil {
		return nil, err
	}
	existingRisks, err = mergeExistingRisksTx(ctx, tx, existingRisks, now)
	if err != nil {
		return nil, err
	}
	if err := saveRiskExtractionsTx(ctx, tx, meetingID, existingRisks, result.Risks, now); err != nil {
		return nil, err
	}

	existingQuestions, err := openQuestionsTx(ctx, tx, meetingID)
	if err != nil {
		return nil, err
	}
	existingQuestions, err = mergeExistingOpenQuestionsTx(ctx, tx, existingQuestions, now)
	if err != nil {
		return nil, err
	}
	if err := saveOpenQuestionExtractionsTx(ctx, tx, meetingID, existingQuestions, result.OpenQuestions, now); err != nil {
		return nil, err
	}

	existingFrameworkSections, err := frameworkSectionsTx(ctx, tx, meetingID, meeting.MeetingType)
	if err != nil {
		return nil, err
	}
	if err := saveFrameworkSectionExtractionsTx(ctx, tx, meetingID, meeting.MeetingType, existingFrameworkSections, result.FrameworkSections, now); err != nil {
		return nil, err
	}

	existingDynamicSections, err := dynamicSectionsTx(ctx, tx, meetingID)
	if err != nil {
		return nil, err
	}
	if err := saveDynamicSectionExtractionsTx(ctx, tx, meetingID, existingDynamicSections, result.DynamicSections, now); err != nil {
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
	risks, err := risksTx(ctx, tx, meetingID)
	if err != nil {
		return err
	}
	if _, err := mergeExistingRisksTx(ctx, tx, risks, now); err != nil {
		return err
	}
	openQuestions, err := openQuestionsTx(ctx, tx, meetingID)
	if err != nil {
		return err
	}
	if _, err := mergeExistingOpenQuestionsTx(ctx, tx, openQuestions, now); err != nil {
		return err
	}
	return tx.Commit()
}

func saveSummaryExtractionTx(ctx context.Context, tx *sql.Tx, meetingID string, summary SummaryExtraction, sourceUtteranceCount int, now string) error {
	summary = normalizeSummaryExtraction(summary)
	if summary.IsEmpty() {
		return nil
	}
	content := strings.TrimSpace(summary.PlainText())

	existingID := ""
	row := tx.QueryRowContext(ctx, `SELECT id FROM meeting_summaries WHERE meeting_id = ?`, meetingID)
	if err := row.Scan(&existingID); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if existingID == "" {
		_, err := tx.ExecContext(ctx, `INSERT INTO meeting_summaries
			(id, meeting_id, content, brief, background, key_points, progress, changes, conclusion, next_follow_up,
			 source_utterance_count, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			newID("sum"), meetingID, content, summary.Brief, summary.Background, encodeStringList(summary.KeyPoints),
			summary.Progress, encodeStringList(summary.Changes), summary.Conclusion, summary.NextFollowUp,
			sourceUtteranceCount, now, now)
		return err
	}
	_, err := tx.ExecContext(ctx, `UPDATE meeting_summaries SET
		content = ?, brief = ?, background = ?, key_points = ?, progress = ?, changes = ?, conclusion = ?, next_follow_up = ?,
		source_utterance_count = ?, updated_at = ?
		WHERE meeting_id = ?`,
		content, summary.Brief, summary.Background, encodeStringList(summary.KeyPoints), summary.Progress,
		encodeStringList(summary.Changes), summary.Conclusion, summary.NextFollowUp, sourceUtteranceCount, now, meetingID)
	return err
}

func saveMeetingMetaExtractionTx(ctx context.Context, tx *sql.Tx, meetingID, title, meetingType, now string) error {
	title = normalizeMeetingTitleInput(title)
	meetingType = normalizeMeetingType(meetingType)
	if title == "" && meetingType == "unknown" {
		return nil
	}
	meeting, err := getMeetingTx(ctx, tx, meetingID)
	if err != nil {
		return err
	}
	nextTitle := meeting.Title
	nextType := meeting.MeetingType
	if title != "" && !meeting.TitleLocked && shouldUpdateMeetingTitle(meeting.Title, title) {
		nextTitle = title
	}
	if meetingType != "unknown" && !meeting.TypeLocked {
		nextType = meetingType
	}
	if nextTitle == meeting.Title && nextType == meeting.MeetingType {
		return nil
	}
	_, err = tx.ExecContext(ctx, `UPDATE meetings SET title = ?, type = ?, meeting_type = ?, updated_at = ? WHERE id = ?`,
		nextTitle, nextType, nextType, now, meetingID)
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
		sourceText := strings.TrimSpace(item.SourceText)
		if index := matchingActionItemIndexWithSource(existingActionItems, task, owner, dueDate, sourceText); index >= 0 {
			merged := existingActionItems[index]
			updatedTask := richerText(merged.Task, task)
			updatedOwner := preferRevisionText(merged.Owner, owner, sourceText)
			updatedDueDate := preferRevisionText(merged.DueDate, dueDate, sourceText)
			updatedStatus := preferFilledText(merged.Status, status)
			updatedSource := mergeSourceText(merged.SourceText, sourceText)
			updatedMissingFields := missingFieldsForActionItem(&MeetingActionItem{
				Task:    updatedTask,
				Owner:   updatedOwner,
				DueDate: updatedDueDate,
				Status:  updatedStatus,
			})
			if updatedTask == merged.Task &&
				updatedOwner == merged.Owner &&
				updatedDueDate == merged.DueDate &&
				updatedStatus == merged.Status &&
				updatedSource == merged.SourceText {
				continue
			}
			_, err := tx.ExecContext(ctx, `UPDATE meeting_action_items SET
				task = ?, owner = ?, due_date = ?, status = ?, missing_fields = ?, source_text = ?, updated_at = ?
				WHERE id = ?`,
				updatedTask, updatedOwner, updatedDueDate, updatedStatus,
				encodeStringList(updatedMissingFields), updatedSource, now, merged.ID)
			if err != nil {
				return err
			}
			merged.Task = updatedTask
			merged.Owner = updatedOwner
			merged.DueDate = updatedDueDate
			merged.Status = updatedStatus
			merged.MissingFields = updatedMissingFields
			merged.SourceText = updatedSource
			merged.UpdatedAt = now
			existingActionItems[index] = merged
			continue
		}
		actionItemID := newID("act")
		missingFields := missingFieldsForActionItem(&MeetingActionItem{
			Task:    task,
			Owner:   owner,
			DueDate: dueDate,
			Status:  status,
		})
		_, err := tx.ExecContext(ctx, `INSERT INTO meeting_action_items
			(id, meeting_id, task, owner, due_date, status, missing_fields, source_text, is_deleted, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, 0, ?, ?)`,
			actionItemID, meetingID, task, owner, dueDate, status, encodeStringList(missingFields),
			sourceText, now, now)
		if err != nil {
			return err
		}
		existingActionItems = append(existingActionItems, MeetingActionItem{
			ID:            actionItemID,
			MeetingID:     meetingID,
			Task:          task,
			Owner:         owner,
			DueDate:       dueDate,
			Status:        status,
			MissingFields: missingFields,
			SourceText:    sourceText,
			CreatedAt:     now,
			UpdatedAt:     now,
		})
	}
	return nil
}

func saveRiskExtractionsTx(ctx context.Context, tx *sql.Tx, meetingID string, existingRisks []MeetingRisk, risks []RiskExtraction, now string) error {
	for _, risk := range risks {
		content := strings.TrimSpace(risk.Content)
		if content == "" || normalizeForCompare(content) == "" {
			continue
		}
		impact := strings.TrimSpace(risk.Impact)
		level := normalizeRiskLevel(risk.Level)
		owner := strings.TrimSpace(risk.Owner)
		mitigation := strings.TrimSpace(risk.Mitigation)
		dueDate := strings.TrimSpace(risk.DueDate)
		status := normalizeStructuredStatus(risk.Status)
		sourceText := strings.TrimSpace(risk.SourceText)
		if index := matchingRiskIndex(existingRisks, content); index >= 0 {
			merged := existingRisks[index]
			updatedContent := richerText(merged.Content, content)
			updatedImpact := richerText(merged.Impact, impact)
			updatedLevel := preferRiskLevel(merged.Level, level)
			updatedOwner := preferRevisionText(merged.Owner, owner, sourceText)
			updatedMitigation := preferRevisionText(merged.Mitigation, mitigation, sourceText)
			updatedDueDate := preferRevisionText(merged.DueDate, dueDate, sourceText)
			updatedStatus := preferFilledText(merged.Status, status)
			updatedSource := mergeSourceText(merged.SourceText, sourceText)
			updatedMissing := missingFieldsForRisk(&MeetingRisk{
				Content:    updatedContent,
				Impact:     updatedImpact,
				Level:      updatedLevel,
				Owner:      updatedOwner,
				Mitigation: updatedMitigation,
				DueDate:    updatedDueDate,
				Status:     updatedStatus,
			})
			if updatedContent == merged.Content &&
				updatedImpact == merged.Impact &&
				updatedLevel == merged.Level &&
				updatedOwner == merged.Owner &&
				updatedMitigation == merged.Mitigation &&
				updatedDueDate == merged.DueDate &&
				updatedStatus == merged.Status &&
				updatedSource == merged.SourceText {
				continue
			}
			_, err := tx.ExecContext(ctx, `UPDATE meeting_risks SET
				content = ?, impact = ?, level = ?, owner = ?, mitigation = ?, due_date = ?, status = ?, missing_fields = ?, source_text = ?, updated_at = ?
				WHERE id = ?`,
				updatedContent, updatedImpact, updatedLevel, updatedOwner, updatedMitigation, updatedDueDate,
				updatedStatus, encodeStringList(updatedMissing), updatedSource, now, merged.ID)
			if err != nil {
				return err
			}
			merged.Content = updatedContent
			merged.Impact = updatedImpact
			merged.Level = updatedLevel
			merged.Owner = updatedOwner
			merged.Mitigation = updatedMitigation
			merged.DueDate = updatedDueDate
			merged.Status = updatedStatus
			merged.MissingFields = updatedMissing
			merged.SourceText = updatedSource
			merged.UpdatedAt = now
			existingRisks[index] = merged
			continue
		}
		riskID := newID("risk")
		missingFields := missingFieldsForRisk(&MeetingRisk{
			Content:    content,
			Impact:     impact,
			Level:      level,
			Owner:      owner,
			Mitigation: mitigation,
			DueDate:    dueDate,
			Status:     status,
		})
		_, err := tx.ExecContext(ctx, `INSERT INTO meeting_risks
			(id, meeting_id, content, impact, level, owner, mitigation, due_date, status, missing_fields, source_text, is_deleted, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0, ?, ?)`,
			riskID, meetingID, content, impact, level, owner, mitigation, dueDate, status,
			encodeStringList(missingFields), sourceText, now, now)
		if err != nil {
			return err
		}
		existingRisks = append(existingRisks, MeetingRisk{
			ID:            riskID,
			MeetingID:     meetingID,
			Content:       content,
			Impact:        impact,
			Level:         level,
			Owner:         owner,
			Mitigation:    mitigation,
			DueDate:       dueDate,
			Status:        status,
			MissingFields: missingFields,
			SourceText:    sourceText,
			CreatedAt:     now,
			UpdatedAt:     now,
		})
	}
	return nil
}

func saveOpenQuestionExtractionsTx(ctx context.Context, tx *sql.Tx, meetingID string, existingQuestions []MeetingOpenQuestion, questions []OpenQuestionExtraction, now string) error {
	for _, question := range questions {
		text := strings.TrimSpace(question.Question)
		if text == "" || normalizeForCompare(text) == "" {
			continue
		}
		owner := strings.TrimSpace(question.Owner)
		dueDate := strings.TrimSpace(question.DueDate)
		status := normalizeStructuredStatus(question.Status)
		sourceText := strings.TrimSpace(question.SourceText)
		if index := matchingOpenQuestionIndex(existingQuestions, text); index >= 0 {
			merged := existingQuestions[index]
			updatedQuestion := richerText(merged.Question, text)
			updatedOwner := preferRevisionText(merged.Owner, owner, sourceText)
			updatedDueDate := preferRevisionText(merged.DueDate, dueDate, sourceText)
			updatedStatus := preferFilledText(merged.Status, status)
			updatedSource := mergeSourceText(merged.SourceText, sourceText)
			updatedMissing := missingFieldsForOpenQuestion(&MeetingOpenQuestion{
				Question: updatedQuestion,
				Owner:    updatedOwner,
				DueDate:  updatedDueDate,
				Status:   updatedStatus,
			})
			if updatedQuestion == merged.Question &&
				updatedOwner == merged.Owner &&
				updatedDueDate == merged.DueDate &&
				updatedStatus == merged.Status &&
				updatedSource == merged.SourceText {
				continue
			}
			_, err := tx.ExecContext(ctx, `UPDATE meeting_open_questions SET
				question = ?, owner = ?, due_date = ?, status = ?, missing_fields = ?, source_text = ?, updated_at = ?
				WHERE id = ?`,
				updatedQuestion, updatedOwner, updatedDueDate, updatedStatus, encodeStringList(updatedMissing),
				updatedSource, now, merged.ID)
			if err != nil {
				return err
			}
			merged.Question = updatedQuestion
			merged.Owner = updatedOwner
			merged.DueDate = updatedDueDate
			merged.Status = updatedStatus
			merged.MissingFields = updatedMissing
			merged.SourceText = updatedSource
			merged.UpdatedAt = now
			existingQuestions[index] = merged
			continue
		}
		questionID := newID("q")
		missingFields := missingFieldsForOpenQuestion(&MeetingOpenQuestion{
			Question: text,
			Owner:    owner,
			DueDate:  dueDate,
			Status:   status,
		})
		_, err := tx.ExecContext(ctx, `INSERT INTO meeting_open_questions
			(id, meeting_id, question, owner, due_date, status, missing_fields, source_text, is_deleted, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, 0, ?, ?)`,
			questionID, meetingID, text, owner, dueDate, status, encodeStringList(missingFields), sourceText, now, now)
		if err != nil {
			return err
		}
		existingQuestions = append(existingQuestions, MeetingOpenQuestion{
			ID:            questionID,
			MeetingID:     meetingID,
			Question:      text,
			Owner:         owner,
			DueDate:       dueDate,
			Status:        status,
			MissingFields: missingFields,
			SourceText:    sourceText,
			CreatedAt:     now,
			UpdatedAt:     now,
		})
	}
	return nil
}
