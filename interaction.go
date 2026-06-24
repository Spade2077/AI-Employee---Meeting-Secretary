package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"regexp"
	"strconv"
	"strings"
	"time"

	"meeting-core-service/internal/stt"
)

var wakeWords = []string{
	"小艾小艾",
	"小爱小爱",
	"小艾小爱",
	"小爱小艾",
	"小AI小AI",
	"小A小A",
}

var wakeWordPatterns = []struct {
	label   string
	pattern *regexp.Regexp
}{
	{label: "小艾小艾", pattern: regexp.MustCompile(`(?i)小\s*艾\s*[,，、。.!！？?\s]*小\s*艾`)},
	{label: "小爱小爱", pattern: regexp.MustCompile(`(?i)小\s*爱\s*[,，、。.!！？?\s]*小\s*爱`)},
	{label: "小艾小爱", pattern: regexp.MustCompile(`(?i)小\s*艾\s*[,，、。.!！？?\s]*小\s*爱`)},
	{label: "小爱小艾", pattern: regexp.MustCompile(`(?i)小\s*爱\s*[,，、。.!！？?\s]*小\s*艾`)},
	{label: "小AI小AI", pattern: regexp.MustCompile(`(?i)小\s*a\s*i?\s*[,，、。.!！？?\s]*小\s*a\s*i?`)},
}

const (
	interactionDebounce = 900 * time.Millisecond
	interactionWakeTTL  = 8 * time.Second
)

type PendingInteraction struct {
	WakeWord       string
	WakeTranscript string
	Instruction    string
	FirstResultAt  time.Time
	WakeDetectedAt time.Time
	LastResultAt   time.Time
	UpdatedAt      time.Time
	Timer          *time.Timer
}

type interactionTimingSeed struct {
	FirstResultAt  time.Time
	WakeDetectedAt time.Time
	LastResultAt   time.Time
	DispatchAt     time.Time
}

func newInteractionTiming(seed interactionTimingSeed) *InteractionTiming {
	now := time.Now()
	if seed.FirstResultAt.IsZero() {
		seed.FirstResultAt = now
	}
	if seed.WakeDetectedAt.IsZero() {
		seed.WakeDetectedAt = seed.FirstResultAt
	}
	if seed.LastResultAt.IsZero() {
		seed.LastResultAt = seed.WakeDetectedAt
	}
	if seed.DispatchAt.IsZero() {
		seed.DispatchAt = now
	}

	timing := &InteractionTiming{
		StartedAt: seed.FirstResultAt.UTC().Format(time.RFC3339Nano),
		Stages:    []InteractionTimingStage{},
	}
	timing.addStage("wake_detect", "ASR 到唤醒检测", seed.WakeDetectedAt.Sub(seed.FirstResultAt), "")
	timing.addStage("instruction_wait", "等待完整指令", seed.LastResultAt.Sub(seed.WakeDetectedAt), "")
	timing.addStage("debounce_wait", "防抖等待", seed.DispatchAt.Sub(seed.LastResultAt), "")
	return timing
}

func (t *InteractionTiming) addStage(key, label string, duration time.Duration, detail string) {
	if t == nil {
		return
	}
	ms := duration.Milliseconds()
	if ms < 0 {
		ms = 0
	}
	t.Stages = append(t.Stages, InteractionTimingStage{
		Key:        key,
		Label:      label,
		DurationMs: ms,
		Detail:     strings.TrimSpace(detail),
	})
}

func (t *InteractionTiming) finish(completedAt string, totalMs int64) {
	if t == nil {
		return
	}
	t.CompletedAt = completedAt
	t.TotalMs = totalMs
}

func extractWakeInstruction(text string) (wakeWord, instruction string, ok bool) {
	for _, wake := range wakeWordPatterns {
		loc := wake.pattern.FindStringIndex(text)
		if loc == nil {
			continue
		}
		instruction = strings.TrimSpace(text[loc[1]:])
		instruction = strings.TrimLeft(instruction, " ，。！？、,.!?;；:")
		return wake.label, instruction, true
	}
	return "", "", false
}

func (s *Server) consumeInteractionTranscript(result stt.TranscriptResult) bool {
	if !result.IsFinal || result.MeetingID == "" || strings.TrimSpace(result.Text) == "" {
		return false
	}
	receivedAt := time.Now()
	if wakeWord, instruction, ok := extractWakeInstruction(result.Text); ok {
		s.rememberInteraction(result.MeetingID, wakeWord, result.Text, instruction, receivedAt)
		return true
	}
	if s.hasPendingInteraction(result.MeetingID) {
		s.appendPendingInteractionInstruction(result.MeetingID, result.Text, receivedAt)
		return true
	}
	if isWakePrefix(result.Text) {
		s.rememberInteraction(result.MeetingID, "", result.Text, "", receivedAt)
		return true
	}
	return false
}

func (s *Server) rememberInteraction(meetingID, wakeWord, wakeTranscript, instruction string, receivedAt time.Time) {
	s.interactionMu.Lock()
	defer s.interactionMu.Unlock()
	s.ensureInteractionMapsLocked()

	if receivedAt.IsZero() {
		receivedAt = time.Now()
	}
	pending := s.pendingInteractions[meetingID]
	if pending == nil {
		pending = &PendingInteraction{}
		s.pendingInteractions[meetingID] = pending
	}
	if pending.Timer != nil {
		pending.Timer.Stop()
	}
	if wakeWord != "" {
		pending.WakeWord = wakeWord
		if pending.WakeDetectedAt.IsZero() {
			pending.WakeDetectedAt = receivedAt
		}
	}
	if pending.FirstResultAt.IsZero() {
		pending.FirstResultAt = receivedAt
	}
	pending.LastResultAt = receivedAt
	pending.WakeTranscript = strings.TrimSpace(wakeTranscript)
	pending.Instruction = strings.TrimSpace(instruction)
	pending.UpdatedAt = time.Now()
	s.schedulePendingInteractionLocked(meetingID, pending)
}

func (s *Server) appendPendingInteractionInstruction(meetingID, instruction string, receivedAt time.Time) {
	s.interactionMu.Lock()
	defer s.interactionMu.Unlock()
	s.ensureInteractionMapsLocked()

	if receivedAt.IsZero() {
		receivedAt = time.Now()
	}
	pending := s.pendingInteractions[meetingID]
	if pending == nil {
		return
	}
	if time.Since(pending.UpdatedAt) > interactionWakeTTL {
		s.cancelPendingInteractionLocked(meetingID)
		return
	}
	if pending.Timer != nil {
		pending.Timer.Stop()
	}
	pending.Instruction = richerText(pending.Instruction, instruction)
	pending.LastResultAt = receivedAt
	pending.UpdatedAt = time.Now()
	s.schedulePendingInteractionLocked(meetingID, pending)
}

func (s *Server) hasPendingInteraction(meetingID string) bool {
	s.interactionMu.Lock()
	defer s.interactionMu.Unlock()
	s.ensureInteractionMapsLocked()

	pending := s.pendingInteractions[meetingID]
	if pending == nil {
		return false
	}
	if time.Since(pending.UpdatedAt) > interactionWakeTTL {
		s.cancelPendingInteractionLocked(meetingID)
		return false
	}
	return strings.TrimSpace(pending.WakeWord) != "" || isWakePrefix(pending.WakeTranscript)
}

func (s *Server) schedulePendingInteractionLocked(meetingID string, pending *PendingInteraction) {
	delay := interactionWakeTTL
	if strings.TrimSpace(pending.Instruction) != "" && strings.TrimSpace(pending.WakeWord) != "" {
		delay = interactionDebounce
	}
	pending.Timer = time.AfterFunc(delay, func() {
		s.firePendingInteraction(meetingID)
	})
}

func (s *Server) firePendingInteraction(meetingID string) {
	s.interactionMu.Lock()
	s.ensureInteractionMapsLocked()
	pending := s.pendingInteractions[meetingID]
	if pending == nil {
		s.interactionMu.Unlock()
		return
	}
	delete(s.pendingInteractions, meetingID)
	s.interactionMu.Unlock()

	wakeWord := strings.TrimSpace(pending.WakeWord)
	instruction := strings.TrimSpace(pending.Instruction)
	if wakeWord == "" || instruction == "" {
		return
	}
	rawTranscript := strings.TrimSpace(strings.TrimSpace(pending.WakeTranscript) + " " + instruction)
	s.handleMeetingInteractionWithTiming(context.Background(), stt.TranscriptResult{
		MeetingID: meetingID,
		Text:      rawTranscript,
		IsFinal:   true,
		Provider:  "interaction",
	}, wakeWord, instruction, interactionTimingSeed{
		FirstResultAt:  pending.FirstResultAt,
		WakeDetectedAt: pending.WakeDetectedAt,
		LastResultAt:   pending.LastResultAt,
		DispatchAt:     time.Now(),
	})
}

func (s *Server) ensureInteractionMapsLocked() {
	if s.pendingInteractions == nil {
		s.pendingInteractions = map[string]*PendingInteraction{}
	}
}

func (s *Server) cancelPendingInteractionLocked(meetingID string) {
	pending := s.pendingInteractions[meetingID]
	if pending != nil && pending.Timer != nil {
		pending.Timer.Stop()
	}
	delete(s.pendingInteractions, meetingID)
}

func isWakePrefix(text string) bool {
	key := normalizeForCompare(text)
	if key == "" {
		return false
	}
	for _, wake := range wakeWords {
		wakeKey := normalizeForCompare(wake)
		if strings.HasPrefix(wakeKey, key) && key != wakeKey {
			return true
		}
	}
	return false
}

func (s *Server) handleMeetingInteraction(ctx context.Context, result stt.TranscriptResult, wakeWord, instruction string) {
	now := time.Now()
	s.handleMeetingInteractionWithTiming(ctx, result, wakeWord, instruction, interactionTimingSeed{
		FirstResultAt:  now,
		WakeDetectedAt: now,
		LastResultAt:   now,
		DispatchAt:     now,
	})
}

func (s *Server) handleMeetingInteractionWithTiming(ctx context.Context, result stt.TranscriptResult, wakeWord, instruction string, seed interactionTimingSeed) {
	if result.MeetingID == "" {
		return
	}
	timing := newInteractionTiming(seed)
	event := InteractionEvent{
		ID:              newID("ie"),
		MeetingID:       result.MeetingID,
		RawTranscript:   result.Text,
		WakeWord:        wakeWord,
		Instruction:     instruction,
		ExecutionStatus: "failed",
		CreatedAt:       timing.StartedAt,
		Timing:          timing,
	}
	s.broadcastInteractionStatus(result.MeetingID, event.ID, "understanding", "正在理解语音指令...")

	command, commandJSON, err := s.parseInteractionCommand(ctx, result.MeetingID, instruction, timing)
	if err != nil {
		event.Message = "指令理解失败。"
		event.ErrorMessage = err.Error()
		s.saveAndBroadcastInteraction(ctx, event, nil)
		return
	}
	event.Intent = command.Intent
	event.TargetType = command.TargetType
	event.TargetIndex = command.TargetIndex
	event.TargetID = command.TargetID
	event.CommandJSON = commandJSON
	s.broadcastInteractionStatus(result.MeetingID, event.ID, "executing", "已识别指令，正在执行："+command.Intent)

	executeStarted := time.Now()
	snapshot, message, err := s.executeInteractionCommandWithEventID(ctx, result.MeetingID, command, event.ID)
	timing.addStage("execute_command", "执行数据库操作", time.Since(executeStarted), command.Intent)
	if err != nil {
		event.Message = message
		event.ErrorMessage = err.Error()
		s.saveAndBroadcastInteraction(ctx, event, snapshot)
		return
	}
	event.ExecutionStatus = "success"
	event.Message = message
	if event.Message == "" {
		event.Message = command.Reply
	}
	s.saveAndBroadcastInteraction(ctx, event, snapshot)
}

func (s *Server) parseInteractionCommand(ctx context.Context, meetingID, instruction string, timing *InteractionTiming) (InteractionCommand, string, error) {
	contextStarted := time.Now()
	snapshot, err := s.store.GetIntelligenceSnapshot(ctx, meetingID)
	timing.addStage("load_context", "读取结构化上下文", time.Since(contextStarted), "")
	if err != nil {
		return InteractionCommand{}, "", err
	}

	localStarted := time.Now()
	if command, ok := parseUndoLocally(instruction); ok {
		timing.addStage("local_parse", "本地解析指令", time.Since(localStarted), "未调用 LLM；本地撤销指令")
		command = normalizeInteractionCommand(command)
		bytes, _ := json.Marshal(command)
		return command, string(bytes), nil
	}

	configStarted := time.Now()
	cfg, err := s.store.GetLLMConfigFor(ctx, LLMUseCaseInteraction)
	timing.addStage("load_llm_config", "读取交互 LLM 配置", time.Since(configStarted), interactionLLMDetail(cfg))
	if err != nil {
		return InteractionCommand{}, "", err
	}
	if strings.TrimSpace(cfg.BaseURL) != "" && strings.TrimSpace(cfg.Model) != "" {
		llmCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
		defer cancel()
		llmStarted := time.Now()
		command, content, err := s.callLLMForInteraction(llmCtx, cfg, instruction, snapshot)
		timing.addStage("interaction_llm", "调用交互 LLM", time.Since(llmStarted), interactionLLMDetail(cfg))
		if err == nil {
			return normalizeInteractionCommand(command), content, nil
		}
		log.Printf("interaction llm failed meeting=%s: %v", meetingID, err)
	}

	fallbackStarted := time.Now()
	command, err := parseDeleteActionItemLocally(instruction)
	timing.addStage("local_fallback_parse", "本地兜底解析", time.Since(fallbackStarted), "未调用 LLM；本地删除待办兜底")
	if err != nil {
		return InteractionCommand{}, "", err
	}
	command = normalizeInteractionCommand(command)
	bytes, _ := json.Marshal(command)
	return command, string(bytes), nil
}

func (s *Server) callLLMForInteraction(ctx context.Context, cfg *LLMConfig, instruction string, snapshot *IntelligenceSnapshot) (InteractionCommand, string, error) {
	type actionItemContext struct {
		Index   int    `json:"index"`
		ID      string `json:"id"`
		Task    string `json:"task"`
		Owner   string `json:"owner,omitempty"`
		DueDate string `json:"dueDate,omitempty"`
		Status  string `json:"status"`
	}
	type decisionContext struct {
		Index   int    `json:"index"`
		ID      string `json:"id"`
		Content string `json:"content"`
	}

	items := make([]actionItemContext, 0, len(snapshot.ActionItems))
	for index, item := range snapshot.ActionItems {
		items = append(items, actionItemContext{
			Index:   index + 1,
			ID:      item.ID,
			Task:    item.Task,
			Owner:   item.Owner,
			DueDate: item.DueDate,
			Status:  item.Status,
		})
	}
	decisions := make([]decisionContext, 0, len(snapshot.Decisions))
	for index, decision := range snapshot.Decisions {
		decisions = append(decisions, decisionContext{
			Index:   index + 1,
			ID:      decision.ID,
			Content: decision.Content,
		})
	}
	summary := ""
	if snapshot.Summary != nil {
		summary = snapshot.Summary.Content
	}

	payload := map[string]any{
		"instruction":  instruction,
		"summary":      summary,
		"decisions":    decisions,
		"action_items": items,
		"supported_intents": []string{
			"delete_action_item",
			"update_action_item",
			"create_action_item",
			"complete_action_item",
			"move_action_item_to_decision",
			"merge_action_items",
			"delete_decision",
			"update_decision",
			"create_decision",
			"merge_decisions",
			"move_decision_to_action_item",
			"rewrite_summary_section",
			"undo_last_interaction",
			"unsupported",
		},
	}
	payloadBytes, _ := json.MarshalIndent(payload, "", "  ")
	systemPrompt := `You are the meeting assistant interaction parser. Convert the user's spoken instruction into one executable JSON command only. Do not execute anything. Do not output Markdown, explanations, or code fences.

Return exactly this JSON shape:
{
  "intent": "delete_action_item | update_action_item | create_action_item | complete_action_item | move_action_item_to_decision | merge_action_items | delete_decision | update_decision | create_decision | merge_decisions | move_decision_to_action_item | rewrite_summary_section | undo_last_interaction | unsupported",
  "target_type": "action_item | decision | summary | interaction",
  "target_index": 0,
  "target_id": "",
  "source_indices": [],
  "source_ids": [],
  "task": "",
  "owner": "",
  "due_date": "",
  "status": "",
  "content": "",
  "confidence": 0.0,
  "need_confirm": false,
  "reply": "",
  "reason": ""
}

Rules:
1. Use the supplied action_items, decisions, and summary as the only editable context.
2. For delete_action_item, complete_action_item, update_action_item, and move_action_item_to_decision, target_type must be action_item and target_index or target_id must identify one existing action item.
3. For create_action_item, target_type must be action_item and task is required. Fill owner, due_date, and status only if the instruction says them.
4. For update_action_item, fill only the fields the user asked to change: task, owner, due_date, status, or content when useful.
5. For complete_action_item, set status to completed.
6. For move_action_item_to_decision, content is optional. If the user asks to rewrite the decision wording, put the rewritten decision text in content.
7. For merge_action_items, source_indices or source_ids must contain every action item mentioned by the user. Do not use delete_action_item for merge instructions. If the user does not provide exact merged wording, leave task empty so the backend can combine the original tasks.
8. For delete_decision and update_decision, target_type must be decision and target_index or target_id must identify one existing decision. update_decision requires content.
9. For create_decision, target_type must be decision and content is required.
10. For merge_decisions, source_indices or source_ids must contain every decision mentioned by the user. Do not use delete_decision for merge instructions. If the user does not provide exact merged wording, leave content empty so the backend can combine the original decision texts.
11. For move_decision_to_action_item, target_type must be decision and target_index or target_id must identify one existing decision. task, owner, due_date, and status are optional action item values.
12. For rewrite_summary_section, target_type must be summary and content must contain the new full summary or rewritten summary section text.
13. For undo_last_interaction, target_type must be interaction. Use it when the user says undo, revert, cancel the previous operation, or 撤销刚刚的操作.
14. If the target is ambiguous or does not exist, set need_confirm=true and explain in reason. Do not guess.
15. If the instruction is outside supported_intents, return unsupported.`

	content, err := s.llmClient.Complete(ctx, cfg, []LLMMessage{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: string(payloadBytes)},
	}, 0)
	if err != nil {
		return InteractionCommand{}, "", err
	}
	var command InteractionCommand
	if err := json.Unmarshal([]byte(extractJSONObject(content)), &command); err != nil {
		return InteractionCommand{}, content, err
	}
	return command, content, nil
}

func interactionLLMDetail(cfg *LLMConfig) string {
	if cfg == nil {
		return "交互 LLM 配置读取失败"
	}
	model := strings.TrimSpace(cfg.Model)
	if model == "" {
		model = "未配置"
	}
	return fmt.Sprintf("模型：%s；思考模式：%s", model, thinkingModeLabel(cfg.ThinkingMode))
}

func thinkingModeLabel(mode string) string {
	switch normalizeLLMThinkingMode(mode) {
	case LLMThinkingEnabled:
		return "enabled（已开启）"
	case LLMThinkingDisabled:
		return "disabled（已关闭）"
	default:
		return "default（未显式发送）"
	}
}

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
		summary, err := s.store.RewriteSummary(ctx, meetingID, content)
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

func (s *Server) resolveActionItemTarget(ctx context.Context, meetingID string, command InteractionCommand) (int, *MeetingActionItem, error) {
	actionItems, err := s.store.ListActionItems(ctx, meetingID)
	if err != nil {
		return 0, nil, err
	}
	targetID := strings.TrimSpace(command.TargetID)
	if targetID != "" {
		for index, item := range actionItems {
			if item.ID == targetID {
				return index + 1, &actionItems[index], nil
			}
		}
		return 0, nil, fmt.Errorf("action item id %s not found", targetID)
	}
	if command.TargetIndex <= 0 || command.TargetIndex > len(actionItems) {
		return 0, nil, fmt.Errorf("action item index %d out of range", command.TargetIndex)
	}
	return command.TargetIndex, &actionItems[command.TargetIndex-1], nil
}

func (s *Server) resolveMergeActionItemIDs(ctx context.Context, meetingID string, command InteractionCommand) ([]string, error) {
	actionItems, err := s.store.ListActionItems(ctx, meetingID)
	if err != nil {
		return nil, err
	}
	ids := append([]string{}, command.SourceIDs...)
	if command.TargetID != "" {
		ids = append([]string{command.TargetID}, ids...)
	}
	for _, index := range command.SourceIndices {
		if index <= 0 || index > len(actionItems) {
			return nil, fmt.Errorf("action item index %d out of range", index)
		}
		ids = append(ids, actionItems[index-1].ID)
	}
	if command.TargetID == "" && command.TargetIndex > 0 {
		if command.TargetIndex > len(actionItems) {
			return nil, fmt.Errorf("action item index %d out of range", command.TargetIndex)
		}
		ids = append([]string{actionItems[command.TargetIndex-1].ID}, ids...)
	}
	ids = uniqueNonEmptyStrings(ids)
	if len(ids) < 2 {
		return nil, fmt.Errorf("merge requires at least two action item targets")
	}
	return ids, nil
}

func (s *Server) resolveDecisionTarget(ctx context.Context, meetingID string, command InteractionCommand) (int, *MeetingDecision, error) {
	decisions, err := s.store.ListDecisions(ctx, meetingID)
	if err != nil {
		return 0, nil, err
	}
	targetID := strings.TrimSpace(command.TargetID)
	if targetID != "" {
		for index, decision := range decisions {
			if decision.ID == targetID {
				return index + 1, &decisions[index], nil
			}
		}
		return 0, nil, fmt.Errorf("decision id %s not found", targetID)
	}
	if command.TargetIndex <= 0 || command.TargetIndex > len(decisions) {
		return 0, nil, fmt.Errorf("decision index %d out of range", command.TargetIndex)
	}
	return command.TargetIndex, &decisions[command.TargetIndex-1], nil
}

func (s *Server) resolveMergeDecisionIDs(ctx context.Context, meetingID string, command InteractionCommand) ([]string, error) {
	decisions, err := s.store.ListDecisions(ctx, meetingID)
	if err != nil {
		return nil, err
	}
	ids := append([]string{}, command.SourceIDs...)
	if command.TargetID != "" {
		ids = append([]string{command.TargetID}, ids...)
	}
	for _, index := range command.SourceIndices {
		if index <= 0 || index > len(decisions) {
			return nil, fmt.Errorf("decision index %d out of range", index)
		}
		ids = append(ids, decisions[index-1].ID)
	}
	if command.TargetID == "" && command.TargetIndex > 0 {
		if command.TargetIndex > len(decisions) {
			return nil, fmt.Errorf("decision index %d out of range", command.TargetIndex)
		}
		ids = append([]string{decisions[command.TargetIndex-1].ID}, ids...)
	}
	ids = uniqueNonEmptyStrings(ids)
	if len(ids) < 2 {
		return nil, fmt.Errorf("merge requires at least two decision targets")
	}
	return ids, nil
}

func actionItemMutationFromCommand(command InteractionCommand) ActionItemMutation {
	task := strings.TrimSpace(command.Task)
	if task == "" && command.Intent == "merge_action_items" {
		task = strings.TrimSpace(command.Content)
	}
	if task == "" && command.Intent == "update_action_item" {
		task = strings.TrimSpace(command.Content)
	}
	if task == "" && command.Intent == "move_decision_to_action_item" {
		task = strings.TrimSpace(command.Content)
	}
	return ActionItemMutation{
		Task:       task,
		Owner:      strings.TrimSpace(command.Owner),
		DueDate:    strings.TrimSpace(command.DueDate),
		Status:     strings.TrimSpace(command.Status),
		SourceText: strings.TrimSpace(command.Reason),
	}
}

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

func (s *Server) saveAndBroadcastInteraction(ctx context.Context, event InteractionEvent, snapshot *IntelligenceSnapshot) {
	finalizeInteractionEvent(&event, snapshot)
	saveStarted := time.Now()
	saved, err := s.store.SaveInteractionEvent(ctx, event)
	saveDuration := time.Since(saveStarted)
	if err != nil {
		log.Printf("save interaction event failed meeting=%s: %v", event.MeetingID, err)
		saved = &event
	} else if saved != nil && saved.Timing != nil {
		saved.Timing.addStage("save_event", "保存交互事件", saveDuration, "")
		if err := s.store.UpdateInteractionEventTiming(ctx, saved.ID, saved.Timing); err != nil {
			log.Printf("update interaction timing failed event=%s: %v", saved.ID, err)
		}
	}
	update := InteractionUpdate{
		Type:      "interaction_update",
		MeetingID: event.MeetingID,
		Event:     saved,
		Snapshot:  snapshot,
		Timestamp: nowString(),
	}
	s.hub.Broadcast(update)
	if snapshot != nil {
		s.hub.Broadcast(snapshot)
	}
}

func (s *Server) broadcastInteractionStatus(meetingID, eventID, status, message string) {
	s.hub.Broadcast(InteractionStatusUpdate{
		Type:      "interaction_status",
		MeetingID: meetingID,
		EventID:   eventID,
		Status:    status,
		Message:   message,
		Timestamp: nowString(),
	})
}

func finalizeInteractionEvent(event *InteractionEvent, snapshot *IntelligenceSnapshot) {
	now := nowString()
	if event.CreatedAt == "" {
		event.CreatedAt = now
	}
	if event.CompletedAt == "" {
		event.CompletedAt = now
	}
	event.DurationMs = interactionDurationMs(event.CreatedAt, event.CompletedAt)
	if event.Timing != nil {
		event.Timing.finish(event.CompletedAt, event.DurationMs)
	}
	if event.ExecutionStatus == "success" {
		event.Message = detailedInteractionMessage(event.Message, event, snapshot)
	}
}

func interactionDurationMs(startedAt, completedAt string) int64 {
	start, err := time.Parse(time.RFC3339Nano, startedAt)
	if err != nil {
		return 0
	}
	end, err := time.Parse(time.RFC3339Nano, completedAt)
	if err != nil || end.Before(start) {
		return 0
	}
	return end.Sub(start).Milliseconds()
}

func detailedInteractionMessage(base string, event *InteractionEvent, snapshot *IntelligenceSnapshot) string {
	parts := []string{}
	if strings.TrimSpace(base) != "" {
		parts = append(parts, strings.TrimSpace(base))
	}
	if strings.TrimSpace(event.Instruction) != "" {
		parts = append(parts, "识别到的指令："+strings.TrimSpace(event.Instruction))
	}
	if event.Intent != "" {
		target := event.TargetType
		if event.TargetIndex > 0 {
			target = fmt.Sprintf("%s #%d", target, event.TargetIndex)
		}
		parts = append(parts, "执行动作："+event.Intent+"；目标："+target)
	}
	if snapshot != nil {
		summaryState := "无摘要"
		if snapshot.Summary != nil && strings.TrimSpace(snapshot.Summary.Content) != "" {
			summaryState = fmt.Sprintf("摘要 %d 字", len([]rune(snapshot.Summary.Content)))
		}
		parts = append(parts, fmt.Sprintf("当前结果：待办 %d 条，决策 %d 条，%s。", len(snapshot.ActionItems), len(snapshot.Decisions), summaryState))
	}
	parts = append(parts, fmt.Sprintf("总耗时：%s。", formatDurationMs(event.DurationMs)))
	return strings.Join(parts, "\n")
}

func formatDurationMs(ms int64) string {
	if ms < 1000 {
		return fmt.Sprintf("%d ms", ms)
	}
	return fmt.Sprintf("%.2f s", float64(ms)/1000)
}

func normalizeInteractionCommand(command InteractionCommand) InteractionCommand {
	command.Intent = strings.TrimSpace(command.Intent)
	command.TargetType = strings.TrimSpace(command.TargetType)
	command.TargetID = strings.TrimSpace(command.TargetID)
	command.Task = strings.TrimSpace(command.Task)
	command.Owner = strings.TrimSpace(command.Owner)
	command.DueDate = strings.TrimSpace(command.DueDate)
	command.Status = normalizeActionItemStatus(command.Status)
	command.Content = strings.TrimSpace(command.Content)
	command.Reply = strings.TrimSpace(command.Reply)
	command.Reason = strings.TrimSpace(command.Reason)
	command.SourceIDs = uniqueNonEmptyStrings(command.SourceIDs)
	command.SourceIndices = positiveUniqueInts(command.SourceIndices)

	switch command.Intent {
	case "delete_action_item", "update_action_item", "create_action_item", "complete_action_item", "move_action_item_to_decision", "merge_action_items":
		if command.TargetType == "" {
			command.TargetType = "action_item"
		}
	case "delete_decision", "update_decision", "create_decision", "merge_decisions", "move_decision_to_action_item":
		if command.TargetType == "" {
			command.TargetType = "decision"
		}
	case "rewrite_summary_section":
		if command.TargetType == "" {
			command.TargetType = "summary"
		}
	case "undo_last_interaction":
		if command.TargetType == "" {
			command.TargetType = "interaction"
		}
	}
	return command
}

func parseUndoLocally(instruction string) (InteractionCommand, bool) {
	key := normalizeForCompare(instruction)
	if key == "" {
		return InteractionCommand{}, false
	}
	undoPhrases := []string{
		"撤销",
		"撤回",
		"恢复上一步",
		"取消刚刚",
		"还原刚刚",
		"undo",
		"revert",
	}
	for _, phrase := range undoPhrases {
		if strings.Contains(key, normalizeForCompare(phrase)) {
			return InteractionCommand{
				Intent:      "undo_last_interaction",
				TargetType:  "interaction",
				Confidence:  0.8,
				NeedConfirm: false,
				Reply:       "已识别为撤销上一步会中交互操作。",
			}, true
		}
	}
	return InteractionCommand{}, false
}

func parseDeleteActionItemLocally(instruction string) (InteractionCommand, error) {
	key := normalizeForCompare(instruction)
	if !strings.Contains(key, "待办") || !(strings.Contains(key, "删") || strings.Contains(key, "删除") || strings.Contains(key, "去掉")) {
		return InteractionCommand{Intent: "unsupported", TargetType: "action_item", Confidence: 0.2, NeedConfirm: true}, fmt.Errorf("unsupported local interaction instruction")
	}
	index := parseOrdinalIndex(instruction)
	if index <= 0 {
		return InteractionCommand{Intent: "delete_action_item", TargetType: "action_item", Confidence: 0.4, NeedConfirm: true}, nil
	}
	return InteractionCommand{
		Intent:      "delete_action_item",
		TargetType:  "action_item",
		TargetIndex: index,
		Confidence:  0.7,
		NeedConfirm: false,
		Reply:       fmt.Sprintf("已识别为删除第 %d 条待办。", index),
	}, nil
}

func parseOrdinalIndex(value string) int {
	value = strings.TrimSpace(value)
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`第\s*([0-9]+|[一二两三四五六七八九十]{1,3})\s*[条项个]`),
		regexp.MustCompile(`([0-9]+|[一二两三四五六七八九十]{1,3})\s*[条项个]`),
	}
	for _, pattern := range patterns {
		matches := pattern.FindStringSubmatch(value)
		if len(matches) < 2 {
			continue
		}
		if number, err := strconv.Atoi(matches[1]); err == nil {
			return number
		}
		if number := chineseNumber(matches[1]); number > 0 {
			return number
		}
	}
	return 0
}

func chineseNumber(value string) int {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	digits := map[rune]int{
		'一': 1,
		'二': 2,
		'两': 2,
		'三': 3,
		'四': 4,
		'五': 5,
		'六': 6,
		'七': 7,
		'八': 8,
		'九': 9,
	}
	if value == "十" {
		return 10
	}
	runes := []rune(value)
	if len(runes) == 1 {
		return digits[runes[0]]
	}
	parts := strings.Split(value, "十")
	if len(parts) != 2 {
		return 0
	}
	tens := 1
	if parts[0] != "" {
		tens = digits[[]rune(parts[0])[0]]
	}
	ones := 0
	if parts[1] != "" {
		ones = digits[[]rune(parts[1])[0]]
	}
	if tens == 0 {
		return 0
	}
	return tens*10 + ones
}

func positiveUniqueInts(values []int) []int {
	seen := map[int]bool{}
	result := make([]int, 0, len(values))
	for _, value := range values {
		if value <= 0 || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	return result
}
