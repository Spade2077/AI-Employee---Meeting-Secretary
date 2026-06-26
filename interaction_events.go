// 本文件负责会中交互事件的保存、WebSocket 推送、耗时统计和展示文案。
package main

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"
)

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
