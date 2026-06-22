package stt

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

type MockAdapter struct {
	mu       sync.Mutex
	sessions map[string]struct{}
	counter  int
	results  chan TranscriptResult
	errors   chan AdapterError
}

func NewMockAdapter() *MockAdapter {
	return &MockAdapter{
		sessions: map[string]struct{}{},
		results:  make(chan TranscriptResult, 32),
		errors:   make(chan AdapterError, 8),
	}
}

func (a *MockAdapter) Provider() string {
	return "mock"
}

func (a *MockAdapter) StartSession(ctx context.Context, meetingID string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.sessions[meetingID] = struct{}{}
	return nil
}

func (a *MockAdapter) SendAudio(ctx context.Context, chunk AudioChunk) error {
	a.mu.Lock()
	if _, ok := a.sessions[chunk.MeetingID]; !ok {
		a.mu.Unlock()
		return fmt.Errorf("mock stt session is not started: %s", chunk.MeetingID)
	}

	a.counter++
	text := strings.TrimSpace(string(chunk.Data))
	if text == "" {
		text = fmt.Sprintf("mock transcript segment %d", a.counter)
	}
	a.mu.Unlock()

	now := time.Now().UTC()
	result := TranscriptResult{
		MeetingID:    chunk.MeetingID,
		SpeakerLabel: "speaker_1",
		Text:         text,
		IsFinal:      true,
		Confidence:   1,
		Provider:     a.Provider(),
		StartedAt:    now,
		EndedAt:      now,
	}

	select {
	case a.results <- result:
	case <-ctx.Done():
		return ctx.Err()
	}
	return nil
}

func (a *MockAdapter) StopSession(ctx context.Context, meetingID string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.sessions, meetingID)
	return nil
}

func (a *MockAdapter) Results() <-chan TranscriptResult {
	return a.results
}

func (a *MockAdapter) Errors() <-chan AdapterError {
	return a.errors
}

func (a *MockAdapter) Close() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.sessions = map[string]struct{}{}
	return nil
}
