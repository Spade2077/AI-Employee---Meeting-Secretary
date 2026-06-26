// 本文件负责执行会中交互命令，并把解析后的意图落到具体数据库操作。
package main

import (
	"context"
	"fmt"
	"strings"
)

func (s *Server) executeInteractionCommand(ctx context.Context, meetingID string, command InteractionCommand) (*IntelligenceSnapshot, string, error) {
	return s.executeInteractionCommandWithEventID(ctx, meetingID, command, newID("ie"))
}

func (s *Server) executeInteractionCommandWithEventID(ctx context.Context, meetingID string, command InteractionCommand, eventID string) (*IntelligenceSnapshot, string, error) {
	command = normalizeInteractionCommand(command)
	if command.NeedConfirm {
		return nil, "这条指令还需要确认目标。", fmt.Errorf("interaction target needs confirmation: %s", command.Reason)
	}

	switch command.Intent {
	case "delete_action_item":
		index, item, err := s.resolveActionItemTarget(ctx, meetingID, command)
		if err != nil {
			return nil, "没有找到要删除的待办。", err
		}
		deleted, err := s.store.DeleteActionItem(ctx, meetingID, item.ID)
		if err != nil {
			return nil, "删除待办失败。", err
		}
		s.rememberUndoRecord(ctx, meetingID, eventID, interactionUndoPayload{
			Type:       "restore_action_item",
			ActionItem: item,
		})
		return s.snapshotWithMessage(ctx, meetingID, fmt.Sprintf("已删除第 %d 条待办：%s", index, deleted.Task))

	case "update_action_item":
		index, item, err := s.resolveActionItemTarget(ctx, meetingID, command)
		if err != nil {
			return nil, "没有找到要更新的待办。", err
		}
		mutation := actionItemMutationFromCommand(command)
		if mutation.Task == "" && mutation.Owner == "" && mutation.DueDate == "" && mutation.Status == "" {
			return nil, "没有识别到要修改的待办字段。", fmt.Errorf("empty action item update")
		}
		updated, err := s.store.UpdateActionItem(ctx, meetingID, item.ID, mutation)
		if err != nil {
			return nil, "更新待办失败。", err
		}
		s.rememberUndoRecord(ctx, meetingID, eventID, interactionUndoPayload{
			Type:       "restore_action_item",
			ActionItem: item,
		})
		return s.snapshotWithMessage(ctx, meetingID, fmt.Sprintf("已更新第 %d 条待办：%s", index, updated.Task))

	case "create_action_item":
		mutation := actionItemMutationFromCommand(command)
		if mutation.Task == "" {
			return nil, "没有识别到新待办内容。", fmt.Errorf("create action item missing task")
		}
		created, err := s.store.CreateActionItem(ctx, meetingID, mutation)
		if err != nil {
			return nil, "创建待办失败。", err
		}
		s.rememberUndoRecord(ctx, meetingID, eventID, interactionUndoPayload{
			Type:         "delete_action_item",
			ActionItemID: created.ID,
		})
		return s.snapshotWithMessage(ctx, meetingID, fmt.Sprintf("已新增待办：%s", created.Task))

	case "complete_action_item":
		index, item, err := s.resolveActionItemTarget(ctx, meetingID, command)
		if err != nil {
			return nil, "没有找到要完成的待办。", err
		}
		completed, err := s.store.CompleteActionItem(ctx, meetingID, item.ID)
		if err != nil {
			return nil, "标记待办完成失败。", err
		}
		s.rememberUndoRecord(ctx, meetingID, eventID, interactionUndoPayload{
			Type:       "restore_action_item",
			ActionItem: item,
		})
		return s.snapshotWithMessage(ctx, meetingID, fmt.Sprintf("已完成第 %d 条待办：%s", index, completed.Task))

	case "move_action_item_to_decision":
		index, item, err := s.resolveActionItemTarget(ctx, meetingID, command)
		if err != nil {
			return nil, "没有找到要转为决策的待办。", err
		}
		_, decision, err := s.store.MoveActionItemToDecision(ctx, meetingID, item.ID, command.Content)
		if err != nil {
			return nil, "待办转决策失败。", err
		}
		s.rememberUndoRecord(ctx, meetingID, eventID, interactionUndoPayload{Type: "unsupported_operation"})
		return s.snapshotWithMessage(ctx, meetingID, fmt.Sprintf("已将第 %d 条待办转为决策：%s", index, decision.Content))

	case "merge_action_items":
		ids, err := s.resolveMergeActionItemIDs(ctx, meetingID, command)
		if err != nil {
			return nil, "没有找到要合并的待办。", err
		}
		mutation := actionItemMutationFromCommand(command)
		merged, removed, err := s.store.MergeActionItems(ctx, meetingID, ids, mutation)
		if err != nil {
			return nil, "合并待办失败。", err
		}
		s.rememberUndoRecord(ctx, meetingID, eventID, interactionUndoPayload{Type: "unsupported_operation"})
		return s.snapshotWithMessage(ctx, meetingID, fmt.Sprintf("已合并 %d 条待办为：%s", len(removed)+1, merged.Task))

	case "delete_decision":
		index, decision, err := s.resolveDecisionTarget(ctx, meetingID, command)
		if err != nil {
			return nil, "No decision target found.", err
		}
		deleted, err := s.store.DeleteDecision(ctx, meetingID, decision.ID)
		if err != nil {
			return nil, "Delete decision failed.", err
		}
		s.rememberUndoRecord(ctx, meetingID, eventID, interactionUndoPayload{
			Type:     "restore_decision",
			Decision: decision,
		})
		return s.snapshotWithMessage(ctx, meetingID, fmt.Sprintf("Deleted decision %d: %s", index, deleted.Content))

	case "update_decision":
		index, decision, err := s.resolveDecisionTarget(ctx, meetingID, command)
		if err != nil {
			return nil, "No decision target found.", err
		}
		content := strings.TrimSpace(command.Content)
		if content == "" {
			return nil, "No new decision content found.", fmt.Errorf("update decision missing content")
		}
		updated, err := s.store.UpdateDecision(ctx, meetingID, decision.ID, content)
		if err != nil {
			return nil, "Update decision failed.", err
		}
		s.rememberUndoRecord(ctx, meetingID, eventID, interactionUndoPayload{
			Type:     "restore_decision",
			Decision: decision,
		})
		return s.snapshotWithMessage(ctx, meetingID, fmt.Sprintf("Updated decision %d: %s", index, updated.Content))

	case "create_decision":
		content := strings.TrimSpace(command.Content)
		if content == "" {
			return nil, "No decision content found.", fmt.Errorf("create decision missing content")
		}
		created, err := s.store.CreateDecision(ctx, meetingID, content, command.Reason)
		if err != nil {
			return nil, "Create decision failed.", err
		}
		s.rememberUndoRecord(ctx, meetingID, eventID, interactionUndoPayload{
			Type:       "delete_decision",
			DecisionID: created.ID,
		})
		return s.snapshotWithMessage(ctx, meetingID, fmt.Sprintf("Created decision: %s", created.Content))

	case "merge_decisions":
		ids, err := s.resolveMergeDecisionIDs(ctx, meetingID, command)
		if err != nil {
			return nil, "No decision targets found.", err
		}
		merged, removed, err := s.store.MergeDecisions(ctx, meetingID, ids, command.Content)
		if err != nil {
			return nil, "Merge decisions failed.", err
		}
		s.rememberUndoRecord(ctx, meetingID, eventID, interactionUndoPayload{Type: "unsupported_operation"})
		return s.snapshotWithMessage(ctx, meetingID, fmt.Sprintf("Merged %d decisions into: %s", len(removed)+1, merged.Content))

	case "move_decision_to_action_item":
		index, decision, err := s.resolveDecisionTarget(ctx, meetingID, command)
		if err != nil {
			return nil, "No decision target found.", err
		}
		mutation := actionItemMutationFromCommand(command)
		if mutation.Task == "" {
			mutation.Task = decision.Content
		}
		_, item, err := s.store.MoveDecisionToActionItem(ctx, meetingID, decision.ID, mutation)
		if err != nil {
			return nil, "Move decision to action item failed.", err
		}
		s.rememberUndoRecord(ctx, meetingID, eventID, interactionUndoPayload{Type: "unsupported_operation"})
		return s.snapshotWithMessage(ctx, meetingID, fmt.Sprintf("Moved decision %d to action item: %s", index, item.Task))

	case "rewrite_summary_section":
		content := strings.TrimSpace(command.Content)
		if content == "" {
			return nil, "没有识别到新的摘要内容。", fmt.Errorf("rewrite summary missing content")
		}
		summary, err := s.rewriteSummaryByCommand(ctx, meetingID, command)
		if err != nil {
			return nil, "更新摘要失败。", err
		}
		s.rememberUndoRecord(ctx, meetingID, eventID, interactionUndoPayload{Type: "unsupported_operation"})
		return s.snapshotWithMessage(ctx, meetingID, fmt.Sprintf("已更新会议摘要，当前长度 %d 字。", len([]rune(summary.Content))))

	case "undo_last_interaction":
		return s.undoLastInteraction(ctx, meetingID, eventID)

	default:
		return nil, "暂不支持这个语音指令。", fmt.Errorf("unsupported interaction intent: %s", command.Intent)
	}
}

func (s *Server) snapshotWithMessage(ctx context.Context, meetingID, message string) (*IntelligenceSnapshot, string, error) {
	snapshot, err := s.store.GetIntelligenceSnapshot(ctx, meetingID)
	if err != nil {
		return nil, message, err
	}
	return snapshot, message, nil
}
