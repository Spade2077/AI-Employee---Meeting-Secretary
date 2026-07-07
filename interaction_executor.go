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
	case "start_meeting":
		meeting, err := s.store.GetMeeting(ctx, meetingID)
		if err != nil {
			return nil, "读取会议状态失败。", err
		}
		switch MeetingStatus(meeting.Status) {
		case StatusRecording:
			return s.snapshotWithMessage(ctx, meetingID, "会议已经在记录中。")
		case StatusStandby:
			updated, event, err := s.store.Transition(ctx, meetingID, "start", "voice")
			if err != nil {
				return nil, "开始会议失败。", err
			}
			s.hub.Broadcast(s.stateUpdate(updated, event))
			return s.snapshotWithMessage(ctx, meetingID, "已开始会议，正在记录转写。")
		default:
			return nil, "当前状态不能开始会议。", fmt.Errorf("cannot start meeting from %s", meeting.Status)
		}

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

	case "set_transcript_timestamps":
		target := normalizeToggleTarget(command.TargetID)
		if target == "" {
			return nil, "没有识别到要打开还是关闭时间戳。", fmt.Errorf("missing timestamp toggle target")
		}
		return s.snapshotWithMessage(ctx, meetingID, transcriptTimestampReply(target))

	case "scroll_summary":
		action := normalizeScrollAction(command.TargetID)
		if action == "" {
			return nil, "没有识别到总结页滚动方向。", fmt.Errorf("missing summary scroll action")
		}
		return s.snapshotWithMessage(ctx, meetingID, summaryScrollReply(action))

	case "show_share_qr":
		return s.snapshotWithMessage(ctx, meetingID, "已打开会议纪要二维码。")

	case "request_end_meeting":
		return s.snapshotWithMessage(ctx, meetingID, "已收到结束会议请求，请在页面确认后结束会议。")

	case "create_dynamic_section":
		result, err := s.executeInteractionTool(ctx, meetingID, eventID, 1, ToolDynamicSectionCreate, dynamicSectionCreateToolArgsFromCommand(command))
		if err != nil {
			return nil, "新增动态模块失败。", err
		}
		return s.snapshotWithMessage(ctx, meetingID, result.Message)

	case "add_dynamic_section_item":
		result, err := s.executeInteractionTool(ctx, meetingID, eventID, 1, ToolDynamicSectionAddItem, dynamicSectionAddItemToolArgsFromCommand(command))
		if err != nil {
			return nil, "追加动态模块内容失败。", err
		}
		return s.snapshotWithMessage(ctx, meetingID, result.Message)

	case "update_dynamic_section":
		result, err := s.executeInteractionTool(ctx, meetingID, eventID, 1, ToolDynamicSectionUpdate, dynamicSectionUpdateToolArgsFromCommand(command))
		if err != nil {
			return nil, "更新动态模块失败。", err
		}
		return s.snapshotWithMessage(ctx, meetingID, result.Message)

	case "update_dynamic_section_item":
		result, err := s.executeInteractionTool(ctx, meetingID, eventID, 1, ToolDynamicSectionItemUpdate, dynamicSectionItemUpdateToolArgsFromCommand(command))
		if err != nil {
			return nil, "更新动态模块条目失败。", err
		}
		return s.snapshotWithMessage(ctx, meetingID, result.Message)

	case "delete_dynamic_section":
		index, section, err := s.resolveDynamicSectionTarget(ctx, meetingID, command)
		if err != nil {
			return nil, "没有找到要删除的动态模块。", err
		}
		deleted, err := s.store.DeleteDynamicSection(ctx, meetingID, section.ID)
		if err != nil {
			return nil, "删除动态模块失败。", err
		}
		s.rememberUndoRecord(ctx, meetingID, eventID, interactionUndoPayload{
			Type:            "restore_dynamic_section",
			DynamicSections: []MeetingDynamicSection{*section},
		})
		return s.snapshotWithMessage(ctx, meetingID, fmt.Sprintf("已删除第 %d 个动态模块：%s", index, deleted.Title))

	case "merge_dynamic_sections":
		ids, err := s.resolveMergeDynamicSectionIDs(ctx, meetingID, command)
		if err != nil {
			return nil, "没有找到要合并的动态模块。", err
		}
		originals, err := s.dynamicSectionSnapshots(ctx, meetingID, ids)
		if err != nil {
			return nil, "读取动态模块快照失败。", err
		}
		title := strings.TrimSpace(command.SectionTitle)
		if title == "" {
			title = strings.TrimSpace(command.Title)
		}
		merged, removed, err := s.store.MergeDynamicSections(ctx, meetingID, ids, title, command.Content)
		if err != nil {
			return nil, "合并动态模块失败。", err
		}
		s.rememberUndoRecord(ctx, meetingID, eventID, interactionUndoPayload{
			Type:            "restore_dynamic_section_set",
			DynamicSections: originals,
		})
		return s.snapshotWithMessage(ctx, meetingID, fmt.Sprintf("已合并 %d 个动态模块为：%s", len(removed)+1, merged.Title))

	case "show_dynamic_section":
		return s.executeDynamicSectionVisibility(ctx, meetingID, eventID, command, "visible")

	case "hide_dynamic_section":
		return s.executeDynamicSectionVisibility(ctx, meetingID, eventID, command, "hidden")

	case "pin_dynamic_section":
		return s.executeDynamicSectionVisibility(ctx, meetingID, eventID, command, "pinned")

	case "unpin_dynamic_section":
		return s.executeDynamicSectionVisibility(ctx, meetingID, eventID, command, "visible")

	case "show_all_dynamic_sections":
		return s.executeAllDynamicSectionsVisibility(ctx, meetingID, eventID, "visible")

	case "hide_all_dynamic_sections":
		return s.executeAllDynamicSectionsVisibility(ctx, meetingID, eventID, "hidden")

	case "update_framework_section":
		return s.executeFrameworkSectionMutation(ctx, meetingID, eventID, command, "replace")

	case "append_framework_section":
		return s.executeFrameworkSectionMutation(ctx, meetingID, eventID, command, "append")

	case "rewrite_framework_section":
		return s.executeFrameworkSectionMutation(ctx, meetingID, eventID, command, "replace")

	case "clear_framework_section":
		return s.executeFrameworkSectionMutation(ctx, meetingID, eventID, command, "clear")

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
		if existing, err := s.store.ListActionItems(ctx, meetingID); err != nil {
			return nil, "读取现有待办失败。", err
		} else if index := matchingActionItemIndexWithSource(existing, mutation.Task, mutation.Owner, mutation.DueDate, mutation.SourceText); index >= 0 {
			item := existing[index]
			updatedTask := richerText(item.Task, mutation.Task)
			shouldUpdate := updatedTask != item.Task ||
				(strings.TrimSpace(mutation.Owner) != "" && normalizeForCompare(item.Owner) != normalizeForCompare(mutation.Owner)) ||
				(strings.TrimSpace(mutation.DueDate) != "" && normalizeForCompare(item.DueDate) != normalizeForCompare(mutation.DueDate)) ||
				(strings.TrimSpace(mutation.Status) != "" && normalizeForCompare(item.Status) != normalizeForCompare(mutation.Status))
			if !shouldUpdate {
				return s.snapshotWithMessage(ctx, meetingID, fmt.Sprintf("已存在相似待办，未重复新增：%s", item.Task))
			}
			mutation.Task = updatedTask
			updated, err := s.store.UpdateActionItem(ctx, meetingID, item.ID, mutation)
			if err != nil {
				return nil, "更新已有待办失败。", err
			}
			s.rememberUndoRecord(ctx, meetingID, eventID, interactionUndoPayload{
				Type:       "restore_action_item",
				ActionItem: &item,
			})
			return s.snapshotWithMessage(ctx, meetingID, fmt.Sprintf("已更新已有待办，避免重复新增：%s", updated.Task))
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
		previous := *item
		_, decision, err := s.store.MoveActionItemToDecision(ctx, meetingID, item.ID, command.Content)
		if err != nil {
			return nil, "待办转决策失败。", err
		}
		s.rememberUndoRecord(ctx, meetingID, eventID, interactionUndoPayload{
			Type:       "restore_action_item_delete_decision",
			ActionItem: &previous,
			DecisionID: decision.ID,
		})
		return s.snapshotWithMessage(ctx, meetingID, fmt.Sprintf("已将第 %d 条待办转为决策：%s", index, decision.Content))

	case "merge_action_items":
		ids, err := s.resolveMergeActionItemIDs(ctx, meetingID, command)
		if err != nil {
			return nil, "没有找到要合并的待办。", err
		}
		originals, err := s.actionItemSnapshots(ctx, meetingID, ids)
		if err != nil {
			return nil, "读取待办快照失败。", err
		}
		mutation := actionItemMutationFromCommand(command)
		merged, removed, err := s.store.MergeActionItems(ctx, meetingID, ids, mutation)
		if err != nil {
			return nil, "合并待办失败。", err
		}
		s.rememberUndoRecord(ctx, meetingID, eventID, interactionUndoPayload{
			Type:        "restore_action_item_set",
			ActionItems: originals,
		})
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
		if existing, err := s.store.ListDecisions(ctx, meetingID); err != nil {
			return nil, "Read decisions failed.", err
		} else if index := matchingDecisionIndex(existing, content); index >= 0 {
			decision := existing[index]
			updatedContent := richerText(decision.Content, content)
			if updatedContent == decision.Content {
				return s.snapshotWithMessage(ctx, meetingID, fmt.Sprintf("Decision already exists: %s", decision.Content))
			}
			updated, err := s.store.UpdateDecision(ctx, meetingID, decision.ID, updatedContent)
			if err != nil {
				return nil, "Update existing decision failed.", err
			}
			s.rememberUndoRecord(ctx, meetingID, eventID, interactionUndoPayload{
				Type:     "restore_decision",
				Decision: &decision,
			})
			return s.snapshotWithMessage(ctx, meetingID, fmt.Sprintf("Updated existing decision to avoid duplicate: %s", updated.Content))
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
		originals, err := s.decisionSnapshots(ctx, meetingID, ids)
		if err != nil {
			return nil, "Read decision snapshots failed.", err
		}
		merged, removed, err := s.store.MergeDecisions(ctx, meetingID, ids, command.Content)
		if err != nil {
			return nil, "Merge decisions failed.", err
		}
		s.rememberUndoRecord(ctx, meetingID, eventID, interactionUndoPayload{
			Type:      "restore_decision_set",
			Decisions: originals,
		})
		return s.snapshotWithMessage(ctx, meetingID, fmt.Sprintf("Merged %d decisions into: %s", len(removed)+1, merged.Content))

	case "move_decision_to_action_item":
		index, decision, err := s.resolveDecisionTarget(ctx, meetingID, command)
		if err != nil {
			return nil, "No decision target found.", err
		}
		previous := *decision
		mutation := actionItemMutationFromCommand(command)
		if mutation.Task == "" {
			mutation.Task = decision.Content
		}
		_, item, err := s.store.MoveDecisionToActionItem(ctx, meetingID, decision.ID, mutation)
		if err != nil {
			return nil, "Move decision to action item failed.", err
		}
		s.rememberUndoRecord(ctx, meetingID, eventID, interactionUndoPayload{
			Type:         "restore_decision_delete_action_item",
			Decision:     &previous,
			ActionItemID: item.ID,
		})
		return s.snapshotWithMessage(ctx, meetingID, fmt.Sprintf("Moved decision %d to action item: %s", index, item.Task))

	case "create_risk":
		mutation := riskMutationFromCommand(command)
		if mutation.Content == "" {
			return nil, "没有识别到新风险内容。", fmt.Errorf("create risk missing content")
		}
		if existing, err := s.store.ListRisks(ctx, meetingID); err != nil {
			return nil, "读取现有风险失败。", err
		} else if index := matchingRiskIndex(existing, mutation.Content); index >= 0 {
			risk := existing[index]
			updatedContent := richerText(risk.Content, mutation.Content)
			shouldUpdate := updatedContent != risk.Content ||
				(strings.TrimSpace(mutation.Owner) != "" && normalizeForCompare(risk.Owner) != normalizeForCompare(mutation.Owner)) ||
				(strings.TrimSpace(mutation.Mitigation) != "" && normalizeForCompare(risk.Mitigation) != normalizeForCompare(mutation.Mitigation)) ||
				(strings.TrimSpace(mutation.DueDate) != "" && normalizeForCompare(risk.DueDate) != normalizeForCompare(mutation.DueDate)) ||
				(strings.TrimSpace(mutation.Status) != "" && normalizeForCompare(risk.Status) != normalizeForCompare(mutation.Status))
			if !shouldUpdate {
				return s.snapshotWithMessage(ctx, meetingID, fmt.Sprintf("已存在相似风险，未重复新增：%s", risk.Content))
			}
			mutation.Content = updatedContent
			updated, err := s.store.UpdateRisk(ctx, meetingID, risk.ID, mutation)
			if err != nil {
				return nil, "更新已有风险失败。", err
			}
			s.rememberUndoRecord(ctx, meetingID, eventID, interactionUndoPayload{
				Type: "restore_risk",
				Risk: &risk,
			})
			return s.snapshotWithMessage(ctx, meetingID, fmt.Sprintf("已更新已有风险，避免重复新增：%s", updated.Content))
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
		if existing, err := s.store.ListOpenQuestions(ctx, meetingID); err != nil {
			return nil, "读取现有待确认问题失败。", err
		} else if index := matchingOpenQuestionIndex(existing, mutation.Question); index >= 0 {
			question := existing[index]
			updatedQuestion := richerText(question.Question, mutation.Question)
			shouldUpdate := updatedQuestion != question.Question ||
				(strings.TrimSpace(mutation.Owner) != "" && normalizeForCompare(question.Owner) != normalizeForCompare(mutation.Owner)) ||
				(strings.TrimSpace(mutation.DueDate) != "" && normalizeForCompare(question.DueDate) != normalizeForCompare(mutation.DueDate)) ||
				(strings.TrimSpace(mutation.Status) != "" && normalizeForCompare(question.Status) != normalizeForCompare(mutation.Status))
			if !shouldUpdate {
				return s.snapshotWithMessage(ctx, meetingID, fmt.Sprintf("已存在相似待确认问题，未重复新增：%s", question.Question))
			}
			mutation.Question = updatedQuestion
			updated, err := s.store.UpdateOpenQuestion(ctx, meetingID, question.ID, mutation)
			if err != nil {
				return nil, "更新已有待确认问题失败。", err
			}
			s.rememberUndoRecord(ctx, meetingID, eventID, interactionUndoPayload{
				Type:         "restore_open_question",
				OpenQuestion: &question,
			})
			return s.snapshotWithMessage(ctx, meetingID, fmt.Sprintf("已更新已有待确认问题，避免重复新增：%s", updated.Question))
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

	case "delete_open_questions":
		ids, err := s.resolveMergeOpenQuestionIDs(ctx, meetingID, command)
		if err != nil {
			return nil, "没有找到要删除的待确认问题。", err
		}
		originals, err := s.openQuestionSnapshots(ctx, meetingID, ids)
		if err != nil {
			return nil, "读取待确认问题快照失败。", err
		}
		for _, id := range ids {
			if _, err := s.store.DeleteOpenQuestion(ctx, meetingID, id); err != nil {
				return nil, "删除待确认问题失败。", err
			}
		}
		s.rememberUndoRecord(ctx, meetingID, eventID, interactionUndoPayload{
			Type:          "restore_open_question_set",
			OpenQuestions: originals,
		})
		return s.snapshotWithMessage(ctx, meetingID, fmt.Sprintf("已删除 %d 条待确认问题。", len(originals)))

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
		if content == "" && !commandRequestsConciseSummary(command) {
			return nil, "没有识别到新的摘要内容。", fmt.Errorf("rewrite summary missing content")
		}
		beforeSnapshot, err := s.store.GetIntelligenceSnapshot(ctx, meetingID)
		if err != nil {
			return nil, "读取摘要快照失败。", err
		}
		summary, err := s.rewriteSummaryByCommand(ctx, meetingID, command)
		if err != nil {
			return nil, "更新摘要失败。", err
		}
		if beforeSnapshot != nil && beforeSnapshot.Summary != nil {
			s.rememberUndoRecord(ctx, meetingID, eventID, interactionUndoPayload{
				Type:    "restore_summary",
				Summary: beforeSnapshot.Summary,
			})
		}
		return s.snapshotWithMessage(ctx, meetingID, fmt.Sprintf("已更新会议摘要，当前长度 %d 字。", len([]rune(summary.Content))))

	case "undo_last_interaction":
		return s.undoLastInteraction(ctx, meetingID, eventID)

	default:
		return nil, "暂不支持这个语音指令。", fmt.Errorf("unsupported interaction intent: %s", command.Intent)
	}
}

func (s *Server) executeFrameworkSectionMutation(ctx context.Context, meetingID, eventID string, command InteractionCommand, mode string) (*IntelligenceSnapshot, string, error) {
	index, previous, err := s.resolveFrameworkSectionTarget(ctx, meetingID, command)
	if err != nil {
		return nil, "没有找到要修改的会议框架模块。", err
	}
	content := strings.TrimSpace(command.Content)
	if mode != "clear" && content == "" {
		return nil, "没有识别到新的模块内容。", fmt.Errorf("framework section content is required")
	}
	sourceText := strings.TrimSpace(command.Instruction)
	if sourceText == "" {
		sourceText = strings.TrimSpace(command.Reason)
	}
	updated, err := s.store.ApplyFrameworkSectionMutation(ctx, meetingID, previous.MeetingType, previous.SectionKey, FrameworkSectionMutation{
		Mode:       mode,
		Content:    content,
		SourceText: sourceText,
		Locked:     true,
	})
	if err != nil {
		return nil, "更新会议框架模块失败。", err
	}
	s.rememberUndoRecord(ctx, meetingID, eventID, interactionUndoPayload{
		Type:             "restore_framework_section",
		FrameworkSection: previous,
	})
	action := frameworkSectionActionLabel(mode)
	if mode == "clear" {
		return s.snapshotWithMessage(ctx, meetingID, fmt.Sprintf("已%s第 %d 个框架模块：%s。", action, index, updated.Title))
	}
	return s.snapshotWithMessage(ctx, meetingID, fmt.Sprintf("已%s第 %d 个框架模块：%s：%s", action, index, updated.Title, updated.Content))
}

func frameworkSectionActionLabel(mode string) string {
	switch normalizeFrameworkMutationMode(mode) {
	case "append":
		return "补充"
	case "clear":
		return "清空"
	default:
		return "更新"
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

func (s *Server) executeDynamicSectionVisibility(ctx context.Context, meetingID, eventID string, command InteractionCommand, visibility string) (*IntelligenceSnapshot, string, error) {
	index, section, err := s.resolveDynamicSectionTarget(ctx, meetingID, command)
	if err != nil {
		return nil, "没有找到要操作的动态模块。", err
	}
	previous := *section
	updated, err := s.store.SetDynamicSectionVisibility(ctx, meetingID, section.ID, visibility)
	if err != nil {
		return nil, "更新动态模块展示状态失败。", err
	}
	s.rememberUndoRecord(ctx, meetingID, eventID, interactionUndoPayload{
		Type:            "restore_dynamic_section_visibility",
		DynamicSections: []MeetingDynamicSection{previous},
	})
	action := dynamicSectionVisibilityAction(visibility)
	return s.snapshotWithMessage(ctx, meetingID, fmt.Sprintf("已%s第 %d 个动态模块：%s", action, index, updated.Title))
}

func (s *Server) executeAllDynamicSectionsVisibility(ctx context.Context, meetingID, eventID, visibility string) (*IntelligenceSnapshot, string, error) {
	previous, err := s.store.ListDynamicSections(ctx, meetingID)
	if err != nil {
		return nil, "读取动态模块失败。", err
	}
	count, err := s.store.SetAllDynamicSectionVisibility(ctx, meetingID, visibility)
	if err != nil {
		return nil, "更新动态模块展示状态失败。", err
	}
	s.rememberUndoRecord(ctx, meetingID, eventID, interactionUndoPayload{
		Type:            "restore_dynamic_section_visibility",
		DynamicSections: previous,
	})
	action := dynamicSectionVisibilityAction(visibility)
	if count == 0 {
		return s.snapshotWithMessage(ctx, meetingID, "当前没有可操作的动态模块。")
	}
	return s.snapshotWithMessage(ctx, meetingID, fmt.Sprintf("已%s全部动态模块，共 %d 个。", action, count))
}

func dynamicSectionVisibilityAction(visibility string) string {
	switch normalizeDynamicSectionVisibility(visibility) {
	case "hidden":
		return "隐藏"
	case "pinned":
		return "固定展示"
	default:
		return "展示"
	}
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

func dynamicSectionCreateToolArgsFromCommand(command InteractionCommand) map[string]any {
	args := map[string]any{}
	title := strings.TrimSpace(command.SectionTitle)
	if title == "" {
		title = strings.TrimSpace(command.Title)
	}
	putToolArg(args, "title", title)
	putToolArg(args, "section_key", command.SectionKey)
	putToolArg(args, "description", command.Reason)
	putToolArg(args, "content", command.Content)
	putToolArg(args, "source_text", command.Instruction)
	return args
}

func dynamicSectionAddItemToolArgsFromCommand(command InteractionCommand) map[string]any {
	args := targetToolArgsFromCommand(command)
	putToolArg(args, "section_key", command.SectionKey)
	putToolArg(args, "section_title", command.SectionTitle)
	putToolArg(args, "title", command.Title)
	putToolArg(args, "content", command.Content)
	putToolArg(args, "source_text", command.Instruction)
	return args
}

func dynamicSectionUpdateToolArgsFromCommand(command InteractionCommand) map[string]any {
	args := targetToolArgsFromCommand(command)
	putToolArg(args, "section_key", command.SectionKey)
	putToolArg(args, "section_title", command.SectionTitle)
	putToolArg(args, "title", command.Title)
	putToolArg(args, "description", command.Content)
	putToolArg(args, "reason", command.Reason)
	return args
}

func dynamicSectionItemUpdateToolArgsFromCommand(command InteractionCommand) map[string]any {
	args := dynamicSectionAddItemToolArgsFromCommand(command)
	delete(args, "source_text")
	if len(command.SourceIndices) > 0 {
		args["source_indices"] = command.SourceIndices
	}
	if len(command.SourceIDs) > 0 {
		args["source_ids"] = command.SourceIDs
	}
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
