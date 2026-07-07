// 本文件负责接入独立部署的本地 ASR 服务。
// Go 会议核心服务只负责把浏览器音频转发给本地服务，并把本地服务返回的
// meeting-asr-v1 事件归一化为 TranscriptResult；SenseVoice、CAM++、RKNN 等
// 端侧推理细节都留在 local-asr-service 内部。
package stt

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

const defaultLocalASRURL = "http://127.0.0.1:9001"

type LocalAdapter struct {
	baseURL string
	client  *http.Client

	mu       sync.Mutex
	sessions map[string]localSession
	results  chan TranscriptResult
	errors   chan AdapterError
}

type localSession struct {
	meetingID string
	startedAt time.Time
}

type localASRResponse struct {
	OK     bool                   `json:"ok"`
	Error  string                 `json:"error"`
	Events []LocalTranscriptEvent `json:"events"`
}

func NewLocalAdapterFromEnv() (*LocalAdapter, error) {
	return NewLocalAdapter(envOrDefault("LOCAL_ASR_URL", defaultLocalASRURL))
}

func NewLocalAdapter(baseURL string) (*LocalAdapter, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		baseURL = defaultLocalASRURL
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, fmt.Errorf("invalid LOCAL_ASR_URL %q", baseURL)
	}
	return &LocalAdapter{
		baseURL:  baseURL,
		client:   &http.Client{Timeout: 8 * time.Second},
		sessions: map[string]localSession{},
		results:  make(chan TranscriptResult, 128),
		errors:   make(chan AdapterError, 32),
	}, nil
}

func (a *LocalAdapter) Provider() string {
	return ProviderLocal
}

func (a *LocalAdapter) StartSession(ctx context.Context, meetingID string) error {
	if strings.TrimSpace(meetingID) == "" {
		return fmt.Errorf("meetingID is required")
	}
	_ = a.StopSession(ctx, meetingID)

	if _, err := a.post(ctx, meetingID, "start", nil); err != nil {
		return err
	}

	a.mu.Lock()
	a.sessions[meetingID] = localSession{
		meetingID: meetingID,
		startedAt: time.Now().UTC(),
	}
	a.mu.Unlock()
	return nil
}

func (a *LocalAdapter) SendAudio(ctx context.Context, chunk AudioChunk) error {
	session, ok := a.getSession(chunk.MeetingID)
	if !ok {
		return fmt.Errorf("local stt session is not started: %s", chunk.MeetingID)
	}
	resp, err := a.post(ctx, chunk.MeetingID, "audio", chunk.Data)
	if err != nil {
		return err
	}
	a.emitEvents(session, resp.Events)
	return nil
}

func (a *LocalAdapter) StopSession(ctx context.Context, meetingID string) error {
	session, ok := a.removeSession(meetingID)
	if !ok {
		return nil
	}
	resp, err := a.post(ctx, meetingID, "stop", nil)
	if err != nil {
		return err
	}
	a.emitEvents(session, resp.Events)
	return nil
}

func (a *LocalAdapter) Results() <-chan TranscriptResult {
	return a.results
}

func (a *LocalAdapter) Errors() <-chan AdapterError {
	return a.errors
}

func (a *LocalAdapter) Close() error {
	a.mu.Lock()
	meetings := make([]string, 0, len(a.sessions))
	for meetingID := range a.sessions {
		meetings = append(meetings, meetingID)
	}
	a.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	for _, meetingID := range meetings {
		if err := a.StopSession(ctx, meetingID); err != nil {
			a.emitError(meetingID, err.Error())
		}
	}
	return nil
}

func (a *LocalAdapter) getSession(meetingID string) (localSession, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	session, ok := a.sessions[meetingID]
	return session, ok
}

func (a *LocalAdapter) removeSession(meetingID string) (localSession, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	session, ok := a.sessions[meetingID]
	if ok {
		delete(a.sessions, meetingID)
	}
	return session, ok
}

func (a *LocalAdapter) post(ctx context.Context, meetingID, action string, body []byte) (*localASRResponse, error) {
	endpoint := fmt.Sprintf("%s/v1/sessions/%s/%s", a.baseURL, url.PathEscape(meetingID), action)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/octet-stream")
	}

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("call local asr %s: %w", action, err)
	}
	defer resp.Body.Close()

	payload, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, fmt.Errorf("read local asr %s response: %w", action, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("local asr %s failed status=%s body=%s", action, resp.Status, string(payload))
	}

	var parsed localASRResponse
	if len(bytes.TrimSpace(payload)) > 0 {
		if err := json.Unmarshal(payload, &parsed); err != nil {
			return nil, fmt.Errorf("decode local asr %s response: %w body=%s", action, err, string(payload))
		}
	}
	if !parsed.OK {
		if parsed.Error == "" {
			parsed.Error = "local asr returned ok=false"
		}
		return nil, errors.New(parsed.Error)
	}
	return &parsed, nil
}

func (a *LocalAdapter) emitEvents(session localSession, events []LocalTranscriptEvent) {
	for _, event := range events {
		text := strings.TrimSpace(event.Text)
		if text == "" {
			continue
		}
		meetingID := strings.TrimSpace(event.MeetingID)
		if meetingID == "" {
			meetingID = session.meetingID
		}
		speaker := strings.TrimSpace(event.SpeakerLabel)
		if speaker == "" {
			speaker = DefaultSpeakerLabel
		}
		provider := strings.TrimSpace(event.Provider)
		if provider == "" {
			provider = ProviderSenseVoice
		}
		startedAt := session.startedAt.Add(time.Duration(event.StartedAtMs) * time.Millisecond)
		endedAt := session.startedAt.Add(time.Duration(event.EndedAtMs) * time.Millisecond)
		if endedAt.Before(startedAt) {
			endedAt = startedAt
		}

		result := TranscriptResult{
			MeetingID:    meetingID,
			SpeakerLabel: speaker,
			Text:         text,
			IsFinal:      event.IsFinal,
			Confidence:   event.Confidence,
			Provider:     provider,
			StartedAt:    startedAt,
			EndedAt:      endedAt,
		}
		select {
		case a.results <- result:
		default:
			a.emitError(meetingID, "result channel is full")
		}
	}
}

func (a *LocalAdapter) emitError(meetingID, message string) {
	err := AdapterError{
		MeetingID: meetingID,
		Provider:  a.Provider(),
		Message:   message,
	}
	select {
	case a.errors <- err:
	default:
	}
}

func envOrDefault(key, fallback string) string {
	if value := os.Getenv(key); strings.TrimSpace(value) != "" {
		return value
	}
	return fallback
}
