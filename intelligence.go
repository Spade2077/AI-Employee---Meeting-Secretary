// 本文件负责会议结构化分析的触发、调度和结果广播。
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"
)

const (
	intelligenceDebounce    = 8 * time.Second
	intelligenceMinInterval = 10 * time.Second
	intelligenceMinNewChars = 250
	intelligenceBusyRetry   = 2 * time.Second
	intelligenceLLMTimeout  = 150 * time.Second
)

func (s *Server) scheduleIntelligenceAnalysis(ctx context.Context, meetingID string) {
	sourceLen, err := s.currentIntelligenceSourceLen(ctx, meetingID)
	if err != nil {
		log.Printf("measure intelligence source failed meeting=%s: %v", meetingID, err)
		return
	}
	if sourceLen == 0 {
		return
	}

	now := time.Now()
	shouldRun := false
	delay := intelligenceDebounce

	s.intelMu.Lock()
	s.ensureIntelligenceMapsLocked()
	lastRun := s.intelLastRun[meetingID]
	lastLen := s.intelLastLen[meetingID]
	if remaining := intelligenceMinInterval - now.Sub(lastRun); !lastRun.IsZero() && remaining > delay {
		delay = remaining
	}

	if sourceLen-lastLen >= intelligenceMinNewChars &&
		(lastRun.IsZero() || now.Sub(lastRun) >= intelligenceMinInterval) &&
		!s.intelBusy[meetingID] {
		s.cancelIntelligenceTimerLocked(meetingID)
		shouldRun = true
	} else {
		s.setIntelligenceTimerLocked(meetingID, delay, false)
	}
	s.intelMu.Unlock()

	if shouldRun {
		go s.runIntelligenceAnalysis(context.Background(), meetingID, false)
	}
}

func (s *Server) forceIntelligenceAnalysis(ctx context.Context, meetingID string) {
	s.runIntelligenceAnalysis(ctx, meetingID, true)
}

func (s *Server) runIntelligenceAnalysis(ctx context.Context, meetingID string, force bool) {
	s.intelMu.Lock()
	s.ensureIntelligenceMapsLocked()
	if s.intelBusy[meetingID] {
		if force {
			s.intelMu.Unlock()
			s.broadcastIntelligenceStatus(meetingID, "running", "已有结构化任务正在运行，请等待当前任务完成。")
			return
		}
		s.setIntelligenceTimerLocked(meetingID, intelligenceBusyRetry, false)
		s.intelMu.Unlock()
		return
	}
	s.intelBusy[meetingID] = true
	s.cancelIntelligenceTimerLocked(meetingID)
	s.intelMu.Unlock()
	defer func() {
		s.intelMu.Lock()
		delete(s.intelBusy, meetingID)
		s.intelMu.Unlock()
	}()

	meeting, err := s.store.GetMeeting(ctx, meetingID)
	if err != nil {
		log.Printf("intelligence skipped meeting=%s: %v", meetingID, err)
		return
	}
	if meeting.Status != string(StatusRecording) && meeting.Status != string(StatusSoftPaused) && meeting.Status != string(StatusEnded) {
		return
	}

	utterances, err := s.store.ListUtterances(ctx, meetingID)
	if err != nil {
		log.Printf("list utterances for intelligence failed meeting=%s: %v", meetingID, err)
		return
	}
	transcript := transcriptForIntelligence(utterances)
	sourceLen := intelligenceSourceLen(transcript)
	if sourceLen == 0 {
		s.broadcastIntelligenceStatus(meetingID, "no_transcript", "暂无可结构化的转写内容。")
		return
	}

	s.intelMu.Lock()
	s.ensureIntelligenceMapsLocked()
	lastRun := s.intelLastRun[meetingID]
	lastLen := s.intelLastLen[meetingID]
	if !force {
		if sourceLen <= lastLen {
			s.intelMu.Unlock()
			return
		}
		if remaining := intelligenceMinInterval - time.Since(lastRun); !lastRun.IsZero() && remaining > 0 {
			s.setIntelligenceTimerLocked(meetingID, remaining, false)
			s.intelMu.Unlock()
			return
		}
	}
	s.intelLastRun[meetingID] = time.Now()
	s.intelMu.Unlock()

	cfg, err := s.store.GetLLMConfigFor(ctx, LLMUseCaseIntelligence)
	if err != nil {
		log.Printf("load llm config failed: %v", err)
		return
	}
	if strings.TrimSpace(cfg.BaseURL) == "" || strings.TrimSpace(cfg.Model) == "" {
		s.broadcastIntelligenceStatus(meetingID, "llm_not_configured", "LLM URL and model are required before intelligence extraction.")
		return
	}

	current, err := s.store.GetIntelligenceSnapshot(ctx, meetingID)
	if err != nil {
		log.Printf("load intelligence snapshot failed meeting=%s: %v", meetingID, err)
		return
	}

	llmCtx, cancel := context.WithTimeout(ctx, intelligenceLLMTimeout)
	defer cancel()
	s.broadcastIntelligenceStatus(meetingID, "running", fmt.Sprintf("正在结构化分析会议内容，模型：%s，最长等待 %d 秒...", cfg.Model, int(intelligenceLLMTimeout.Seconds())))
	result, err := s.callLLMForIntelligence(llmCtx, cfg, transcript, current)
	if err != nil {
		log.Printf("llm intelligence failed meeting=%s: %v", meetingID, err)
		message := "结构化分析失败：" + err.Error()
		if errors.Is(err, context.DeadlineExceeded) || strings.Contains(err.Error(), "context deadline exceeded") {
			message = fmt.Sprintf("结构化分析超时：LLM 在 %d 秒内没有返回，请稍后重试，或把结构化模型换成更快的模型。", int(intelligenceLLMTimeout.Seconds()))
		}
		s.broadcastIntelligenceStatus(meetingID, "failed", message)
		s.hub.Broadcast(map[string]any{
			"type":      "intelligence_error",
			"meetingId": meetingID,
			"message":   message,
			"timestamp": nowString(),
		})
		return
	}

	snapshot, err := s.store.SaveIntelligenceExtraction(ctx, meetingID, *result, len(utterances))
	if err != nil {
		log.Printf("save intelligence failed meeting=%s: %v", meetingID, err)
		return
	}

	s.intelMu.Lock()
	s.ensureIntelligenceMapsLocked()
	s.intelLastRun[meetingID] = time.Now()
	s.intelLastLen[meetingID] = sourceLen
	s.intelMu.Unlock()

	s.hub.Broadcast(snapshot)
	s.broadcastIntelligenceStatus(meetingID, "completed", "结构化分析已完成。")
}

func (s *Server) broadcastIntelligenceStatus(meetingID, status, message string) {
	s.hub.Broadcast(map[string]any{
		"type":      "intelligence_status",
		"meetingId": meetingID,
		"status":    status,
		"message":   message,
		"timestamp": nowString(),
	})
}

func (s *Server) currentIntelligenceSourceLen(ctx context.Context, meetingID string) (int, error) {
	utterances, err := s.store.ListUtterances(ctx, meetingID)
	if err != nil {
		return 0, err
	}
	return intelligenceSourceLen(transcriptForIntelligence(utterances)), nil
}

func intelligenceSourceLen(transcript string) int {
	return len([]rune(strings.TrimSpace(transcript)))
}

func (s *Server) ensureIntelligenceMapsLocked() {
	if s.intelBusy == nil {
		s.intelBusy = map[string]bool{}
	}
	if s.intelLastRun == nil {
		s.intelLastRun = map[string]time.Time{}
	}
	if s.intelLastLen == nil {
		s.intelLastLen = map[string]int{}
	}
	if s.intelTimers == nil {
		s.intelTimers = map[string]*time.Timer{}
	}
}

func (s *Server) setIntelligenceTimerLocked(meetingID string, delay time.Duration, force bool) {
	s.cancelIntelligenceTimerLocked(meetingID)
	s.intelTimers[meetingID] = time.AfterFunc(delay, func() {
		s.runIntelligenceAnalysis(context.Background(), meetingID, force)
	})
}

func (s *Server) cancelIntelligenceTimerLocked(meetingID string) {
	timer := s.intelTimers[meetingID]
	if timer != nil {
		timer.Stop()
		delete(s.intelTimers, meetingID)
	}
}
