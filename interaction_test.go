// 本文件负责验证会中交互命令执行、合并和撤销行为。
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
	store, meetingID := newInteractionStore(t, ctx)
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
		MeetingID: meetingID,
		Text:      "小艾小艾，帮我把待办事项的第三条删掉",
		IsFinal:   true,
		Provider:  "test",
	}, "小艾小艾", "帮我把待办事项的第三条删掉")

	items, err := store.ListActionItems(ctx, meetingID)
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
	events, err := store.ListInteractionEvents(ctx, meetingID)
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
	store, meetingID := newInteractionStore(t, ctx)
	if _, err := store.SaveLLMConfigFor(ctx, LLMUseCaseInteraction, LLMConfig{BaseURL: "https://llm.example/v1", Model: "test-model"}); err != nil {
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
	if !server.consumeInteractionTranscript(stt.TranscriptResult{MeetingID: meetingID, Text: "小", IsFinal: true, Provider: "test"}) {
		t.Fatal("expected wake prefix to be consumed")
	}
	if !server.consumeInteractionTranscript(stt.TranscriptResult{MeetingID: meetingID, Text: "小爱，小爱", IsFinal: true, Provider: "test"}) {
		t.Fatal("expected wake phrase to be consumed")
	}
	if !server.consumeInteractionTranscript(stt.TranscriptResult{MeetingID: meetingID, Text: "帮我把待办事项的第二", IsFinal: true, Provider: "test"}) {
		t.Fatal("expected partial instruction to be consumed")
	}
	if !server.consumeInteractionTranscript(stt.TranscriptResult{MeetingID: meetingID, Text: "帮我把待办事项的第二条删掉", IsFinal: true, Provider: "test"}) {
		t.Fatal("expected final instruction to be consumed")
	}

	waitFor(t, time.Second, func() bool {
		events, err := store.ListInteractionEvents(ctx, meetingID)
		return err == nil && len(events) == 1
	})
	items, err := store.ListActionItems(ctx, meetingID)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 action items after split command, got %d: %#v", len(items), items)
	}
	for _, item := range items {
		if item.Task == "Book meeting room" {
			t.Fatalf("expected second action item to be deleted, got %#v", items)
		}
	}
}

func TestInteractionCommandExecutionCoversSupportedIntents(t *testing.T) {
	ctx := context.Background()
	store, meetingID := newInteractionStore(t, ctx)
	server := &Server{store: store, hub: NewHub()}

	_, _, err := server.executeInteractionCommand(ctx, meetingID, InteractionCommand{
		Intent:      "update_action_item",
		TargetType:  "action_item",
		TargetIndex: 1,
		Owner:       "Alice",
		DueDate:     "Friday",
	})
	if err != nil {
		t.Fatal(err)
	}
	items, err := store.ListActionItems(ctx, meetingID)
	if err != nil {
		t.Fatal(err)
	}
	if items[0].Owner != "Alice" || items[0].DueDate != "Friday" {
		t.Fatalf("expected first action item to be updated, got %#v", items[0])
	}

	_, _, err = server.executeInteractionCommand(ctx, meetingID, InteractionCommand{
		Intent:     "create_action_item",
		TargetType: "action_item",
		Task:       "Prepare project rollout checklist",
		Owner:      "Bob",
		DueDate:    "next Monday",
	})
	if err != nil {
		t.Fatal(err)
	}
	items, err = store.ListActionItems(ctx, meetingID)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 4 {
		t.Fatalf("expected new action item, got %#v", items)
	}

	_, _, err = server.executeInteractionCommand(ctx, meetingID, InteractionCommand{
		Intent:      "complete_action_item",
		TargetType:  "action_item",
		TargetIndex: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	items, err = store.ListActionItems(ctx, meetingID)
	if err != nil {
		t.Fatal(err)
	}
	if items[1].Status != "completed" {
		t.Fatalf("expected second item completed, got %#v", items[1])
	}

	_, _, err = server.executeInteractionCommand(ctx, meetingID, InteractionCommand{
		Intent:      "move_action_item_to_decision",
		TargetType:  "action_item",
		TargetIndex: 3,
		Content:     "Keep Volcano ASR connection alive during pause.",
	})
	if err != nil {
		t.Fatal(err)
	}
	decisions, err := store.ListDecisions(ctx, meetingID)
	if err != nil {
		t.Fatal(err)
	}
	if len(decisions) != 1 || !strings.Contains(decisions[0].Content, "Volcano ASR") {
		t.Fatalf("expected moved decision, got %#v", decisions)
	}

	mergeOne, err := store.CreateActionItem(ctx, meetingID, ActionItemMutation{Task: "Draft launch email", Owner: "Carol"})
	if err != nil {
		t.Fatal(err)
	}
	mergeTwo, err := store.CreateActionItem(ctx, meetingID, ActionItemMutation{Task: "Review launch email", DueDate: "Wednesday"})
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = server.executeInteractionCommand(ctx, meetingID, InteractionCommand{
		Intent:    "merge_action_items",
		SourceIDs: []string{mergeOne.ID, mergeTwo.ID},
	})
	if err != nil {
		t.Fatal(err)
	}
	items, err = store.ListActionItems(ctx, meetingID)
	if err != nil {
		t.Fatal(err)
	}
	foundMerged := false
	for _, item := range items {
		if strings.Contains(item.Task, "Draft launch email") && strings.Contains(item.Task, "Review launch email") {
			foundMerged = true
			break
		}
	}
	if !foundMerged {
		t.Fatalf("expected merged action item, got %#v", items)
	}

	_, _, err = server.executeInteractionCommand(ctx, meetingID, InteractionCommand{
		Intent:     "rewrite_summary_section",
		TargetType: "summary",
		Content:    "The meeting focused on ASR stability, rollout preparation, and launch communications.",
	})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := store.GetIntelligenceSnapshot(ctx, meetingID)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Summary == nil || !strings.Contains(snapshot.Summary.Content, "ASR stability") {
		t.Fatalf("expected rewritten summary, got %#v", snapshot.Summary)
	}

	if _, err := store.RewriteSummary(ctx, meetingID, "会议摘要：本次会议同步培训试运行安排。风险提示：报名人数和网络稳定性需要关注。缺失项提醒：参训名单仍待确认。"); err != nil {
		t.Fatal(err)
	}
	_, _, err = server.executeInteractionCommand(ctx, meetingID, InteractionCommand{
		Intent:      "rewrite_summary_section",
		TargetType:  "summary",
		Content:     "重点突出人员时间冲突和场地设备检查",
		Reason:      "风险",
		Instruction: "会议摘要里关于风险那段重新写一下，重点突出人员时间冲突和场地设备检查。",
	})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err = store.GetIntelligenceSnapshot(ctx, meetingID)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Summary == nil ||
		!strings.Contains(snapshot.Summary.Content, "会议摘要") ||
		!strings.Contains(snapshot.Summary.Content, "风险提示") ||
		!strings.Contains(snapshot.Summary.Content, "人员时间冲突") ||
		!strings.Contains(snapshot.Summary.Content, "缺失项提醒") {
		t.Fatalf("expected section rewrite to preserve summary context, got %#v", snapshot.Summary)
	}
}

func TestInteractionDecisionCommands(t *testing.T) {
	ctx := context.Background()
	store, meetingID := newInteractionStore(t, ctx)
	server := &Server{store: store, hub: NewHub()}

	_, _, err := server.executeInteractionCommand(ctx, meetingID, InteractionCommand{
		Intent:     "create_decision",
		TargetType: "decision",
		Content:    "Use cloud ASR for the first demo.",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = server.executeInteractionCommand(ctx, meetingID, InteractionCommand{
		Intent:      "update_decision",
		TargetType:  "decision",
		TargetIndex: 1,
		Content:     "Use cloud ASR for the first demo, then evaluate local ASR.",
	})
	if err != nil {
		t.Fatal(err)
	}
	decisions, err := store.ListDecisions(ctx, meetingID)
	if err != nil {
		t.Fatal(err)
	}
	if len(decisions) != 1 || !strings.Contains(decisions[0].Content, "local ASR") {
		t.Fatalf("expected updated decision, got %#v", decisions)
	}

	_, _, err = server.executeInteractionCommand(ctx, meetingID, InteractionCommand{
		Intent:     "create_decision",
		TargetType: "decision",
		Content:    "Keep the interaction parser separate from intelligence extraction.",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = server.executeInteractionCommand(ctx, meetingID, InteractionCommand{
		Intent:        "merge_decisions",
		TargetType:    "decision",
		SourceIndices: []int{1, 2},
	})
	if err != nil {
		t.Fatal(err)
	}
	decisions, err = store.ListDecisions(ctx, meetingID)
	if err != nil {
		t.Fatal(err)
	}
	if len(decisions) != 1 ||
		!strings.Contains(decisions[0].Content, "local ASR") ||
		!strings.Contains(decisions[0].Content, "interaction parser") {
		t.Fatalf("expected merged decision, got %#v", decisions)
	}

	_, _, err = server.executeInteractionCommand(ctx, meetingID, InteractionCommand{
		Intent:      "move_decision_to_action_item",
		TargetType:  "decision",
		TargetIndex: 1,
		Task:        "Evaluate local ASR after the cloud demo",
		Owner:       "Dana",
		DueDate:     "next Friday",
	})
	if err != nil {
		t.Fatal(err)
	}
	decisions, err = store.ListDecisions(ctx, meetingID)
	if err != nil {
		t.Fatal(err)
	}
	if len(decisions) != 0 {
		t.Fatalf("expected moved decision to be removed, got %#v", decisions)
	}
	items, err := store.ListActionItems(ctx, meetingID)
	if err != nil {
		t.Fatal(err)
	}
	foundMoved := false
	for _, item := range items {
		if item.Task == "Evaluate local ASR after the cloud demo" && item.Owner == "Dana" {
			foundMoved = true
			break
		}
	}
	if !foundMoved {
		t.Fatalf("expected moved decision action item, got %#v", items)
	}

	_, _, err = server.executeInteractionCommand(ctx, meetingID, InteractionCommand{
		Intent:     "create_decision",
		TargetType: "decision",
		Content:    "Use a fast model for interaction commands.",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = server.executeInteractionCommand(ctx, meetingID, InteractionCommand{
		Intent:      "delete_decision",
		TargetType:  "decision",
		TargetIndex: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	decisions, err = store.ListDecisions(ctx, meetingID)
	if err != nil {
		t.Fatal(err)
	}
	if len(decisions) != 0 {
		t.Fatalf("expected decision to be deleted, got %#v", decisions)
	}
}

func TestUndoDeleteActionItemInteraction(t *testing.T) {
	ctx := context.Background()
	store, meetingID := newInteractionStore(t, ctx)
	server := &Server{store: store, hub: NewHub()}

	_, _, err := server.executeInteractionCommand(ctx, meetingID, InteractionCommand{
		Intent:      "delete_action_item",
		TargetType:  "action_item",
		TargetIndex: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	items, err := store.ListActionItems(ctx, meetingID)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("expected one action item to be deleted, got %#v", items)
	}

	_, message, err := server.executeInteractionCommand(ctx, meetingID, InteractionCommand{
		Intent:     "undo_last_interaction",
		TargetType: "interaction",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(message, "Book meeting room") {
		t.Fatalf("expected undo message to mention restored item, got %q", message)
	}
	items, err = store.ListActionItems(ctx, meetingID)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 3 {
		t.Fatalf("expected deleted action item to be restored, got %#v", items)
	}
	found := false
	for _, item := range items {
		if item.Task == "Book meeting room" && !item.IsDeleted {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected restored Book meeting room item, got %#v", items)
	}
}

func TestUndoUpdateDecisionInteraction(t *testing.T) {
	ctx := context.Background()
	store, meetingID := newInteractionStore(t, ctx)
	server := &Server{store: store, hub: NewHub()}

	_, _, err := server.executeInteractionCommand(ctx, meetingID, InteractionCommand{
		Intent:     "create_decision",
		TargetType: "decision",
		Content:    "Use cloud ASR for the first demo.",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = server.executeInteractionCommand(ctx, meetingID, InteractionCommand{
		Intent:      "update_decision",
		TargetType:  "decision",
		TargetIndex: 1,
		Content:     "Use local ASR for the first demo.",
	})
	if err != nil {
		t.Fatal(err)
	}
	decisions, err := store.ListDecisions(ctx, meetingID)
	if err != nil {
		t.Fatal(err)
	}
	if len(decisions) != 1 || decisions[0].Content != "Use local ASR for the first demo." {
		t.Fatalf("expected updated decision, got %#v", decisions)
	}

	_, _, err = server.executeInteractionCommand(ctx, meetingID, InteractionCommand{
		Intent:     "undo_last_interaction",
		TargetType: "interaction",
	})
	if err != nil {
		t.Fatal(err)
	}
	decisions, err = store.ListDecisions(ctx, meetingID)
	if err != nil {
		t.Fatal(err)
	}
	if len(decisions) != 1 || decisions[0].Content != "Use cloud ASR for the first demo." {
		t.Fatalf("expected original decision to be restored, got %#v", decisions)
	}
}

func newInteractionStore(t *testing.T, ctx context.Context) (*Store, string) {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })

	store := &Store{db: db}
	if err := store.Init(ctx); err != nil {
		t.Fatal(err)
	}
	meeting, _, err := store.CreateMeeting(ctx, "interaction", "test", "test")
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.SaveIntelligenceExtraction(ctx, meeting.ID, IntelligenceExtraction{
		Summary: "Initial summary.",
		ActionItems: []ActionItemExtraction{
			{Task: "Prepare meeting materials", Owner: "Zhang San", Status: "open"},
			{Task: "Book meeting room", Owner: "Li Si", Status: "open"},
			{Task: "Investigate ASR connection keepalive", Owner: "Wang Wu", Status: "open"},
		},
	}, 1)
	if err != nil {
		t.Fatal(err)
	}
	return store, meeting.ID
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
