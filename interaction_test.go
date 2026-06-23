package main

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"meeting-core-service/internal/stt"

	_ "modernc.org/sqlite"
)

type fakeLLMClient struct {
	content string
}

func (f fakeLLMClient) Complete(ctx context.Context, cfg *LLMConfig, messages []LLMMessage, temperature float64) (string, error) {
	return f.content, nil
}

func TestExtractWakeInstruction(t *testing.T) {
	wake, instruction, ok := extractWakeInstruction("小艾小艾，帮我把待办事项的第三条删掉")
	if !ok {
		t.Fatal("expected wake word to be detected")
	}
	if wake != "小艾小艾" {
		t.Fatalf("unexpected wake word: %q", wake)
	}
	if instruction != "帮我把待办事项的第三条删掉" {
		t.Fatalf("unexpected instruction: %q", instruction)
	}
}

func TestExtractWakeInstructionWithPunctuationBetweenWakeWords(t *testing.T) {
	wake, instruction, ok := extractWakeInstruction("小爱，小爱，帮我把待办事项的第二条删掉")
	if !ok {
		t.Fatal("expected wake word with punctuation to be detected")
	}
	if wake != "小爱小爱" {
		t.Fatalf("unexpected wake word: %q", wake)
	}
	if instruction != "帮我把待办事项的第二条删掉" {
		t.Fatalf("unexpected instruction: %q", instruction)
	}
}

func TestMeetingInteractionDeletesActionItem(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	store := &Store{db: db}
	if err := store.Init(ctx); err != nil {
		t.Fatal(err)
	}
	meeting, _, err := store.CreateMeeting(ctx, "interaction", "test", "test")
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.SaveIntelligenceExtraction(ctx, meeting.ID, IntelligenceExtraction{
		ActionItems: []ActionItemExtraction{
			{Task: "准备会议材料", Owner: "张三", Status: "open"},
			{Task: "确认会议室", Owner: "李四", Status: "open"},
			{Task: "排查 ASR 连接保持逻辑", Owner: "王磊", Status: "open"},
		},
	}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SaveLLMConfigFor(ctx, LLMUseCaseInteraction, LLMConfig{BaseURL: "https://llm.example/v1", Model: "test-model"}); err != nil {
		t.Fatal(err)
	}

	server := &Server{
		store: store,
		hub:   NewHub(),
		llmClient: fakeLLMClient{content: `{
			"intent": "delete_action_item",
			"target_type": "action_item",
			"target_index": 3,
			"target_id": "",
			"confidence": 0.98,
			"need_confirm": false,
			"reply": "已删除第 3 条待办。"
		}`},
	}
	server.handleMeetingInteraction(ctx, stt.TranscriptResult{
		MeetingID: meeting.ID,
		Text:      "小艾小艾，帮我把待办事项的第三条删掉",
		IsFinal:   true,
		Provider:  "test",
	}, "小艾小艾", "帮我把待办事项的第三条删掉")

	items, err := store.ListActionItems(ctx, meeting.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 action items after delete, got %d: %#v", len(items), items)
	}
	for _, item := range items {
		if strings.Contains(item.Task, "ASR") {
			t.Fatalf("expected ASR item to be deleted, got %#v", items)
		}
	}
	events, err := store.ListInteractionEvents(ctx, meeting.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 interaction event, got %d", len(events))
	}
	if events[0].ExecutionStatus != "success" {
		t.Fatalf("expected success event, got %#v", events[0])
	}
}

func TestMeetingInteractionConsumesSplitWakeAndInstruction(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	store := &Store{db: db}
	if err := store.Init(ctx); err != nil {
		t.Fatal(err)
	}
	meeting, _, err := store.CreateMeeting(ctx, "split-interaction", "test", "test")
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.SaveIntelligenceExtraction(ctx, meeting.ID, IntelligenceExtraction{
		ActionItems: []ActionItemExtraction{
			{Task: "准备会议材料", Owner: "张三", Status: "open"},
			{Task: "确认会议室", Owner: "李四", Status: "open"},
		},
	}, 1)
	if err != nil {
		t.Fatal(err)
	}

	server := &Server{
		store: store,
		hub:   NewHub(),
		llmClient: fakeLLMClient{content: `{
			"intent": "delete_action_item",
			"target_type": "action_item",
			"target_index": 2,
			"target_id": "",
			"confidence": 0.98,
			"need_confirm": false,
			"reply": "已删除第 2 条待办。"
		}`},
		pendingInteractions: map[string]*PendingInteraction{},
	}
	if !server.consumeInteractionTranscript(stt.TranscriptResult{MeetingID: meeting.ID, Text: "小", IsFinal: true, Provider: "test"}) {
		t.Fatal("expected wake prefix to be consumed")
	}
	if !server.consumeInteractionTranscript(stt.TranscriptResult{MeetingID: meeting.ID, Text: "小爱，小爱", IsFinal: true, Provider: "test"}) {
		t.Fatal("expected wake phrase to be consumed")
	}
	if !server.consumeInteractionTranscript(stt.TranscriptResult{MeetingID: meeting.ID, Text: "帮我把待办事项的第二", IsFinal: true, Provider: "test"}) {
		t.Fatal("expected partial instruction to be consumed")
	}
	if !server.consumeInteractionTranscript(stt.TranscriptResult{MeetingID: meeting.ID, Text: "帮我把待办事项的第2条删掉", IsFinal: true, Provider: "test"}) {
		t.Fatal("expected final instruction to be consumed")
	}

	waitFor(t, time.Second, func() bool {
		events, err := store.ListInteractionEvents(ctx, meeting.ID)
		return err == nil && len(events) == 1
	})
	items, err := store.ListActionItems(ctx, meeting.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 action item after split command, got %d: %#v", len(items), items)
	}
	if strings.Contains(items[0].Task, "会议室") {
		t.Fatalf("expected second action item to be deleted, got %#v", items)
	}
}

func waitFor(t *testing.T, timeout time.Duration, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("condition was not met before timeout")
}
