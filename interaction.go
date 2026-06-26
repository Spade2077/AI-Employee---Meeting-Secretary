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
	command.Instruction = strings.TrimSpace(instruction)
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
