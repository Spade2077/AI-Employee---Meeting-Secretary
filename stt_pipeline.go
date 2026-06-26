// 本文件负责音频到 ASR、转写入库和 WebSocket 推送链路。
package main

import (
	"context"
	"log"
	"net/http"
	"strings"
	"time"

	"meeting-core-service/internal/stt"
)

func (s *Server) sendAudioToSTT(ctx context.Context, chunk stt.AudioChunk) error {
	// Volcano sessions can expire if no packet arrives quickly enough. Lazily
	// reconnecting here makes pause/resume and browser-mic retries forgiving.
	if err := s.sttAdapter.SendAudio(ctx, chunk); err == nil {
		return nil
	}

	if err := s.sttAdapter.StartSession(ctx, chunk.MeetingID); err != nil {
		return err
	}
	s.noteSTTRestart(chunk.MeetingID)
	if err := s.sttAdapter.SendAudio(ctx, chunk); err != nil {
		_ = s.sttAdapter.StopSession(context.Background(), chunk.MeetingID)
		return err
	}
	return nil
}

func (s *Server) noteSTTRestart(meetingID string) {
	s.sttMu.Lock()
	defer s.sttMu.Unlock()
	if s.sttRestartAt == nil {
		s.sttRestartAt = map[string]time.Time{}
	}
	s.sttRestartAt[meetingID] = time.Now()
}

func (s *Server) noteSTTResult(meetingID string) {
	s.sttMu.Lock()
	defer s.sttMu.Unlock()
	if s.sttResultAt == nil {
		s.sttResultAt = map[string]time.Time{}
	}
	s.sttResultAt[meetingID] = time.Now()
}

func (s *Server) maybeRestartStuckSTT(ctx context.Context, meetingID string, peak int) {
	const voicePeakThreshold = 80
	if peak < voicePeakThreshold {
		return
	}

	now := time.Now()
	shouldRestart := false

	s.sttMu.Lock()
	if s.sttSeenAt == nil {
		s.sttSeenAt = map[string]time.Time{}
	}
	if s.sttResultAt == nil {
		s.sttResultAt = map[string]time.Time{}
	}
	if s.sttRestartAt == nil {
		s.sttRestartAt = map[string]time.Time{}
	}
	s.sttSeenAt[meetingID] = now

	lastResult := s.sttResultAt[meetingID]
	lastRestart := s.sttRestartAt[meetingID]
	if lastRestart.IsZero() {
		s.sttRestartAt[meetingID] = now
		lastRestart = now
	}

	baseline := lastResult
	if baseline.IsZero() || lastRestart.After(baseline) {
		baseline = lastRestart
	}
	if now.Sub(baseline) > 18*time.Second && now.Sub(lastRestart) > 18*time.Second {
		s.sttRestartAt[meetingID] = now
		shouldRestart = true
	}
	s.sttMu.Unlock()

	if !shouldRestart {
		return
	}

	log.Printf("stt watchdog restart meeting=%s peak=%d no_result_for=%s", meetingID, peak, now.Sub(s.latestSTTResultAt(meetingID)).Truncate(time.Second))
	go func() {
		restartCtx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
		defer cancel()
		if err := s.sttAdapter.StartSession(restartCtx, meetingID); err != nil {
			log.Printf("stt watchdog restart failed meeting=%s: %v", meetingID, err)
			return
		}
		log.Printf("stt watchdog restart complete meeting=%s", meetingID)
	}()
}

func (s *Server) latestSTTResultAt(meetingID string) time.Time {
	s.sttMu.Lock()
	defer s.sttMu.Unlock()
	return s.sttResultAt[meetingID]
}

func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("websocket upgrade: %v", err)
		return
	}
	s.hub.Add(conn)
	defer s.hub.Remove(conn)

	meeting, err := s.store.CurrentMeeting(r.Context())
	if err == nil {
		_ = conn.WriteJSON(s.stateUpdate(meeting, nil))
	}

	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			return
		}
	}
}

func (s *Server) consumeSTT(ctx context.Context) {
	for {
		select {
		case result, ok := <-s.sttAdapter.Results():
			if !ok {
				return
			}
			if result.MeetingID == "" || strings.TrimSpace(result.Text) == "" {
				continue
			}
			s.noteSTTResult(result.MeetingID)
			if s.consumeInteractionTranscript(result) {
				continue
			}
			utterance, err := s.store.AddTranscript(ctx, result)
			if err != nil {
				log.Printf("store transcript failed meeting=%s provider=%s: %v", result.MeetingID, result.Provider, err)
				continue
			}
			s.hub.Broadcast(s.transcriptDelta(result.MeetingID, utterance))
			if utterance.IsFinal {
				go s.scheduleIntelligenceAnalysis(context.Background(), result.MeetingID)
			}

		case adapterErr, ok := <-s.sttAdapter.Errors():
			if !ok {
				return
			}
			log.Printf("stt error provider=%s meeting=%s: %s", adapterErr.Provider, adapterErr.MeetingID, adapterErr.Message)
			s.hub.Broadcast(STTErrorEvent{
				Type:      "stt_error",
				MeetingID: adapterErr.MeetingID,
				Provider:  adapterErr.Provider,
				Message:   adapterErr.Message,
				Timestamp: nowString(),
			})

		case <-ctx.Done():
			return
		}
	}
}
