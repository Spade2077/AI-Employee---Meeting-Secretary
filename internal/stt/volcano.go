// 本文件负责火山 ASR WebSocket 协议接入和转写结果解析。
package stt

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	volcanoASRURL = "wss://openspeech.bytedance.com/api/v3/sauc/bigmodel_async"

	volcanoResourceID = "volc.seedasr.sauc.duration"

	volcanoProtocolVersion = 0x1
	volcanoHeaderSize      = 0x1

	volcanoMsgTypeFullClient  = 0x1
	volcanoMsgTypeAudioOnly   = 0x2
	volcanoMsgTypeAudioResult = 0x3
	volcanoMsgTypeFullServer  = 0x9
	volcanoMsgTypeError       = 0xf

	volcanoSerialMethodNone = 0x0
	volcanoSerialMethodJSON = 0x1

	volcanoCompressionNone = 0x0

	volcanoFlagNoSequence = 0x0
	volcanoFlagLastPacket = 0x2
)

type VolcanoAdapter struct {
	appID       string
	accessToken string
	secretKey   string

	mu       sync.Mutex
	sessions map[string]*volcanoSession
	results  chan TranscriptResult
	errors   chan AdapterError
}

type volcanoSession struct {
	meetingID  string
	connectID  string
	resourceID string
	conn       *websocket.Conn
	writeMu    sync.Mutex
	done       chan struct{}
}

func NewVolcanoAdapterFromEnv() (*VolcanoAdapter, error) {
	appID := os.Getenv("VOLCANO_APP_ID")
	accessToken := os.Getenv("VOLCANO_ACCESS_TOKEN")
	secretKey := os.Getenv("VOLCANO_SECRET_KEY")
	if appID == "" || accessToken == "" || secretKey == "" {
		return nil, fmt.Errorf("missing VOLCANO_APP_ID, VOLCANO_ACCESS_TOKEN, or VOLCANO_SECRET_KEY")
	}
	return NewVolcanoAdapter(appID, accessToken, secretKey), nil
}

func NewVolcanoAdapter(appID, accessToken, secretKey string) *VolcanoAdapter {
	return &VolcanoAdapter{
		appID:       appID,
		accessToken: accessToken,
		secretKey:   secretKey,
		sessions:    map[string]*volcanoSession{},
		results:     make(chan TranscriptResult, 128),
		errors:      make(chan AdapterError, 32),
	}
}

func (a *VolcanoAdapter) Provider() string {
	return "volcano"
}

func (a *VolcanoAdapter) StartSession(ctx context.Context, meetingID string) error {
	if err := a.StopSession(ctx, meetingID); err != nil {
		return err
	}

	session, err := a.dial(ctx, meetingID)
	if err != nil {
		return err
	}
	if err := a.sendFullClientRequest(session); err != nil {
		_ = session.conn.Close()
		return err
	}

	a.mu.Lock()
	a.sessions[meetingID] = session
	a.mu.Unlock()

	go a.readLoop(session)
	return nil
}

func (a *VolcanoAdapter) SendAudio(ctx context.Context, chunk AudioChunk) error {
	session, ok := a.getSession(chunk.MeetingID)
	if !ok {
		return fmt.Errorf("volcano stt session is not started: %s", chunk.MeetingID)
	}
	return a.sendAudioPacket(ctx, session, chunk.Data, false)
}

func (a *VolcanoAdapter) StopSession(ctx context.Context, meetingID string) error {
	a.mu.Lock()
	session := a.sessions[meetingID]
	delete(a.sessions, meetingID)
	a.mu.Unlock()
	if session == nil {
		return nil
	}

	if err := a.sendAudioPacket(ctx, session, nil, true); err != nil {
		_ = session.conn.Close()
		return err
	}

	select {
	case <-session.done:
		return nil
	case <-ctx.Done():
		_ = session.conn.Close()
		return ctx.Err()
	case <-time.After(2 * time.Second):
		_ = session.conn.Close()
		return nil
	}
}

func (a *VolcanoAdapter) Results() <-chan TranscriptResult {
	return a.results
}

func (a *VolcanoAdapter) Errors() <-chan AdapterError {
	return a.errors
}

func (a *VolcanoAdapter) Close() error {
	a.mu.Lock()
	sessions := make([]*volcanoSession, 0, len(a.sessions))
	for _, session := range a.sessions {
		sessions = append(sessions, session)
	}
	a.sessions = map[string]*volcanoSession{}
	a.mu.Unlock()

	for _, session := range sessions {
		_ = a.sendAudioPacket(context.Background(), session, nil, true)
		select {
		case <-session.done:
		case <-time.After(500 * time.Millisecond):
			_ = session.conn.Close()
		}
	}
	return nil
}

func (a *VolcanoAdapter) getSession(meetingID string) (*volcanoSession, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	session, ok := a.sessions[meetingID]
	return session, ok
}

func (a *VolcanoAdapter) dial(ctx context.Context, meetingID string) (*volcanoSession, error) {
	resourceIDs := []string{
		volcanoResourceID,
		"volc.bigasr.sauc.duration",
		"volc.bigasr.sauc.concurrent",
		"volcengine_streaming_common",
	}

	var lastErr error
	for _, resourceID := range resourceIDs {
		connectID := volcanoUUID()
		header := http.Header{}
		header.Set("Authorization", a.authHeader(resourceID, connectID))
		header.Set("X-Api-App-Key", a.appID)
		header.Set("X-Api-Access-Key", a.accessToken)
		header.Set("X-Api-Resource-Id", resourceID)
		header.Set("X-Api-Connect-Id", connectID)

		u, _ := url.Parse(volcanoASRURL)
		conn, resp, err := websocket.DefaultDialer.DialContext(ctx, u.String(), header)
		if err == nil {
			log.Printf("volcano stt connected meeting=%s resource_id=%s", meetingID, resourceID)
			return &volcanoSession{
				meetingID:  meetingID,
				connectID:  connectID,
				resourceID: resourceID,
				conn:       conn,
				done:       make(chan struct{}),
			}, nil
		}
		if resp != nil {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
			log.Printf("volcano stt resource failed meeting=%s resource_id=%s status=%s body=%s", meetingID, resourceID, resp.Status, string(body))
			lastErr = fmt.Errorf("resource_id=%s status=%s body=%s", resourceID, resp.Status, string(body))
		} else {
			log.Printf("volcano stt dial failed meeting=%s resource_id=%s: %v", meetingID, resourceID, err)
			lastErr = err
		}
	}
	return nil, fmt.Errorf("all volcano resource IDs failed, last: %w", lastErr)
}

func (a *VolcanoAdapter) sendFullClientRequest(session *volcanoSession) error {
	req := map[string]any{
		"app": map[string]any{
			"appid": a.appID,
		},
		"user": map[string]any{
			"uid":         "desktop-ai-secretary",
			"did":         "device-001",
			"platform":    "Linux",
			"sdk_version": "1.0.0",
		},
		"audio": map[string]any{
			"format":   "pcm",
			"rate":     16000,
			"bits":     16,
			"channel":  1,
			"language": "zh-CN",
		},
		"request": map[string]any{
			"model_name":       "bigmodel",
			"enable_itn":       true,
			"enable_punc":      true,
			"enable_ddc":       false,
			"show_utterances":  true,
			"result_type":      "single",
			"show_speech_rate": true,
		},
	}

	payload, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal full request: %w", err)
	}
	header := volcanoEncodeHeader(volcanoMsgTypeFullClient, volcanoFlagNoSequence, volcanoSerialMethodJSON, volcanoCompressionNone)
	payloadLen := make([]byte, 4)
	binary.BigEndian.PutUint32(payloadLen, uint32(len(payload)))
	msg := append(header, payloadLen...)
	msg = append(msg, payload...)

	session.writeMu.Lock()
	defer session.writeMu.Unlock()
	if err := session.conn.WriteMessage(websocket.BinaryMessage, msg); err != nil {
		return fmt.Errorf("send full client request: %w", err)
	}
	return nil
}

func (a *VolcanoAdapter) sendAudioPacket(ctx context.Context, session *volcanoSession, data []byte, isLast bool) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	flags := byte(volcanoFlagNoSequence)
	if isLast {
		flags = volcanoFlagLastPacket
	}
	header := volcanoEncodeHeader(volcanoMsgTypeAudioOnly, flags, volcanoSerialMethodNone, volcanoCompressionNone)
	payloadLen := make([]byte, 4)
	binary.BigEndian.PutUint32(payloadLen, uint32(len(data)))
	msg := append(header, payloadLen...)
	msg = append(msg, data...)

	session.writeMu.Lock()
	defer session.writeMu.Unlock()
	return session.conn.WriteMessage(websocket.BinaryMessage, msg)
}

func (a *VolcanoAdapter) readLoop(session *volcanoSession) {
	defer close(session.done)
	for {
		_, msg, err := session.conn.ReadMessage()
		if err != nil {
			if a.removeSession(session.meetingID, session) {
				a.emitError(session.meetingID, err.Error())
			}
			return
		}
		msgType := byte(0)
		if len(msg) > 0 && msg[0] == '{' {
			msgType = volcanoMsgTypeAudioResult
		} else if len(msg) >= 2 {
			msgType = (msg[1] >> 4) & 0xf
		} else {
			continue
		}

		text, utteranceText, isFinal, err := volcanoDecodeResponse(msgType, msg)
		if err != nil {
			a.emitError(session.meetingID, err.Error())
			continue
		}
		resultText := utteranceText
		if resultText == "" {
			resultText = text
		}
		if resultText == "" {
			continue
		}

		now := time.Now().UTC()
		result := TranscriptResult{
			MeetingID:    session.meetingID,
			SpeakerLabel: "unknown",
			Text:         resultText,
			IsFinal:      isFinal,
			Confidence:   0,
			Provider:     a.Provider(),
			StartedAt:    now,
			EndedAt:      now,
		}
		select {
		case a.results <- result:
		default:
			a.emitError(session.meetingID, "result channel is full")
		}
	}
}

func (a *VolcanoAdapter) removeSession(meetingID string, session *volcanoSession) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if current := a.sessions[meetingID]; current == session {
		delete(a.sessions, meetingID)
		return true
	}
	return false
}

func (a *VolcanoAdapter) emitError(meetingID, message string) {
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

func (a *VolcanoAdapter) authHeader(resourceID, connectID string) string {
	authStr := fmt.Sprintf("GET /api/v3/sauc/bigmodel_async HTTP/1.1\r\n"+
		"Host: openspeech.bytedance.com\r\n"+
		"X-Api-App-Key: %s\r\n"+
		"X-Api-Access-Key: %s\r\n"+
		"X-Api-Resource-Id: %s\r\n"+
		"X-Api-Connect-Id: %s\r\n",
		a.appID, a.accessToken, resourceID, connectID)

	h := hmac.New(sha256.New, []byte(a.secretKey))
	h.Write([]byte(authStr))
	mac := base64.URLEncoding.WithPadding(base64.NoPadding).EncodeToString(h.Sum(nil))

	return fmt.Sprintf(`HMAC256; access_token="%s"; mac="%s"; h="X-Api-App-Key,X-Api-Access-Key,X-Api-Resource-Id,X-Api-Connect-Id"`,
		a.accessToken, mac)
}

func volcanoDecodeResponse(msgType byte, data []byte) (text, utteranceText string, isFinal bool, err error) {
	if len(data) == 0 {
		return
	}
	if msgType == volcanoMsgTypeAudioOnly {
		return
	}
	if msgType == volcanoMsgTypeAudioResult {
		var resp map[string]any
		if err = json.Unmarshal(data, &resp); err != nil {
			err = fmt.Errorf("unmarshal audio result: %w", err)
			return
		}
		text, utteranceText = volcanoExtractText(resp)
		isFinal = true
		return
	}
	if len(data) < 12 {
		err = fmt.Errorf("response too short: %d bytes", len(data))
		return
	}

	serialMethod := data[2] >> 4
	if msgType == volcanoMsgTypeError {
		if serialMethod == volcanoSerialMethodJSON && len(data) > 12 {
			err = fmt.Errorf("volcano error: %s", string(data[12:]))
		}
		return
	}
	if msgType != volcanoMsgTypeFullServer {
		return
	}

	isFinal = true
	offset := 8
	payloadSize := binary.BigEndian.Uint32(data[offset : offset+4])
	offset += 4
	if int(payloadSize)+offset > len(data) {
		return
	}
	payload := data[offset : offset+int(payloadSize)]
	var resp map[string]any
	if err = json.Unmarshal(payload, &resp); err != nil {
		err = fmt.Errorf("unmarshal response: %w", err)
		return
	}
	text, utteranceText = volcanoExtractText(resp)
	return
}

func volcanoExtractText(resp map[string]any) (text, utteranceText string) {
	result, ok := resp["result"].(map[string]any)
	if !ok {
		return "", ""
	}
	if textVal, ok := result["text"].(string); ok {
		text = textVal
	}
	if utterances, ok := result["utterances"].([]any); ok {
		for _, item := range utterances {
			utterance, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if t, ok := utterance["text"].(string); ok {
				utteranceText = t
			}
		}
	}
	return text, utteranceText
}

func volcanoEncodeHeader(msgType, flags, serialMethod, compression byte) []byte {
	h := byte((volcanoProtocolVersion << 4) | volcanoHeaderSize)
	b1 := (msgType << 4) | flags
	b2 := (serialMethod << 4) | compression
	return []byte{h, b1, b2, 0}
}

func volcanoUUID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = (b[6] & 0x4f) | 0x40
	b[7] = (b[7] & 0xbf) | 0x80
	return fmt.Sprintf("%s-%s-%s-%s-%s",
		hex.EncodeToString(b[0:4]),
		hex.EncodeToString(b[4:6]),
		hex.EncodeToString(b[6:8]),
		hex.EncodeToString(b[8:12]),
		hex.EncodeToString(b[12:16]))
}
