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
	Type         string             `json:"type"`
	ActionItem   *MeetingActionItem `json:"action_item,omitempty"`
	ActionItemID string             `json:"action_item_id,omitempty"`
	Decision     *MeetingDecision   `json:"decision,omitempty"`
	DecisionID   string             `json:"decision_id,omitempty"`
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

	default:
		return "这一步操作暂时不支持撤销。", fmt.Errorf("unsupported undo payload type: %s", payload.Type)
	}
}
