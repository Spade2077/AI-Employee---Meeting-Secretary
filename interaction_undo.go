// 本文件负责会中交互操作的撤销记录、反向操作和撤销执行。
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
)

type interactionUndoPayload struct {
	Type           string                `json:"type"`
	Meeting        *Meeting              `json:"meeting,omitempty"`
	ActionItem     *MeetingActionItem    `json:"action_item,omitempty"`
	ActionItemID   string                `json:"action_item_id,omitempty"`
	Decision       *MeetingDecision      `json:"decision,omitempty"`
	DecisionID     string                `json:"decision_id,omitempty"`
	Risk           *MeetingRisk          `json:"risk,omitempty"`
	RiskID         string                `json:"risk_id,omitempty"`
	Risks          []MeetingRisk         `json:"risks,omitempty"`
	OpenQuestion   *MeetingOpenQuestion  `json:"open_question,omitempty"`
	OpenQuestionID string                `json:"open_question_id,omitempty"`
	OpenQuestions  []MeetingOpenQuestion `json:"open_questions,omitempty"`
}

func (s *Server) saveUndoRecord(ctx context.Context, meetingID, eventID string, payload interactionUndoPayload) error {
	payload.Type = strings.TrimSpace(payload.Type)
	if payload.Type == "" {
		return fmt.Errorf("undo payload type is required")
	}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = s.store.SaveUndoRecord(ctx, InteractionUndoRecord{
		MeetingID:          meetingID,
		InteractionEventID: eventID,
		UndoType:           payload.Type,
		UndoJSON:           string(payloadBytes),
	})
	return err
}

func (s *Server) rememberUndoRecord(ctx context.Context, meetingID, eventID string, payload interactionUndoPayload) {
	if err := s.saveUndoRecord(ctx, meetingID, eventID, payload); err != nil {
		log.Printf("save undo record failed meeting=%s event=%s type=%s: %v", meetingID, eventID, payload.Type, err)
	}
}

func (s *Server) undoLastInteraction(ctx context.Context, meetingID, eventID string) (*IntelligenceSnapshot, string, error) {
	record, err := s.store.LatestUnusedUndoRecord(ctx, meetingID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, "没有可撤销的会中交互操作。", fmt.Errorf("no undoable interaction")
	}
	if err != nil {
		return nil, "读取撤销记录失败。", err
	}

	var payload interactionUndoPayload
	if err := json.Unmarshal([]byte(record.UndoJSON), &payload); err != nil {
		return nil, "撤销记录解析失败。", err
	}
	message, err := s.applyUndoPayload(ctx, meetingID, payload)
	if err != nil {
		return nil, message, err
	}
	if err := s.store.MarkUndoRecordUsed(ctx, record.ID, eventID); err != nil {
		return nil, "撤销操作已执行，但标记撤销记录失败。", err
	}
	return s.snapshotWithMessage(ctx, meetingID, message)
}

func (s *Server) applyUndoPayload(ctx context.Context, meetingID string, payload interactionUndoPayload) (string, error) {
	switch strings.TrimSpace(payload.Type) {
	case "restore_meeting_title":
		if payload.Meeting == nil {
			return "撤销失败：缺少会议标题快照。", fmt.Errorf("undo meeting snapshot missing")
		}
		meeting, err := s.store.UpdateMeetingTitle(ctx, meetingID, payload.Meeting.Title, payload.Meeting.TitleLocked)
		if err != nil {
			return "撤销会议标题变更失败。", err
		}
		return fmt.Sprintf("已撤销上一步操作，会议标题恢复为：%s", meeting.Title), nil

	case "restore_action_item":
		if payload.ActionItem == nil {
			return "撤销失败：缺少待办快照。", fmt.Errorf("undo action item snapshot missing")
		}
		payload.ActionItem.MeetingID = meetingID
		item, err := s.store.RestoreActionItem(ctx, *payload.ActionItem)
		if err != nil {
			return "撤销待办变更失败。", err
		}
		return fmt.Sprintf("已撤销上一步操作，恢复待办：%s", item.Task), nil

	case "delete_action_item":
		actionItemID := strings.TrimSpace(payload.ActionItemID)
		if actionItemID == "" && payload.ActionItem != nil {
			actionItemID = payload.ActionItem.ID
		}
		if actionItemID == "" {
			return "撤销失败：缺少待办 ID。", fmt.Errorf("undo action item id missing")
		}
		item, err := s.store.DeleteActionItem(ctx, meetingID, actionItemID)
		if err != nil {
			return "撤销新增待办失败。", err
		}
		return fmt.Sprintf("已撤销上一步操作，删除刚刚新增的待办：%s", item.Task), nil

	case "restore_decision":
		if payload.Decision == nil {
			return "撤销失败：缺少决策快照。", fmt.Errorf("undo decision snapshot missing")
		}
		payload.Decision.MeetingID = meetingID
		decision, err := s.store.RestoreDecision(ctx, *payload.Decision)
		if err != nil {
			return "撤销决策变更失败。", err
		}
		return fmt.Sprintf("已撤销上一步操作，恢复决策：%s", decision.Content), nil

	case "delete_decision":
		decisionID := strings.TrimSpace(payload.DecisionID)
		if decisionID == "" && payload.Decision != nil {
			decisionID = payload.Decision.ID
		}
		if decisionID == "" {
			return "撤销失败：缺少决策 ID。", fmt.Errorf("undo decision id missing")
		}
		decision, err := s.store.DeleteDecision(ctx, meetingID, decisionID)
		if err != nil {
			return "撤销新增决策失败。", err
		}
		return fmt.Sprintf("已撤销上一步操作，删除刚刚新增的决策：%s", decision.Content), nil

	case "restore_risk":
		if payload.Risk == nil {
			return "撤销失败：缺少风险快照。", fmt.Errorf("undo risk snapshot missing")
		}
		payload.Risk.MeetingID = meetingID
		risk, err := s.store.RestoreRisk(ctx, *payload.Risk)
		if err != nil {
			return "撤销风险变更失败。", err
		}
		return fmt.Sprintf("已撤销上一步操作，恢复风险：%s", risk.Content), nil

	case "delete_risk":
		riskID := strings.TrimSpace(payload.RiskID)
		if riskID == "" && payload.Risk != nil {
			riskID = payload.Risk.ID
		}
		if riskID == "" {
			return "撤销失败：缺少风险 ID。", fmt.Errorf("undo risk id missing")
		}
		risk, err := s.store.DeleteRisk(ctx, meetingID, riskID)
		if err != nil {
			return "撤销新增风险失败。", err
		}
		return fmt.Sprintf("已撤销上一步操作，删除刚刚新增的风险：%s", risk.Content), nil

	case "restore_risk_set":
		if len(payload.Risks) == 0 {
			return "撤销失败：缺少风险快照。", fmt.Errorf("undo risk set missing")
		}
		for _, risk := range payload.Risks {
			risk.MeetingID = meetingID
			if _, err := s.store.RestoreRisk(ctx, risk); err != nil {
				return "撤销合并风险失败。", err
			}
		}
		return fmt.Sprintf("已撤销上一步操作，恢复 %d 条风险。", len(payload.Risks)), nil

	case "restore_open_question":
		if payload.OpenQuestion == nil {
			return "撤销失败：缺少待确认问题快照。", fmt.Errorf("undo open question snapshot missing")
		}
		payload.OpenQuestion.MeetingID = meetingID
		question, err := s.store.RestoreOpenQuestion(ctx, *payload.OpenQuestion)
		if err != nil {
			return "撤销待确认问题变更失败。", err
		}
		return fmt.Sprintf("已撤销上一步操作，恢复待确认问题：%s", question.Question), nil

	case "delete_open_question":
		questionID := strings.TrimSpace(payload.OpenQuestionID)
		if questionID == "" && payload.OpenQuestion != nil {
			questionID = payload.OpenQuestion.ID
		}
		if questionID == "" {
			return "撤销失败：缺少待确认问题 ID。", fmt.Errorf("undo open question id missing")
		}
		question, err := s.store.DeleteOpenQuestion(ctx, meetingID, questionID)
		if err != nil {
			return "撤销新增待确认问题失败。", err
		}
		return fmt.Sprintf("已撤销上一步操作，删除刚刚新增的待确认问题：%s", question.Question), nil

	case "restore_open_question_set":
		if len(payload.OpenQuestions) == 0 {
			return "撤销失败：缺少待确认问题快照。", fmt.Errorf("undo open question set missing")
		}
		for _, question := range payload.OpenQuestions {
			question.MeetingID = meetingID
			if _, err := s.store.RestoreOpenQuestion(ctx, question); err != nil {
				return "撤销合并待确认问题失败。", err
			}
		}
		return fmt.Sprintf("已撤销上一步操作，恢复 %d 条待确认问题。", len(payload.OpenQuestions)), nil

	default:
		return "这一步操作暂时不支持撤销。", fmt.Errorf("unsupported undo payload type: %s", payload.Type)
	}
}
