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
	case "update_meeting_title":
		result, err := s.executeInteractionTool(ctx, meetingID, eventID, 1, ToolMeetingUpdateTitle, meetingTitleToolArgsFromCommand(command))
		if err != nil {
			return nil, "修改会议标题失败。", err
		}
		return s.snapshotWithMessage(ctx, meetingID, result.Message)

	case "switch_meeting_view":
		view := normalizeMeetingView(command.TargetID)
		if view == "" {
			view = normalizeMeetingView(command.Content)
		}
		if view == "" {
			return nil, "没有识别到要切换到哪个页面。", fmt.Errorf("missing meeting view target")
		}
		return s.snapshotWithMessage(ctx, meetingID, fmt.Sprintf("已切换到%s。", meetingViewLabel(view)))

	case "show_share_qr":
		return s.snapshotWithMessage(ctx, meetingID, "已打开会议纪要二维码。")

	case "request_end_meeting":
		return s.snapshotWithMessage(ctx, meetingID, "已收到结束会议请求，请在页面确认后结束会议。")

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

	case "create_risk":
		mutation := riskMutationFromCommand(command)
		if mutation.Content == "" {
			return nil, "没有识别到新风险内容。", fmt.Errorf("create risk missing content")
		}
		created, err := s.store.CreateRisk(ctx, meetingID, mutation)
		if err != nil {
			return nil, "创建风险失败。", err
		}
		s.rememberUndoRecord(ctx, meetingID, eventID, interactionUndoPayload{
			Type:   "delete_risk",
			RiskID: created.ID,
		})
		return s.snapshotWithMessage(ctx, meetingID, fmt.Sprintf("已新增风险：%s", created.Content))

	case "update_risk":
		result, err := s.executeInteractionTool(ctx, meetingID, eventID, 1, ToolRiskUpdate, riskToolArgsFromCommand(command))
		if err != nil {
			return nil, "更新风险失败。", err
		}
		return s.snapshotWithMessage(ctx, meetingID, result.Message)

	case "close_risk":
		result, err := s.executeInteractionTool(ctx, meetingID, eventID, 1, ToolRiskClose, targetToolArgsFromCommand(command))
		if err != nil {
			return nil, "关闭风险失败。", err
		}
		return s.snapshotWithMessage(ctx, meetingID, result.Message)

	case "delete_risk":
		index, risk, err := s.resolveRiskTarget(ctx, meetingID, command)
		if err != nil {
			return nil, "没有找到要删除的风险。", err
		}
		deleted, err := s.store.DeleteRisk(ctx, meetingID, risk.ID)
		if err != nil {
			return nil, "删除风险失败。", err
		}
		s.rememberUndoRecord(ctx, meetingID, eventID, interactionUndoPayload{
			Type: "restore_risk",
			Risk: risk,
		})
		return s.snapshotWithMessage(ctx, meetingID, fmt.Sprintf("已删除第 %d 条风险：%s", index, deleted.Content))

	case "merge_risks":
		ids, err := s.resolveMergeRiskIDs(ctx, meetingID, command)
		if err != nil {
			return nil, "没有找到要合并的风险。", err
		}
		originals, err := s.riskSnapshots(ctx, meetingID, ids)
		if err != nil {
			return nil, "读取风险快照失败。", err
		}
		merged, removed, err := s.store.MergeRisks(ctx, meetingID, ids, riskMutationFromCommand(command))
		if err != nil {
			return nil, "合并风险失败。", err
		}
		s.rememberUndoRecord(ctx, meetingID, eventID, interactionUndoPayload{
			Type:  "restore_risk_set",
			Risks: originals,
		})
		return s.snapshotWithMessage(ctx, meetingID, fmt.Sprintf("已合并 %d 条风险为：%s", len(removed)+1, merged.Content))

	case "create_open_question":
		mutation := openQuestionMutationFromCommand(command)
		if mutation.Question == "" {
			return nil, "没有识别到新的待确认问题。", fmt.Errorf("create open question missing question")
		}
		created, err := s.store.CreateOpenQuestion(ctx, meetingID, mutation)
		if err != nil {
			return nil, "创建待确认问题失败。", err
		}
		s.rememberUndoRecord(ctx, meetingID, eventID, interactionUndoPayload{
			Type:           "delete_open_question",
			OpenQuestionID: created.ID,
		})
		return s.snapshotWithMessage(ctx, meetingID, fmt.Sprintf("已新增待确认问题：%s", created.Question))

	case "update_open_question":
		result, err := s.executeInteractionTool(ctx, meetingID, eventID, 1, ToolOpenQuestionUpdate, openQuestionToolArgsFromCommand(command))
		if err != nil {
			return nil, "更新待确认问题失败。", err
		}
		return s.snapshotWithMessage(ctx, meetingID, result.Message)

	case "close_open_question":
		result, err := s.executeInteractionTool(ctx, meetingID, eventID, 1, ToolOpenQuestionClose, targetToolArgsFromCommand(command))
		if err != nil {
			return nil, "关闭待确认问题失败。", err
		}
		return s.snapshotWithMessage(ctx, meetingID, result.Message)

	case "delete_open_question":
		index, question, err := s.resolveOpenQuestionTarget(ctx, meetingID, command)
		if err != nil {
			return nil, "没有找到要删除的待确认问题。", err
		}
		deleted, err := s.store.DeleteOpenQuestion(ctx, meetingID, question.ID)
		if err != nil {
			return nil, "删除待确认问题失败。", err
		}
		s.rememberUndoRecord(ctx, meetingID, eventID, interactionUndoPayload{
			Type:         "restore_open_question",
			OpenQuestion: question,
		})
		return s.snapshotWithMessage(ctx, meetingID, fmt.Sprintf("已删除第 %d 条待确认问题：%s", index, deleted.Question))

	case "merge_open_questions":
		ids, err := s.resolveMergeOpenQuestionIDs(ctx, meetingID, command)
		if err != nil {
			return nil, "没有找到要合并的待确认问题。", err
		}
		originals, err := s.openQuestionSnapshots(ctx, meetingID, ids)
		if err != nil {
			return nil, "读取待确认问题快照失败。", err
		}
		merged, removed, err := s.store.MergeOpenQuestions(ctx, meetingID, ids, openQuestionMutationFromCommand(command))
		if err != nil {
			return nil, "合并待确认问题失败。", err
		}
		s.rememberUndoRecord(ctx, meetingID, eventID, interactionUndoPayload{
			Type:          "restore_open_question_set",
			OpenQuestions: originals,
		})
		return s.snapshotWithMessage(ctx, meetingID, fmt.Sprintf("已合并 %d 条待确认问题为：%s", len(removed)+1, merged.Question))

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

func riskMutationIsEmpty(mutation RiskMutation) bool {
	return mutation.Content == "" &&
		mutation.Impact == "" &&
		mutation.Level == "" &&
		mutation.Owner == "" &&
		mutation.Mitigation == "" &&
		mutation.DueDate == "" &&
		mutation.Status == ""
}

func openQuestionMutationIsEmpty(mutation OpenQuestionMutation) bool {
	return mutation.Question == "" &&
		mutation.Owner == "" &&
		mutation.DueDate == "" &&
		mutation.Status == ""
}

func (s *Server) executeInteractionTool(ctx context.Context, meetingID, eventID string, stepIndex int, toolName ToolName, args map[string]any) (*ToolResult, error) {
	executor := NewToolExecutor(NewMeetingToolRegistry(s), s.store)
	return executor.Execute(ctx, ToolExecutionInput{
		MeetingID:          meetingID,
		InteractionEventID: eventID,
		StepIndex:          stepIndex,
		ToolName:           toolName,
		Args:               args,
	})
}

func targetToolArgsFromCommand(command InteractionCommand) map[string]any {
	args := map[string]any{}
	if command.TargetIndex > 0 {
		args["target_index"] = command.TargetIndex
	}
	if strings.TrimSpace(command.TargetID) != "" {
		args["target_id"] = strings.TrimSpace(command.TargetID)
	}
	if strings.TrimSpace(command.Reason) != "" {
		args["reason"] = strings.TrimSpace(command.Reason)
	}
	return args
}

func meetingTitleToolArgsFromCommand(command InteractionCommand) map[string]any {
	args := map[string]any{}
	title := normalizeMeetingTitleInput(command.Title)
	if title == "" {
		title = normalizeMeetingTitleInput(command.Content)
	}
	putToolArg(args, "title", title)
	putToolArg(args, "reason", command.Reason)
	return args
}

func riskToolArgsFromCommand(command InteractionCommand) map[string]any {
	args := targetToolArgsFromCommand(command)
	putToolArg(args, "content", command.Content)
	putToolArg(args, "impact", command.Impact)
	putToolArg(args, "level", command.Level)
	putToolArg(args, "owner", command.Owner)
	putToolArg(args, "mitigation", command.Mitigation)
	putToolArg(args, "due_date", command.DueDate)
	putToolArg(args, "status", command.Status)
	return args
}

func openQuestionToolArgsFromCommand(command InteractionCommand) map[string]any {
	args := targetToolArgsFromCommand(command)
	putToolArg(args, "question", command.Question)
	if strings.TrimSpace(command.Question) == "" {
		putToolArg(args, "question", command.Content)
	}
	putToolArg(args, "owner", command.Owner)
	putToolArg(args, "due_date", command.DueDate)
	putToolArg(args, "status", command.Status)
	return args
}

func putToolArg(args map[string]any, key, value string) {
	value = strings.TrimSpace(value)
	if value != "" {
		args[key] = value
	}
}

func (s *Server) riskSnapshots(ctx context.Context, meetingID string, ids []string) ([]MeetingRisk, error) {
	risks, err := s.store.ListRisks(ctx, meetingID)
	if err != nil {
		return nil, err
	}
	byID := map[string]MeetingRisk{}
	for _, risk := range risks {
		byID[risk.ID] = risk
	}
	snapshots := make([]MeetingRisk, 0, len(ids))
	for _, id := range ids {
		risk, ok := byID[id]
		if !ok {
			return nil, fmt.Errorf("risk id %s not found", id)
		}
		snapshots = append(snapshots, risk)
	}
	return snapshots, nil
}

func (s *Server) openQuestionSnapshots(ctx context.Context, meetingID string, ids []string) ([]MeetingOpenQuestion, error) {
	questions, err := s.store.ListOpenQuestions(ctx, meetingID)
	if err != nil {
		return nil, err
	}
	byID := map[string]MeetingOpenQuestion{}
	for _, question := range questions {
		byID[question.ID] = question
	}
	snapshots := make([]MeetingOpenQuestion, 0, len(ids))
	for _, id := range ids {
		question, ok := byID[id]
		if !ok {
			return nil, fmt.Errorf("open question id %s not found", id)
		}
		snapshots = append(snapshots, question)
	}
	return snapshots, nil
}

func (s *Server) snapshotWithMessage(ctx context.Context, meetingID, message string) (*IntelligenceSnapshot, string, error) {
	snapshot, err := s.store.GetIntelligenceSnapshot(ctx, meetingID)
	if err != nil {
		return nil, message, err
	}
	return snapshot, message, nil
}
