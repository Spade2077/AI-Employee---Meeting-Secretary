package stt

import (
	"context"
	"fmt"
	"strings"
	"time"
)

type AudioChunk struct {
	MeetingID string
	Data      []byte
	Format    string
	Timestamp time.Time
}

type TranscriptResult struct {
	MeetingID    string    `json:"meetingId"`
	SpeakerLabel string    `json:"speakerLabel"`
	Text         string    `json:"text"`
	IsFinal      bool      `json:"isFinal"`
	Confidence   float64   `json:"confidence"`
	Provider     string    `json:"provider"`
	StartedAt    time.Time `json:"startedAt"`
	EndedAt      time.Time `json:"endedAt"`
}

type AdapterError struct {
	MeetingID string `json:"meetingId,omitempty"`
	Provider  string `json:"provider"`
	Message   string `json:"message"`
}

func (e AdapterError) Error() string {
	if e.MeetingID == "" {
		return e.Provider + ": " + e.Message
	}
	return e.Provider + "/" + e.MeetingID + ": " + e.Message
}

type Adapter interface {
	Provider() string
	StartSession(ctx context.Context, meetingID string) error
	SendAudio(ctx context.Context, chunk AudioChunk) error
	StopSession(ctx context.Context, meetingID string) error
	Results() <-chan TranscriptResult
	Errors() <-chan AdapterError
	Close() error
}

func NewAdapter(provider string) (Adapter, error) {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "", "mock":
		return NewMockAdapter(), nil
	case "volcano", "volcano_asr", "volcano-asr":
		return NewVolcanoAdapterFromEnv()
	default:
		return nil, fmt.Errorf("unsupported STT_PROVIDER %q", provider)
	}
}
