package main

import (
	"context"
	"encoding/json"
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
	"小ai小ai",
	"小a小a",
}

var wakeWordPatterns = []struct {
	label   string
	pattern *regexp.Regexp
}{
	{label: "小艾小艾", pattern: regexp.MustCompile(`(?i)小\s*艾\s*[,，、。.\s]*小\s*艾`)},
	{label: "小爱小爱", pattern: regexp.MustCompile(`(?i)小\s*爱\s*[,，、。.\s]*小\s*爱`)},
	{label: "小艾小爱", pattern: regexp.MustCompile(`(?i)小\s*艾\s*[,，、。.\s]*小\s*爱`)},
	{label: "小爱小艾", pattern: regexp.MustCompile(`(?i)小\s*爱\s*[,，、。.\s]*小\s*艾`)},
	{label: "小AI小AI", pattern: regexp.MustCompile(`(?i)小\s*a\s*i?\s*[,，、。.\s]*小\s*a\s*i?`)},
}

const (
	interactionDebounce = 900 * time.Millisecond
	interactionWakeTTL  = 8 * time.Second
)

type PendingInteraction struct {
	WakeWord       string
	WakeTranscript string
	Instruction    string
	UpdatedAt      time.Time
	Timer          *time.Timer
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

	if wakeWord, instruction, ok := extractWakeInstruction(result.Text); ok {
		s.rememberInteraction(result.MeetingID, wakeWord, result.Text, instruction)
		return true
	}
	if s.hasPendingInteraction(result.MeetingID) {
		s.appendPendingInteractionInstruction(result.MeetingID, result.Text)
		return true
	}
	if isWakePrefix(result.Text) {
		s.rememberInteraction(result.MeetingID, "", result.Text, "")
		return true
	}
	return false
}

func (s *Server) rememberInteraction(meetingID, wakeWord, wakeTranscript, instruction string) {
	s.interactionMu.Lock()
	defer s.interactionMu.Unlock()
	s.ensureInteractionMapsLocked()

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
	}
	pending.WakeTranscript = strings.TrimSpace(wakeTranscript)
	pending.Instruction = strings.TrimSpace(instruction)
	pending.UpdatedAt = time.Now()
	s.schedulePendingInteractionLocked(meetingID, pending)
}

func (s *Server) appendPendingInteractionInstruction(meetingID, instruction string) {
	s.interactionMu.Lock()
	defer s.interactionMu.Unlock()
	s.ensureInteractionMapsLocked()

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
	s.handleMeetingInteraction(context.Background(), stt.TranscriptResult{
		MeetingID: meetingID,
		Text:      rawTranscript,
		IsFinal:   true,
		Provider:  "interaction",
	}, wakeWord, instruction)
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
	if result.MeetingID == "" {
		return
	}
	event := InteractionEvent{
		MeetingID:       result.MeetingID,
		RawTranscript:   result.Text,
		WakeWord:        wakeWord,
		Instruction:     instruction,
		ExecutionStatus: "failed",
		CreatedAt:       nowString(),
	}

	command, commandJSON, err := s.parseInteractionCommand(ctx, result.MeetingID, instruction)
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

	snapshot, message, err := s.executeInteractionCommand(ctx, result.MeetingID, command)
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

func (s *Server) parseInteractionCommand(ctx context.Context, meetingID, instruction string) (InteractionCommand, string, error) {
	actionItems, err := s.store.ListActionItems(ctx, meetingID)
	if err != nil {
		return InteractionCommand{}, "", err
	}

	cfg, err := s.store.GetLLMConfigFor(ctx, LLMUseCaseInteraction)
	if err != nil {
		return InteractionCommand{}, "", err
	}
	if strings.TrimSpace(cfg.BaseURL) != "" && strings.TrimSpace(cfg.Model) != "" {
		llmCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
		defer cancel()
		command, content, err := s.callLLMForInteraction(llmCtx, cfg, instruction, actionItems)
		if err == nil {
			return normalizeInteractionCommand(command), content, nil
		}
		log.Printf("interaction llm failed meeting=%s: %v", meetingID, err)
	}

	command, err := parseDeleteActionItemLocally(instruction)
	if err != nil {
		return InteractionCommand{}, "", err
	}
	command = normalizeInteractionCommand(command)
	bytes, _ := json.Marshal(command)
	return command, string(bytes), nil
}

func (s *Server) callLLMForInteraction(ctx context.Context, cfg *LLMConfig, instruction string, actionItems []MeetingActionItem) (InteractionCommand, string, error) {
	type actionItemContext struct {
		Index   int    `json:"index"`
		ID      string `json:"id"`
		Task    string `json:"task"`
		Owner   string `json:"owner,omitempty"`
		DueDate string `json:"dueDate,omitempty"`
		Status  string `json:"status"`
	}
	items := make([]actionItemContext, 0, len(actionItems))
	for index, item := range actionItems {
		items = append(items, actionItemContext{
			Index:   index + 1,
			ID:      item.ID,
			Task:    item.Task,
			Owner:   item.Owner,
			DueDate: item.DueDate,
			Status:  item.Status,
		})
	}
	payload := map[string]any{
		"instruction":  instruction,
		"action_items": items,
		"supported_intents": []string{
			"delete_action_item",
			"unsupported",
		},
	}
	payloadBytes, _ := json.MarshalIndent(payload, "", "  ")

	systemPrompt := `你是会议助手“小艾”的语音指令理解模块。你只把用户指令解析成可执行 JSON，不直接执行操作。
只输出合法 JSON，不要输出解释、Markdown 或代码块。
当前只支持删除待办事项。字段固定为：
{
  "intent": "delete_action_item 或 unsupported",
  "target_type": "action_item",
  "target_index": 1,
  "target_id": "如能从列表确定则填写 id，否则为空字符串",
  "confidence": 0.0,
  "need_confirm": false,
  "reply": "给用户看的简短反馈",
  "reason": "无法执行或置信度低时说明原因"
}
规则：
1. “第三条待办”“待办事项的第3条”“删掉第三个”都应解析为 target_index=3。
2. 如果用户要删除待办，但目标不明确，intent 仍为 delete_action_item，need_confirm=true。
3. 不要把 action_items 中不存在的条目当成存在。
4. 除 delete_action_item 之外的指令返回 unsupported。`

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

func (s *Server) executeInteractionCommand(ctx context.Context, meetingID string, command InteractionCommand) (*IntelligenceSnapshot, string, error) {
	if command.Intent != "delete_action_item" || command.TargetType != "action_item" {
		return nil, "暂不支持这个语音指令。", fmt.Errorf("unsupported interaction intent: %s", command.Intent)
	}
	if command.NeedConfirm {
		return nil, "这条指令还需要确认目标。", fmt.Errorf("interaction target needs confirmation")
	}

	actionItems, err := s.store.ListActionItems(ctx, meetingID)
	if err != nil {
		return nil, "读取待办失败。", err
	}
	targetID := strings.TrimSpace(command.TargetID)
	targetIndex := command.TargetIndex
	if targetID == "" {
		if targetIndex <= 0 || targetIndex > len(actionItems) {
			return nil, "没有找到要删除的待办。", fmt.Errorf("action item index %d out of range", targetIndex)
		}
		targetID = actionItems[targetIndex-1].ID
	}
	if targetIndex <= 0 {
		for index, item := range actionItems {
			if item.ID == targetID {
				targetIndex = index + 1
				break
			}
		}
	}

	deleted, err := s.store.DeleteActionItem(ctx, meetingID, targetID)
	if err != nil {
		return nil, "删除待办失败。", err
	}
	snapshot, err := s.store.GetIntelligenceSnapshot(ctx, meetingID)
	if err != nil {
		return nil, "待办已删除，但刷新结构化结果失败。", err
	}
	message := fmt.Sprintf("已删除第 %d 条待办：%s", targetIndex, deleted.Task)
	return snapshot, message, nil
}

func (s *Server) saveAndBroadcastInteraction(ctx context.Context, event InteractionEvent, snapshot *IntelligenceSnapshot) {
	saved, err := s.store.SaveInteractionEvent(ctx, event)
	if err != nil {
		log.Printf("save interaction event failed meeting=%s: %v", event.MeetingID, err)
		saved = &event
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

func normalizeInteractionCommand(command InteractionCommand) InteractionCommand {
	command.Intent = strings.TrimSpace(command.Intent)
	command.TargetType = strings.TrimSpace(command.TargetType)
	command.TargetID = strings.TrimSpace(command.TargetID)
	command.Reply = strings.TrimSpace(command.Reply)
	command.Reason = strings.TrimSpace(command.Reason)
	if command.TargetType == "" && command.Intent == "delete_action_item" {
		command.TargetType = "action_item"
	}
	return command
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
	tenIndex := strings.IndexRune(value, '十')
	if tenIndex < 0 {
		return 0
	}
	parts := strings.Split(value, "十")
	tens := 1
	if parts[0] != "" {
		tens = digits[[]rune(parts[0])[0]]
	}
	ones := 0
	if len(parts) > 1 && parts[1] != "" {
		ones = digits[[]rune(parts[1])[0]]
	}
	if tens == 0 {
		return 0
	}
	return tens*10 + ones
}
