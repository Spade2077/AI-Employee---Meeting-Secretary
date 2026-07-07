// 本文件负责会中语音交互的唤醒词检测、指令防抖聚合和入口调度。
package main

import (
	"context"
	"regexp"
	"strings"
	"time"

	"meeting-core-service/internal/stt"
)

var wakeWords = []string{
	"会议助手",
}

var wakeWordPatterns = []struct {
	label   string
	pattern *regexp.Regexp
}{
	{label: "会议助手", pattern: regexp.MustCompile(`(?i)会\s*议\s*[,，、。.!！？?\s]*助\s*手`)},
}

const (
	interactionDebounce           = 1500 * time.Millisecond
	interactionIncompleteDebounce = 2200 * time.Millisecond
	interactionWakeTTL            = 8 * time.Second
	interactionEchoTTL            = 18 * time.Second
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

type recentInteractionTranscript struct {
	Text   string
	SeenAt time.Time
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
	text := strings.TrimSpace(result.Text)
	if !result.IsFinal || result.MeetingID == "" || text == "" {
		return false
	}
	receivedAt := time.Now()
	if wakeWord, instruction, ok := extractWakeInstruction(text); ok {
		if isEnterInteractionModeCommand(instruction) {
			s.enableInteractionEditMode(result.MeetingID, "已进入修改模式，30 秒内后续语音将作为修改指令。", receivedAt)
			return true
		}
		if isExitInteractionModeCommand(instruction) {
			s.disableInteractionEditMode(result.MeetingID, "已退出修改模式，后续语音将继续进入全文转写。")
			return true
		}
		s.rememberInteraction(result.MeetingID, wakeWord, text, instruction, receivedAt)
		return true
	}

	if s.interactionEditModeActive(result.MeetingID, receivedAt) {
		if isExitInteractionModeCommand(text) {
			s.disableInteractionEditMode(result.MeetingID, "已退出修改模式，后续语音将继续进入全文转写。")
			return true
		}
		if isEnterInteractionModeCommand(text) {
			s.refreshInteractionEditMode(result.MeetingID, "修改模式倒计时已刷新。", receivedAt)
			return true
		}
		if s.hasPendingInteraction(result.MeetingID) {
			s.appendPendingInteractionInstruction(result.MeetingID, text, receivedAt)
			s.refreshInteractionEditMode(result.MeetingID, "修改模式倒计时已刷新。", receivedAt)
			return true
		}
		if s.isRecentInteractionEcho(result.MeetingID, text, receivedAt) {
			s.refreshInteractionEditMode(result.MeetingID, "修改模式倒计时已刷新。", receivedAt)
			return true
		}
		s.refreshInteractionEditMode(result.MeetingID, "修改模式倒计时已刷新。", receivedAt)
		s.rememberInteraction(result.MeetingID, "修改模式", "", text, receivedAt)
		return true
	}

	if s.hasPendingInteraction(result.MeetingID) {
		s.appendPendingInteractionInstruction(result.MeetingID, text, receivedAt)
		return true
	}
	if s.isRecentInteractionEcho(result.MeetingID, text, receivedAt) {
		return true
	}
	if isWakePrefix(text) {
		s.rememberInteraction(result.MeetingID, "", text, "", receivedAt)
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
	s.rememberRecentInteractionTranscriptLocked(meetingID, receivedAt, pending.WakeTranscript, pending.Instruction)
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
	s.rememberRecentInteractionTranscriptLocked(meetingID, receivedAt, pending.Instruction, instruction)
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
		if looksLikeIncompleteInteractionInstruction(pending.Instruction) {
			delay = interactionIncompleteDebounce
		}
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
	if looksClearlyIncompleteInteractionInstruction(instruction) {
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
	if s.recentInteractions == nil {
		s.recentInteractions = map[string][]recentInteractionTranscript{}
	}
	if s.interactionModes == nil {
		s.interactionModes = map[string]*InteractionModeState{}
	}
}

func (s *Server) cancelPendingInteractionLocked(meetingID string) {
	pending := s.pendingInteractions[meetingID]
	if pending != nil && pending.Timer != nil {
		pending.Timer.Stop()
	}
	delete(s.pendingInteractions, meetingID)
}

func (s *Server) rememberRecentInteractionTranscriptLocked(meetingID string, seenAt time.Time, values ...string) {
	s.ensureInteractionMapsLocked()
	if seenAt.IsZero() {
		seenAt = time.Now()
	}
	cutoff := seenAt.Add(-interactionEchoTTL)
	current := s.recentInteractions[meetingID]
	next := make([]recentInteractionTranscript, 0, len(current)+len(values))
	for _, item := range current {
		if item.SeenAt.After(cutoff) && normalizeForCompare(item.Text) != "" {
			next = append(next, item)
		}
	}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if normalizeForCompare(value) == "" {
			continue
		}
		next = append(next, recentInteractionTranscript{Text: value, SeenAt: seenAt})
	}
	if len(next) > 12 {
		next = next[len(next)-12:]
	}
	s.recentInteractions[meetingID] = next
}

func (s *Server) isRecentInteractionEcho(meetingID, text string, seenAt time.Time) bool {
	key := normalizeForCompare(text)
	if key == "" {
		return false
	}
	s.interactionMu.Lock()
	defer s.interactionMu.Unlock()
	s.ensureInteractionMapsLocked()

	if seenAt.IsZero() {
		seenAt = time.Now()
	}
	cutoff := seenAt.Add(-interactionEchoTTL)
	items := s.recentInteractions[meetingID]
	filtered := make([]recentInteractionTranscript, 0, len(items))
	matched := false
	for _, item := range items {
		if item.SeenAt.Before(cutoff) {
			continue
		}
		filtered = append(filtered, item)
		if matched {
			continue
		}
		if interactionTextsLikelySame(item.Text, text) {
			matched = true
		}
	}
	s.recentInteractions[meetingID] = filtered
	return matched
}

func interactionTextsLikelySame(previous, incoming string) bool {
	left := normalizeForCompare(previous)
	right := normalizeForCompare(incoming)
	if left == "" || right == "" {
		return false
	}
	minLen := minInt(len([]rune(left)), len([]rune(right)))
	if minLen < 8 {
		return false
	}
	if strings.Contains(left, right) || strings.Contains(right, left) {
		return true
	}
	return structuredTextSimilarity(left, right) >= 0.72 && looksLikeInteractionCommandText(incoming)
}

func looksLikeIncompleteInteractionInstruction(instruction string) bool {
	text := strings.TrimSpace(instruction)
	if text == "" || !looksLikeInteractionCommandText(text) {
		return false
	}
	if looksClearlyIncompleteInteractionInstruction(text) {
		return true
	}

	key := normalizeForCompare(text)
	if !containsNormalizedPhrase(key, "新增", "添加", "补充", "创建", "记录", "修改", "改成", "改为", "调整为", "更新为", "设置为", "设为") {
		return false
	}
	payload := interactionInstructionPayload(text)
	if payload == "" {
		return true
	}
	payloadLen := len([]rune(normalizeForCompare(payload)))
	if containsNormalizedPhrase(key, "动态模块", "自定义模块", "发现模块", "模块", "栏目", "板块", "专题") {
		return payloadLen > 0 && payloadLen < 4
	}
	return payloadLen > 0 && payloadLen < 6
}

func looksClearlyIncompleteInteractionInstruction(instruction string) bool {
	text := strings.TrimSpace(instruction)
	key := normalizeForCompare(text)
	if key == "" {
		return true
	}
	if containsNormalizedPhrase(key, "删除", "删掉", "去掉", "移除", "关闭", "完成", "解决", "撤销", "撤回", "展示", "显示", "隐藏", "切换") {
		return false
	}
	for _, suffix := range []string{
		"改成", "改为", "调整为", "更新为", "设置为", "设为", "替换为", "变成",
		"合并为", "合并成", "改名为", "重命名为", "命名为",
		"新增一个", "新增一条", "添加一个", "添加一条", "补充一个", "补充一条", "创建一个", "创建一条",
	} {
		if strings.HasSuffix(key, normalizeForCompare(suffix)) {
			return true
		}
	}
	if containsNormalizedPhrase(key, "改成", "改为", "调整为", "更新为", "设置为", "设为", "替换为", "变成", "合并为", "合并成") {
		return interactionInstructionPayload(text) == ""
	}
	if containsNormalizedPhrase(key, "新增", "添加", "补充", "创建", "记录") &&
		containsNormalizedPhrase(key, "待办", "任务", "风险", "决策", "待确认问题", "动态模块", "模块") {
		return interactionInstructionPayload(text) == ""
	}
	return false
}

func interactionInstructionPayload(text string) string {
	markers := []string{
		"改成", "改为", "调整为", "更新为", "设置为", "设为", "替换为", "变成",
		"合并为", "合并成", "整合为", "归并为",
		"改名为", "重命名为", "命名为", "名字改成", "名字改为", "标题改成", "标题改为",
		"新增一个待确认问题", "新增一条待确认问题", "新增一个确认问题", "新增一条确认问题",
		"新增一个待办", "新增一条待办", "新增一个任务", "新增一条任务",
		"新增一个风险", "新增一条风险", "新增一个决策", "新增一条决策",
		"新增一个动态模块", "新增一条动态模块", "新增一个模块", "新增一条模块",
		"添加一个待确认问题", "添加一条待确认问题", "添加一个待办", "添加一条待办",
		"添加一个风险", "添加一条风险", "添加一个决策", "添加一条决策",
		"补充一个待确认问题", "补充一条待确认问题", "补充一个待办", "补充一条待办",
		"补充一个风险", "补充一条风险", "补充一个决策", "补充一条决策",
		"创建一个待确认问题", "创建一条待确认问题", "创建一个待办", "创建一条待办",
		"创建一个风险", "创建一条风险", "创建一个决策", "创建一条决策",
		"记录一个待确认问题", "记录一条待确认问题", "记录一个待办", "记录一条待办",
		"记录一个风险", "记录一条风险", "记录一个决策", "记录一条决策",
		"新增一个", "新增一条", "新增", "添加一个", "添加一条", "添加",
		"补充一个", "补充一条", "补充", "创建一个", "创建一条", "创建",
		"记录一个", "记录一条", "记录",
		"内容是", "内容为", "是", "为",
	}
	return cleanupDynamicSectionValue(textAfterAnyMarker(text, markers))
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
	command.Instruction = strings.TrimSpace(instruction)
	event.Intent = command.Intent
	event.TargetType = command.TargetType
	event.TargetIndex = command.TargetIndex
	event.TargetID = command.TargetID
	event.CommandJSON = commandJSON
	if command.Intent == "correct_last_interaction" {
		s.broadcastInteractionStatus(result.MeetingID, event.ID, "executing", "已识别口误纠错，正在撤销上一条并执行纠正后的指令")
		executeStarted := time.Now()
		snapshot, message, correctedCommand, correctedJSON, err := s.executeCorrectionInteraction(ctx, result.MeetingID, event.ID, instruction, command.Content, timing)
		timing.addStage("execute_command", "执行口误纠错", time.Since(executeStarted), correctedCommand.Intent)
		if correctedCommand.Intent != "" {
			event.Intent = correctedCommand.Intent
			event.TargetType = correctedCommand.TargetType
			event.TargetIndex = correctedCommand.TargetIndex
			event.TargetID = correctedCommand.TargetID
			if correctedJSON == "" {
				correctedJSON = interactionCommandJSON(correctedCommand)
			}
			event.CommandJSON = correctedJSON
		}
		if err != nil {
			event.Message = message
			event.ErrorMessage = err.Error()
			s.saveAndBroadcastInteraction(ctx, event, snapshot)
			return
		}
		event.ExecutionStatus = "success"
		event.Message = message
		s.saveAndBroadcastInteraction(ctx, event, snapshot)
		return
	}
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
