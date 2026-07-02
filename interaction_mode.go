// 本文件负责维护会中交互的连续修改模式，包括超时保护和前端状态推送。
package main

import (
	"strings"
	"time"
)

const (
	interactionModeNormal  = "normal"
	interactionModeEditing = "editing"
	interactionEditModeTTL = 30 * time.Second
)

type InteractionModeState struct {
	Mode      string
	Active    bool
	ExpiresAt time.Time
	Timer     *time.Timer
}

func isEnterInteractionModeCommand(text string) bool {
	key := normalizeForCompare(text)
	if key == "" || isExitInteractionModeCommand(text) {
		return false
	}
	patterns := []string{
		"进入修改模式",
		"开启修改模式",
		"打开修改模式",
		"启动修改模式",
		"进入编辑模式",
		"开启编辑模式",
		"打开编辑模式",
		"启动编辑模式",
		"进入连续修改",
		"开启连续修改",
		"连续修改模式",
		"修改模式",
		"编辑模式",
	}
	for _, pattern := range patterns {
		if strings.Contains(key, normalizeForCompare(pattern)) {
			return true
		}
	}
	return false
}

func isExitInteractionModeCommand(text string) bool {
	key := normalizeForCompare(text)
	if key == "" {
		return false
	}
	patterns := []string{
		"退出修改模式",
		"关闭修改模式",
		"结束修改模式",
		"停止修改模式",
		"退出编辑模式",
		"关闭编辑模式",
		"结束编辑模式",
		"停止编辑模式",
		"回到记录模式",
		"恢复记录模式",
		"继续记录",
		"恢复转写",
		"正常转写",
	}
	for _, pattern := range patterns {
		if strings.Contains(key, normalizeForCompare(pattern)) {
			return true
		}
	}
	return false
}

func (s *Server) enableInteractionEditMode(meetingID, reason string, now time.Time) {
	if meetingID == "" {
		return
	}
	if now.IsZero() {
		now = time.Now()
	}

	s.interactionMu.Lock()
	s.ensureInteractionMapsLocked()
	state := s.interactionModes[meetingID]
	if state == nil {
		state = &InteractionModeState{}
		s.interactionModes[meetingID] = state
	}
	if state.Timer != nil {
		state.Timer.Stop()
	}
	state.Mode = interactionModeEditing
	state.Active = true
	state.ExpiresAt = now.Add(interactionEditModeTTL)
	s.scheduleInteractionModeExpiryLocked(meetingID, state)
	update := s.interactionModeUpdateLocked(meetingID, state, reason, now)
	s.interactionMu.Unlock()

	s.broadcastInteractionMode(update)
}

func (s *Server) disableInteractionEditMode(meetingID, reason string) {
	if meetingID == "" {
		return
	}
	now := time.Now()

	s.interactionMu.Lock()
	s.ensureInteractionMapsLocked()
	state := s.interactionModes[meetingID]
	if state == nil {
		state = &InteractionModeState{}
		s.interactionModes[meetingID] = state
	}
	if state.Timer != nil {
		state.Timer.Stop()
		state.Timer = nil
	}
	state.Mode = interactionModeNormal
	state.Active = false
	state.ExpiresAt = time.Time{}
	update := s.interactionModeUpdateLocked(meetingID, state, reason, now)
	s.interactionMu.Unlock()

	s.broadcastInteractionMode(update)
}

func (s *Server) refreshInteractionEditMode(meetingID, reason string, now time.Time) {
	if meetingID == "" {
		return
	}
	if now.IsZero() {
		now = time.Now()
	}

	s.interactionMu.Lock()
	s.ensureInteractionMapsLocked()
	state := s.interactionModes[meetingID]
	if state == nil || !state.Active || state.Mode != interactionModeEditing {
		s.interactionMu.Unlock()
		return
	}
	if state.Timer != nil {
		state.Timer.Stop()
	}
	state.ExpiresAt = now.Add(interactionEditModeTTL)
	s.scheduleInteractionModeExpiryLocked(meetingID, state)
	update := s.interactionModeUpdateLocked(meetingID, state, reason, now)
	s.interactionMu.Unlock()

	s.broadcastInteractionMode(update)
}

func (s *Server) interactionEditModeActive(meetingID string, now time.Time) bool {
	if meetingID == "" {
		return false
	}
	if now.IsZero() {
		now = time.Now()
	}

	s.interactionMu.Lock()
	s.ensureInteractionMapsLocked()
	state := s.interactionModes[meetingID]
	if state == nil || !state.Active || state.Mode != interactionModeEditing {
		s.interactionMu.Unlock()
		return false
	}
	if !state.ExpiresAt.IsZero() && !now.Before(state.ExpiresAt) {
		if state.Timer != nil {
			state.Timer.Stop()
			state.Timer = nil
		}
		state.Mode = interactionModeNormal
		state.Active = false
		state.ExpiresAt = time.Time{}
		update := s.interactionModeUpdateLocked(meetingID, state, "修改模式已超时，已回到记录模式。", now)
		s.interactionMu.Unlock()
		s.broadcastInteractionMode(update)
		return false
	}
	s.interactionMu.Unlock()
	return true
}

func (s *Server) currentInteractionModeUpdate(meetingID string) InteractionModeUpdate {
	now := time.Now()
	s.interactionMu.Lock()
	s.ensureInteractionMapsLocked()
	state := s.interactionModes[meetingID]
	if state == nil {
		state = &InteractionModeState{Mode: interactionModeNormal}
		s.interactionModes[meetingID] = state
	}
	update := s.interactionModeUpdateLocked(meetingID, state, "", now)
	s.interactionMu.Unlock()
	return update
}

func (s *Server) scheduleInteractionModeExpiryLocked(meetingID string, state *InteractionModeState) {
	expiresAt := state.ExpiresAt
	delay := time.Until(expiresAt)
	if delay < 0 {
		delay = 0
	}
	state.Timer = time.AfterFunc(delay, func() {
		now := time.Now()
		s.interactionMu.Lock()
		s.ensureInteractionMapsLocked()
		current := s.interactionModes[meetingID]
		if current == nil || !current.Active || current.Mode != interactionModeEditing || current.ExpiresAt.After(now) {
			s.interactionMu.Unlock()
			return
		}
		current.Mode = interactionModeNormal
		current.Active = false
		current.ExpiresAt = time.Time{}
		current.Timer = nil
		update := s.interactionModeUpdateLocked(meetingID, current, "修改模式已超时，已回到记录模式。", now)
		s.interactionMu.Unlock()
		s.broadcastInteractionMode(update)
	})
}

func (s *Server) interactionModeUpdateLocked(meetingID string, state *InteractionModeState, reason string, now time.Time) InteractionModeUpdate {
	mode := interactionModeNormal
	active := false
	expiresAt := ""
	remaining := 0
	if state != nil && state.Active && state.Mode == interactionModeEditing {
		mode = interactionModeEditing
		active = true
		if !state.ExpiresAt.IsZero() {
			expiresAt = state.ExpiresAt.UTC().Format(time.RFC3339Nano)
			remaining = int(state.ExpiresAt.Sub(now) / time.Second)
			if state.ExpiresAt.After(now) && state.ExpiresAt.Sub(now)%time.Second != 0 {
				remaining++
			}
			if remaining < 0 {
				remaining = 0
			}
		}
	}
	return InteractionModeUpdate{
		Type:             "interaction_mode_update",
		MeetingID:        meetingID,
		Mode:             mode,
		Active:           active,
		ExpiresAt:        expiresAt,
		RemainingSeconds: remaining,
		Reason:           reason,
		Timestamp:        now.UTC().Format(time.RFC3339Nano),
	}
}

func (s *Server) broadcastInteractionMode(update InteractionModeUpdate) {
	if s.hub != nil {
		s.hub.Broadcast(update)
	}
}
