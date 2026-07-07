// 本文件是本地 ASR 服务的可选集成测试。
// 默认跳过；设置 LOCAL_ASR_INTEGRATION_URL 后，会真实调用 local-asr-service。
package stt

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestLocalAdapterIntegrationWithLocalASRService(t *testing.T) {
	baseURL := os.Getenv("LOCAL_ASR_INTEGRATION_URL")
	if baseURL == "" {
		t.Skip("set LOCAL_ASR_INTEGRATION_URL to run local ASR integration test")
	}

	adapter, err := NewLocalAdapter(baseURL)
	if err != nil {
		t.Fatalf("NewLocalAdapter() error = %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	const meetingID = "m_integration"
	if err := adapter.StartSession(ctx, meetingID); err != nil {
		t.Fatalf("StartSession() error = %v", err)
	}
	if err := adapter.SendAudio(ctx, AudioChunk{MeetingID: meetingID, Data: loudPCMForTest(16000)}); err != nil {
		t.Fatalf("SendAudio() error = %v", err)
	}
	if err := adapter.StopSession(ctx, meetingID); err != nil {
		t.Fatalf("StopSession() error = %v", err)
	}

	select {
	case result := <-adapter.Results():
		if result.MeetingID != meetingID {
			t.Fatalf("MeetingID = %q", result.MeetingID)
		}
		if result.Text == "" {
			t.Fatalf("Text is empty")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for transcript result")
	}
}

func loudPCMForTest(sampleCount int) []byte {
	data := make([]byte, sampleCount*2)
	for i := 0; i < len(data); i += 2 {
		data[i] = 0xe8
		data[i+1] = 0x03
	}
	return data
}
