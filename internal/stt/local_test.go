// 本文件测试本地 ASR adapter 与 local-asr-service HTTP 契约的对齐情况。
package stt

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestLocalAdapterSendsAudioAndEmitsTranscript(t *testing.T) {
	var receivedAudio []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/sessions/m_test/start":
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		case "/v1/sessions/m_test/audio":
			receivedAudio, _ = io.ReadAll(r.Body)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok": true,
				"events": []LocalTranscriptEvent{
					{
						Type:            "transcript",
						ContractVersion: ContractVersion,
						MeetingID:       "m_test",
						SpeakerLabel:    "speaker_2",
						Text:            "本地ASR返回文本",
						IsFinal:         true,
						Confidence:      0.91,
						Provider:        ProviderSenseVoice,
						StartedAtMs:     100,
						EndedAtMs:       1200,
					},
				},
			})
		case "/v1/sessions/m_test/stop":
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "events": []LocalTranscriptEvent{}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	adapter, err := NewLocalAdapter(server.URL)
	if err != nil {
		t.Fatalf("NewLocalAdapter() error = %v", err)
	}
	ctx := context.Background()
	if err := adapter.StartSession(ctx, "m_test"); err != nil {
		t.Fatalf("StartSession() error = %v", err)
	}
	pcm := []byte{1, 2, 3, 4}
	if err := adapter.SendAudio(ctx, AudioChunk{MeetingID: "m_test", Data: pcm}); err != nil {
		t.Fatalf("SendAudio() error = %v", err)
	}

	select {
	case result := <-adapter.Results():
		if result.MeetingID != "m_test" {
			t.Fatalf("MeetingID = %q", result.MeetingID)
		}
		if result.SpeakerLabel != "speaker_2" {
			t.Fatalf("SpeakerLabel = %q", result.SpeakerLabel)
		}
		if result.Text != "本地ASR返回文本" {
			t.Fatalf("Text = %q", result.Text)
		}
		if !result.IsFinal {
			t.Fatalf("IsFinal = false")
		}
		if result.Provider != ProviderSenseVoice {
			t.Fatalf("Provider = %q", result.Provider)
		}
		if result.EndedAt.Before(result.StartedAt) {
			t.Fatalf("EndedAt before StartedAt")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for transcript result")
	}
	if string(receivedAudio) != string(pcm) {
		t.Fatalf("received audio = %v, want %v", receivedAudio, pcm)
	}
	if err := adapter.StopSession(ctx, "m_test"); err != nil {
		t.Fatalf("StopSession() error = %v", err)
	}
}

func TestNewAdapterSupportsLocalProvider(t *testing.T) {
	t.Setenv("LOCAL_ASR_URL", "http://127.0.0.1:9001")
	adapter, err := NewAdapter("sensevoice-small")
	if err != nil {
		t.Fatalf("NewAdapter() error = %v", err)
	}
	if adapter.Provider() != ProviderLocal {
		t.Fatalf("Provider() = %q", adapter.Provider())
	}
}
