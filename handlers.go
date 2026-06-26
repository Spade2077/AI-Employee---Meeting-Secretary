// 本文件负责本地 HTTP API 路由和请求处理。
package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"meeting-core-service/internal/stt"
)

func (s *Server) handleLLMConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		useCase := normalizeLLMUseCase(r.URL.Query().Get("useCase"))
		cfg, err := s.store.GetLLMConfigFor(r.Context(), useCase)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, cfg)
	case http.MethodPost:
		var req struct {
			UseCase      string `json:"useCase"`
			BaseURL      string `json:"baseUrl"`
			APIKey       string `json:"apiKey"`
			Model        string `json:"model"`
			ThinkingMode string `json:"thinkingMode"`
			ClearAPIKey  bool   `json:"clearApiKey"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		useCase := normalizeLLMUseCase(req.UseCase)
		if req.UseCase == "" {
			useCase = normalizeLLMUseCase(r.URL.Query().Get("useCase"))
		}
		current, err := s.store.GetLLMConfigFor(r.Context(), useCase)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		apiKey := current.apiKey
		if req.ClearAPIKey {
			apiKey = ""
		} else if strings.TrimSpace(req.APIKey) != "" {
			apiKey = req.APIKey
		}
		cfg, err := s.store.SaveLLMConfigFor(r.Context(), useCase, LLMConfig{
			BaseURL:      req.BaseURL,
			Model:        req.Model,
			ThinkingMode: req.ThinkingMode,
			apiKey:       apiKey,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, cfg)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleState(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	meeting, err := s.store.CurrentMeeting(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, s.stateUpdate(meeting, nil))
}

func (s *Server) handleMeetings(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/api/meetings" {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req struct {
		Title    string `json:"title"`
		Type     string `json:"type"`
		Operator string `json:"operator"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)

	meeting, event, err := s.store.CreateMeeting(r.Context(), req.Title, req.Type, req.Operator)
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	update := s.stateUpdate(meeting, event)
	s.hub.Broadcast(update)
	writeJSON(w, http.StatusCreated, update)
}

func (s *Server) handleMeetingAction(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/meetings/")
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		writeError(w, http.StatusNotFound, "missing meeting id")
		return
	}
	meetingID := parts[0]

	if len(parts) == 1 && r.Method == http.MethodGet {
		meeting, err := s.store.GetMeeting(r.Context(), meetingID)
		if err != nil {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, meeting)
		return
	}

	if len(parts) < 2 {
		writeError(w, http.StatusNotFound, "missing action")
		return
	}

	action := parts[1]
	switch {
	case r.Method == http.MethodPost && action == "start":
		s.transitionHTTP(w, r, meetingID, "start")
	case r.Method == http.MethodPost && action == "pause":
		s.transitionHTTP(w, r, meetingID, "pause")
	case r.Method == http.MethodPost && action == "resume":
		s.transitionHTTP(w, r, meetingID, "resume")
	case r.Method == http.MethodPost && action == "end":
		s.endMeetingHTTP(w, r, meetingID)
	case r.Method == http.MethodPost && action == "utterances":
		s.addUtteranceHTTP(w, r, meetingID)
	case r.Method == http.MethodGet && action == "audio" && len(parts) == 3 && parts[2] == "ws":
		s.audioWebSocket(w, r, meetingID)
	case r.Method == http.MethodGet && action == "intelligence":
		if err := s.store.CleanupIntelligenceDuplicates(r.Context(), meetingID); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		snapshot, err := s.store.GetIntelligenceSnapshot(r.Context(), meetingID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, snapshot)
	case r.Method == http.MethodPost && action == "intelligence" && len(parts) == 3 && parts[2] == "analyze":
		go s.forceIntelligenceAnalysis(context.Background(), meetingID)
		writeJSON(w, http.StatusAccepted, map[string]any{
			"accepted":  true,
			"meetingId": meetingID,
		})
	case r.Method == http.MethodGet && action == "events":
		events, err := s.store.ListEvents(r.Context(), meetingID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, events)
	case r.Method == http.MethodGet && action == "utterances":
		utterances, err := s.store.ListUtterances(r.Context(), meetingID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, utterances)
	case r.Method == http.MethodGet && action == "interactions":
		events, err := s.store.ListInteractionEvents(r.Context(), meetingID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, events)
	case r.Method == http.MethodGet && action == "minutes.md":
		s.handleMinutesMarkdown(w, r, meetingID)
	case r.Method == http.MethodGet && action == "share-qr.png":
		s.handleShareQRCode(w, r, meetingID)
	default:
		writeError(w, http.StatusNotFound, "unknown action")
	}
}

func (s *Server) transitionHTTP(w http.ResponseWriter, r *http.Request, meetingID, eventType string) {
	current, err := s.store.GetMeeting(r.Context(), meetingID)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	_, _, err = nextStatus(current, eventType)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	meeting, event, err := s.store.Transition(r.Context(), meetingID, eventType, operatorFromRequest(r))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	update := s.stateUpdate(meeting, event)
	s.hub.Broadcast(update)
	writeJSON(w, http.StatusOK, update)
}

func (s *Server) endMeetingHTTP(w http.ResponseWriter, r *http.Request, meetingID string) {
	meeting, event, err := s.store.Transition(r.Context(), meetingID, "ending", operatorFromRequest(r))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.hub.Broadcast(s.stateUpdate(meeting, event))
	if err := s.sttAdapter.StopSession(r.Context(), meetingID); err != nil {
		log.Printf("stop stt session failed meeting=%s: %v", meetingID, err)
	}

	meeting, event, err = s.store.Transition(r.Context(), meetingID, "finish", "system")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	update := s.stateUpdate(meeting, event)
	s.hub.Broadcast(update)
	go s.forceIntelligenceAnalysis(context.Background(), meetingID)
	writeJSON(w, http.StatusOK, update)
}

func (s *Server) addUtteranceHTTP(w http.ResponseWriter, r *http.Request, meetingID string) {
	var req struct {
		Text         string `json:"text"`
		SpeakerLabel string `json:"speakerLabel"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	testResult := stt.TranscriptResult{
		MeetingID:    meetingID,
		SpeakerLabel: req.SpeakerLabel,
		Text:         req.Text,
		IsFinal:      true,
		Provider:     "manual",
		Confidence:   1,
	}
	if s.consumeInteractionTranscript(testResult) {
		writeJSON(w, http.StatusAccepted, map[string]any{
			"accepted":  true,
			"meetingId": meetingID,
			"type":      "interaction_queued",
		})
		return
	}
	u, err := s.store.AddUtterance(r.Context(), meetingID, req.SpeakerLabel, req.Text)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	delta := s.transcriptDelta(meetingID, u)
	s.hub.Broadcast(delta)
	if u.IsFinal {
		go s.scheduleIntelligenceAnalysis(context.Background(), meetingID)
	}
	writeJSON(w, http.StatusCreated, delta)
}

func (s *Server) audioWebSocket(w http.ResponseWriter, r *http.Request, meetingID string) {
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("audio websocket upgrade: %v", err)
		return
	}
	startedAt := time.Now()
	packetCount := 0
	byteCount := 0
	log.Printf("audio websocket connected meeting=%s remote=%s", meetingID, r.RemoteAddr)
	defer func() {
		log.Printf("audio websocket closed meeting=%s packets=%d bytes=%d duration=%s",
			meetingID, packetCount, byteCount, time.Since(startedAt).Truncate(time.Millisecond))
	}()
	defer conn.Close()

	for {
		msgType, data, err := conn.ReadMessage()
		if err != nil {
			log.Printf("audio websocket read stopped meeting=%s: %v", meetingID, err)
			return
		}

		switch msgType {
		case websocket.BinaryMessage:
			packetCount++
			byteCount += len(data)
			peak := pcm16Peak(data)
			if packetCount == 1 || packetCount%100 == 0 {
				log.Printf("audio websocket packets meeting=%s packets=%d bytes=%d peak=%d", meetingID, packetCount, byteCount, peak)
			}
			if packetCount == 1 || packetCount%20 == 0 {
				_ = conn.WriteJSON(map[string]any{
					"type":      "audio_metrics",
					"meetingId": meetingID,
					"packets":   packetCount,
					"bytes":     byteCount,
					"peak":      peak,
					"timestamp": nowString(),
				})
			}
			meeting, err := s.store.GetMeeting(r.Context(), meetingID)
			if err != nil {
				_ = conn.WriteJSON(map[string]any{
					"type":  "error",
					"error": err.Error(),
				})
				continue
			}
			status := MeetingStatus(meeting.Status)
			if status == StatusSoftPaused {
				if err := s.sendAudioToSTT(r.Context(), stt.AudioChunk{
					MeetingID: meetingID,
					Data:      make([]byte, len(data)),
					Format:    "pcm_s16le_16k_mono/silence",
					Timestamp: time.Now().UTC(),
				}); err != nil {
					log.Printf("send pause keepalive to stt failed meeting=%s packets=%d: %v", meetingID, packetCount, err)
					_ = conn.WriteJSON(map[string]any{
						"type":  "error",
						"error": err.Error(),
					})
				}
				continue
			}
			if !CanUploadAudio(status) {
				_ = conn.WriteJSON(map[string]any{
					"type":   "audio_blocked",
					"status": meeting.Status,
				})
				continue
			}
			if err := s.sendAudioToSTT(r.Context(), stt.AudioChunk{
				MeetingID: meetingID,
				Data:      data,
				Format:    "pcm_s16le_16k_mono",
				Timestamp: time.Now().UTC(),
			}); err != nil {
				log.Printf("send audio to stt failed meeting=%s packets=%d: %v", meetingID, packetCount, err)
				_ = conn.WriteJSON(map[string]any{
					"type":  "error",
					"error": err.Error(),
				})
			}
			s.maybeRestartStuckSTT(r.Context(), meetingID, peak)

		case websocket.TextMessage:
			var msg map[string]any
			if err := json.Unmarshal(data, &msg); err != nil {
				continue
			}
			if msg["type"] == "end" {
				log.Printf("audio websocket end signal meeting=%s packets=%d", meetingID, packetCount)
				return
			}
		}
	}
}
