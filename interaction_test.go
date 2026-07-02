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

	waitFor(t, 5*time.Second, func() bool {
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

func TestInteractionEditModeConsumesCommandsUntilExit(t *testing.T) {
	server := &Server{
		hub:                 NewHub(),
		pendingInteractions: map[string]*PendingInteraction{},
		recentInteractions:  map[string][]recentInteractionTranscript{},
		interactionModes:    map[string]*InteractionModeState{},
	}
	meetingID := "m_edit_mode"

	if !server.consumeInteractionTranscript(stt.TranscriptResult{MeetingID: meetingID, Text: "小艾小艾，进入修改模式", IsFinal: true, Provider: "test"}) {
		t.Fatal("expected enter edit mode command to be consumed")
	}
	if !server.interactionEditModeActive(meetingID, time.Now()) {
		t.Fatal("expected edit mode to be active")
	}
	if !server.consumeInteractionTranscript(stt.TranscriptResult{MeetingID: meetingID, Text: "把第一条待确认问题改成客户是否允许外网接入", IsFinal: true, Provider: "test"}) {
		t.Fatal("expected edit-mode instruction to be consumed")
	}

	server.interactionMu.Lock()
	server.cancelPendingInteractionLocked(meetingID)
	server.interactionMu.Unlock()

	if !server.consumeInteractionTranscript(stt.TranscriptResult{MeetingID: meetingID, Text: "退出修改模式", IsFinal: true, Provider: "test"}) {
		t.Fatal("expected exit edit mode command to be consumed")
	}
	if server.interactionEditModeActive(meetingID, time.Now()) {
		t.Fatal("expected edit mode to be inactive after exit")
	}
	if server.consumeInteractionTranscript(stt.TranscriptResult{MeetingID: meetingID, Text: "这句话应该进入全文转写", IsFinal: true, Provider: "test"}) {
		t.Fatal("expected normal speech to stop being consumed after exit")
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
	if !strings.HasPrefix(decisions[0].SourceText, "待办：") || !strings.Contains(decisions[0].SourceText, "Investigate ASR connection keepalive") {
		t.Fatalf("expected moved decision source to mention original action item, got %q", decisions[0].SourceText)
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
	_, _, err = server.executeInteractionCommand(ctx, meetingID, InteractionCommand{
		Intent:     "undo_last_interaction",
		TargetType: "interaction",
	})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err = store.GetIntelligenceSnapshot(ctx, meetingID)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Summary == nil ||
		!strings.Contains(snapshot.Summary.Content, "报名人数") ||
		strings.Contains(snapshot.Summary.Content, "人员时间冲突") {
		t.Fatalf("expected summary undo to restore previous content, got %#v", snapshot.Summary)
	}
}

func TestInteractionUpdateMeetingTitleAndUndo(t *testing.T) {
	ctx := context.Background()
	store, meetingID := newInteractionStore(t, ctx)
	server := &Server{store: store, hub: NewHub()}

	_, message, err := server.executeInteractionCommand(ctx, meetingID, InteractionCommand{
		Intent:     "update_meeting_title",
		TargetType: "meeting",
		Title:      "展会演示准备会",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(message, "展会演示准备会") {
		t.Fatalf("expected title update message, got %q", message)
	}
	meeting, err := store.GetMeeting(ctx, meetingID)
	if err != nil {
		t.Fatal(err)
	}
	if meeting.Title != "展会演示准备会" || !meeting.TitleLocked {
		t.Fatalf("expected updated locked title, got %#v", meeting)
	}

	_, _, err = server.executeInteractionCommand(ctx, meetingID, InteractionCommand{
		Intent:     "undo_last_interaction",
		TargetType: "interaction",
	})
	if err != nil {
		t.Fatal(err)
	}
	meeting, err = store.GetMeeting(ctx, meetingID)
	if err != nil {
		t.Fatal(err)
	}
	if meeting.Title != "interaction" || meeting.TitleLocked {
		t.Fatalf("expected title undo to restore original title and lock state, got %#v", meeting)
	}
}

func TestParseMeetingTitleLocally(t *testing.T) {
	command, ok := parseMeetingTitleLocally("把会议标题改成展会演示准备会")
	if !ok {
		t.Fatal("expected local meeting title parser to match")
	}
	command = normalizeInteractionCommand(command)
	if command.Intent != "update_meeting_title" || command.TargetType != "meeting" || command.Title != "展会演示准备会" {
		t.Fatalf("unexpected parsed title command: %#v", command)
	}

	command, ok = parseMeetingTitleLocally("设置标题为客户试点复盘会，谢谢")
	if !ok {
		t.Fatal("expected local meeting title parser to match polite suffix")
	}
	command = normalizeInteractionCommand(command)
	if command.Title != "客户试点复盘会" {
		t.Fatalf("expected polite suffix to be removed, got %#v", command)
	}
}

func TestParseCorrectionLocally(t *testing.T) {
	command, ok := parseCorrectionLocally("不是新增一勾，是新增一个供应商限制模块")
	if !ok {
		t.Fatal("expected correction parser to match not-a-but-b phrase")
	}
	if command.Intent != "correct_last_interaction" || command.Content != "新增一个供应商限制模块" {
		t.Fatalf("unexpected correction command: %#v", command)
	}

	command, ok = parseCorrectionLocally("刚才说错了，应该删除第二条待办")
	if !ok {
		t.Fatal("expected correction parser to match spoken mistake phrase")
	}
	if command.Content != "删除第二条待办" {
		t.Fatalf("unexpected corrected instruction: %#v", command)
	}
}

func TestCorrectionInteractionUndoAndReplay(t *testing.T) {
	ctx := context.Background()
	store, meetingID := newInteractionStore(t, ctx)
	if _, err := store.SaveLLMConfigFor(ctx, LLMUseCaseInteraction, LLMConfig{BaseURL: "https://llm.example/v1", Model: "test-model"}); err != nil {
		t.Fatal(err)
	}
	server := &Server{
		store: store,
		hub:   NewHub(),
		llmClient: fakeLLMClient{content: `{
			"intent": "create_action_item",
			"target_type": "action_item",
			"task": "Check booth network before rehearsal",
			"confidence": 0.95,
			"need_confirm": false,
			"reply": "ok"
		}`},
	}

	_, _, err := server.executeInteractionCommandWithEventID(ctx, meetingID, InteractionCommand{
		Intent:     "create_action_item",
		TargetType: "action_item",
		Task:       "Wrong temporary task",
	}, "ie_wrong")
	if err != nil {
		t.Fatal(err)
	}

	snapshot, message, correctedCommand, _, err := server.executeCorrectionInteraction(
		ctx,
		meetingID,
		"ie_correct",
		"刚才说错了，应该新增检查展台网络的待办",
		"新增检查展台网络的待办",
		newInteractionTiming(interactionTimingSeed{}),
	)
	if err != nil {
		t.Fatal(err)
	}
	if correctedCommand.Intent != "create_action_item" || !strings.Contains(message, "已纠正口误") {
		t.Fatalf("expected correction to replay create action item, got command=%#v message=%q", correctedCommand, message)
	}
	if snapshot == nil {
		t.Fatal("expected correction snapshot")
	}
	items, err := store.ListActionItems(ctx, meetingID)
	if err != nil {
		t.Fatal(err)
	}
	foundCorrected := false
	for _, item := range items {
		if item.Task == "Wrong temporary task" {
			t.Fatalf("expected mistaken item to be deleted, got %#v", items)
		}
		if item.Task == "Check booth network before rehearsal" {
			foundCorrected = true
		}
	}
	if !foundCorrected {
		t.Fatalf("expected corrected item to be created, got %#v", items)
	}

	_, _, err = server.executeInteractionCommand(ctx, meetingID, InteractionCommand{Intent: "undo_last_interaction", TargetType: "interaction"})
	if err != nil {
		t.Fatal(err)
	}
	items, err = store.ListActionItems(ctx, meetingID)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		if item.Task == "Check booth network before rehearsal" {
			t.Fatalf("expected undo after correction to remove corrected item, got %#v", items)
		}
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

func TestInteractionRiskAndOpenQuestionCommands(t *testing.T) {
	ctx := context.Background()
	store, meetingID := newInteractionStore(t, ctx)
	server := &Server{store: store, hub: NewHub()}

	_, _, err := server.executeInteractionCommand(ctx, meetingID, InteractionCommand{
		Intent:      "update_risk",
		TargetType:  "risk",
		TargetIndex: 1,
		Owner:       "Li Na",
		Mitigation:  "Prepare a backup microphone",
		DueDate:     "Wednesday",
		Level:       "high",
	})
	if err != nil {
		t.Fatal(err)
	}
	risks, err := store.ListRisks(ctx, meetingID)
	if err != nil {
		t.Fatal(err)
	}
	if risks[0].Owner != "Li Na" || risks[0].Mitigation != "Prepare a backup microphone" || risks[0].DueDate != "Wednesday" || risks[0].Level != "high" {
		t.Fatalf("expected first risk to be updated, got %#v", risks[0])
	}

	_, _, err = server.executeInteractionCommand(ctx, meetingID, InteractionCommand{
		Intent:      "close_risk",
		TargetType:  "risk",
		TargetIndex: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	risks, err = store.ListRisks(ctx, meetingID)
	if err != nil {
		t.Fatal(err)
	}
	if risks[0].Status != "completed" {
		t.Fatalf("expected first risk completed, got %#v", risks[0])
	}

	_, _, err = server.executeInteractionCommand(ctx, meetingID, InteractionCommand{
		Intent:     "create_risk",
		TargetType: "risk",
		Content:    "Battery overheating may affect long recording",
		Owner:      "Chen Yu",
		Mitigation: "Prepare a cooling stand",
	})
	if err != nil {
		t.Fatal(err)
	}
	risks, err = store.ListRisks(ctx, meetingID)
	if err != nil {
		t.Fatal(err)
	}
	if len(risks) != 3 {
		t.Fatalf("expected new risk, got %#v", risks)
	}

	_, _, err = server.executeInteractionCommand(ctx, meetingID, InteractionCommand{
		Intent:        "merge_risks",
		TargetType:    "risk",
		SourceIndices: []int{2, 3},
		Content:       "Venue network and battery risks need an onsite fallback plan",
	})
	if err != nil {
		t.Fatal(err)
	}
	risks, err = store.ListRisks(ctx, meetingID)
	if err != nil {
		t.Fatal(err)
	}
	if len(risks) != 2 || !strings.Contains(risks[1].Content, "onsite fallback") {
		t.Fatalf("expected merged risk, got %#v", risks)
	}

	_, _, err = server.executeInteractionCommand(ctx, meetingID, InteractionCommand{
		Intent:     "undo_last_interaction",
		TargetType: "interaction",
	})
	if err != nil {
		t.Fatal(err)
	}
	risks, err = store.ListRisks(ctx, meetingID)
	if err != nil {
		t.Fatal(err)
	}
	if len(risks) != 3 {
		t.Fatalf("expected risk merge undo to restore 3 risks, got %#v", risks)
	}

	_, _, err = server.executeInteractionCommand(ctx, meetingID, InteractionCommand{
		Intent:      "update_open_question",
		TargetType:  "open_question",
		TargetIndex: 1,
		Owner:       "Zhao Qiang",
		DueDate:     "Wednesday",
	})
	if err != nil {
		t.Fatal(err)
	}
	questions, err := store.ListOpenQuestions(ctx, meetingID)
	if err != nil {
		t.Fatal(err)
	}
	if questions[0].Owner != "Zhao Qiang" || questions[0].DueDate != "Wednesday" {
		t.Fatalf("expected first open question to be updated, got %#v", questions[0])
	}

	_, _, err = server.executeInteractionCommand(ctx, meetingID, InteractionCommand{
		Intent:      "close_open_question",
		TargetType:  "open_question",
		TargetIndex: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	questions, err = store.ListOpenQuestions(ctx, meetingID)
	if err != nil {
		t.Fatal(err)
	}
	if questions[0].Status != "completed" {
		t.Fatalf("expected first open question completed, got %#v", questions[0])
	}

	_, _, err = server.executeInteractionCommand(ctx, meetingID, InteractionCommand{
		Intent:     "create_open_question",
		TargetType: "open_question",
		Question:   "Whether the customer demo needs a guest network",
		Owner:      "Wang Lei",
	})
	if err != nil {
		t.Fatal(err)
	}
	questions, err = store.ListOpenQuestions(ctx, meetingID)
	if err != nil {
		t.Fatal(err)
	}
	if len(questions) != 3 {
		t.Fatalf("expected new open question, got %#v", questions)
	}

	_, _, err = server.executeInteractionCommand(ctx, meetingID, InteractionCommand{
		Intent:        "merge_open_questions",
		TargetType:    "open_question",
		SourceIndices: []int{2, 3},
		Question:      "Confirm offline mode and guest network requirements",
	})
	if err != nil {
		t.Fatal(err)
	}
	questions, err = store.ListOpenQuestions(ctx, meetingID)
	if err != nil {
		t.Fatal(err)
	}
	if len(questions) != 2 || !strings.Contains(questions[1].Question, "guest network") {
		t.Fatalf("expected merged open question, got %#v", questions)
	}

	_, _, err = server.executeInteractionCommand(ctx, meetingID, InteractionCommand{
		Intent:     "undo_last_interaction",
		TargetType: "interaction",
	})
	if err != nil {
		t.Fatal(err)
	}
	questions, err = store.ListOpenQuestions(ctx, meetingID)
	if err != nil {
		t.Fatal(err)
	}
	if len(questions) != 3 {
		t.Fatalf("expected open question merge undo to restore 3 questions, got %#v", questions)
	}
}

func TestToolExecutorRecordsJournalAndUndo(t *testing.T) {
	ctx := context.Background()
	store, meetingID := newInteractionStore(t, ctx)
	server := &Server{store: store, hub: NewHub()}
	executor := NewToolExecutor(NewMeetingToolRegistry(server), store)

	readResult, err := executor.Execute(ctx, ToolExecutionInput{
		MeetingID:          meetingID,
		InteractionEventID: "ie_tool_read",
		StepIndex:          1,
		ToolName:           ToolMeetingGetSnapshot,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !readResult.Success || readResult.Data == nil {
		t.Fatalf("expected snapshot tool result, got %#v", readResult)
	}

	alertResult, err := executor.Execute(ctx, ToolExecutionInput{
		MeetingID:          meetingID,
		InteractionEventID: "ie_tool_read",
		StepIndex:          2,
		ToolName:           ToolMissingAlertList,
		Args:               map[string]any{"target_type": "risk", "field": "mitigation"},
	})
	if err != nil {
		t.Fatal(err)
	}
	alerts, ok := alertResult.Data.([]MissingFieldAlert)
	if !ok || len(alerts) == 0 {
		t.Fatalf("expected filtered missing alerts, got %#v", alertResult.Data)
	}

	_, err = executor.Execute(ctx, ToolExecutionInput{
		MeetingID:          meetingID,
		InteractionEventID: "ie_tool_write",
		StepIndex:          1,
		ToolName:           ToolRiskUpdate,
		Args: map[string]any{
			"target_index": 1,
			"owner":        "Li Na",
			"mitigation":   "Prepare a backup microphone",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	records, err := store.ListOperationJournalRecords(ctx, "ie_tool_write")
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 operation journal record, got %#v", records)
	}
	if records[0].ToolName != string(ToolRiskUpdate) || records[0].Status != "success" {
		t.Fatalf("unexpected operation journal record: %#v", records[0])
	}
	if records[0].BeforeJSON == "" || records[0].AfterJSON == "" || records[0].UndoJSON == "" {
		t.Fatalf("expected journal to keep before/after/undo, got %#v", records[0])
	}

	_, _, err = server.executeInteractionCommand(ctx, meetingID, InteractionCommand{
		Intent:     "undo_last_interaction",
		TargetType: "interaction",
	})
	if err != nil {
		t.Fatal(err)
	}
	risks, err := store.ListRisks(ctx, meetingID)
	if err != nil {
		t.Fatal(err)
	}
	if risks[0].Owner == "Li Na" || risks[0].Mitigation == "Prepare a backup microphone" {
		t.Fatalf("expected tool undo to restore original risk, got %#v", risks[0])
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

func TestLocalDecisionParserUpdatesFirstDecision(t *testing.T) {
	ctx := context.Background()
	store, meetingID := newInteractionStore(t, ctx)
	server := &Server{store: store, hub: NewHub()}

	first, err := store.CreateDecision(ctx, meetingID, "第一版演示先使用云端 ASR。", "test")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateDecision(ctx, meetingID, "现场网络异常时使用预录文本兜底。", "test"); err != nil {
		t.Fatal(err)
	}

	command, _, err := server.parseInteractionCommand(ctx, meetingID, "把第一条决策改成第一版演示重点展示会中语音交互能力", newInteractionTiming(interactionTimingSeed{}))
	if err != nil {
		t.Fatal(err)
	}
	if command.Intent != "update_decision" || command.TargetIndex != 1 || command.TargetID != first.ID {
		t.Fatalf("expected first decision update command, got %#v", command)
	}

	snapshot, _, err := server.executeInteractionCommand(ctx, meetingID, command)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Decisions) != 2 || snapshot.Decisions[0].Content != "第一版演示重点展示会中语音交互能力" {
		t.Fatalf("expected first decision to be updated, got %#v", snapshot.Decisions)
	}
}

func TestDecisionUpdateWithUndoWordDoesNotTriggerUndo(t *testing.T) {
	ctx := context.Background()
	store, meetingID := newInteractionStore(t, ctx)
	server := &Server{store: store, hub: NewHub()}

	first, err := store.CreateDecision(ctx, meetingID, "第一版演示先使用云端 ASR。", "test")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateDecision(ctx, meetingID, "现场网络异常时使用预录文本兜底。", "test"); err != nil {
		t.Fatal(err)
	}

	command, _, err := server.parseInteractionCommand(ctx, meetingID, "把第一条决策改为正式演示，优先展示会中语音和修改撤销能力", newInteractionTiming(interactionTimingSeed{}))
	if err != nil {
		t.Fatal(err)
	}
	if command.Intent == "undo_last_interaction" {
		t.Fatalf("expected decision update, not undo: %#v", command)
	}
	if command.Intent != "update_decision" || command.TargetIndex != 1 || command.TargetID != first.ID {
		t.Fatalf("expected first decision update command, got %#v", command)
	}
	if !strings.Contains(command.Content, "撤销能力") {
		t.Fatalf("expected undo word to stay in decision content, got %#v", command)
	}
}

func TestUndoParserRequiresUndoAsPrimaryAction(t *testing.T) {
	if command, ok := parseUndoLocally("把第一条决策改成支持修改撤销能力"); ok || command.Intent == "undo_last_interaction" {
		t.Fatalf("expected embedded undo word to be treated as content, got %#v", command)
	}
	command, ok := parseUndoLocally("撤销刚刚的操作")
	if !ok || command.Intent != "undo_last_interaction" {
		t.Fatalf("expected explicit undo command, got ok=%v command=%#v", ok, command)
	}
}

func TestInteractionCommandWaitsForStableTranscript(t *testing.T) {
	ctx := context.Background()
	store, meetingID := newInteractionStore(t, ctx)
	server := &Server{
		store:               store,
		hub:                 NewHub(),
		pendingInteractions: map[string]*PendingInteraction{},
		recentInteractions:  map[string][]recentInteractionTranscript{},
	}

	if !server.consumeInteractionTranscript(stt.TranscriptResult{MeetingID: meetingID, Text: "小艾小艾，新增一个待确认问题，客户现场", IsFinal: true, Provider: "test"}) {
		t.Fatal("expected partial command to be consumed")
	}
	time.Sleep(1200 * time.Millisecond)
	events, err := store.ListInteractionEvents(ctx, meetingID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 0 {
		t.Fatalf("expected partial command to wait for more transcript, got %#v", events)
	}

	if !server.consumeInteractionTranscript(stt.TranscriptResult{MeetingID: meetingID, Text: "新增一个待确认问题，客户现场是否允许接入外网", IsFinal: true, Provider: "test"}) {
		t.Fatal("expected completed command to be consumed")
	}
	waitFor(t, 5*time.Second, func() bool {
		events, err := store.ListInteractionEvents(ctx, meetingID)
		return err == nil && len(events) == 1
	})
	snapshot, err := store.GetIntelligenceSnapshot(ctx, meetingID)
	if err != nil {
		t.Fatal(err)
	}
	if !containsOpenQuestion(snapshot.OpenQuestions, "客户现场是否允许接入外网") {
		t.Fatalf("expected completed open question, got %#v", snapshot.OpenQuestions)
	}
}

func TestUndoActionItemMergeAndMoveToDecision(t *testing.T) {
	ctx := context.Background()
	store, meetingID := newInteractionStore(t, ctx)
	server := &Server{store: store, hub: NewHub()}

	moveTarget, err := store.CreateActionItem(ctx, meetingID, ActionItemMutation{Task: "Confirm demo network route", Owner: "Alice", Status: "open"})
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = server.executeInteractionCommandWithEventID(ctx, meetingID, InteractionCommand{
		Intent:     "move_action_item_to_decision",
		TargetType: "action_item",
		TargetID:   moveTarget.ID,
		Content:    "Use the wired route for the demo network.",
	}, "ie_move_action")
	if err != nil {
		t.Fatal(err)
	}
	decisions, err := store.ListDecisions(ctx, meetingID)
	if err != nil {
		t.Fatal(err)
	}
	if len(decisions) != 1 || !strings.Contains(decisions[0].Content, "wired route") {
		t.Fatalf("expected moved action item decision, got %#v", decisions)
	}
	_, _, err = server.executeInteractionCommand(ctx, meetingID, InteractionCommand{Intent: "undo_last_interaction", TargetType: "interaction"})
	if err != nil {
		t.Fatal(err)
	}
	items, err := store.ListActionItems(ctx, meetingID)
	if err != nil {
		t.Fatal(err)
	}
	foundOriginal := false
	for _, item := range items {
		if item.ID == moveTarget.ID && item.Task == moveTarget.Task && !item.IsDeleted {
			foundOriginal = true
		}
	}
	if !foundOriginal {
		t.Fatalf("expected moved action item to be restored, got %#v", items)
	}
	decisions, err = store.ListDecisions(ctx, meetingID)
	if err != nil {
		t.Fatal(err)
	}
	if len(decisions) != 0 {
		t.Fatalf("expected created decision to be removed by undo, got %#v", decisions)
	}

	mergeOne, err := store.CreateActionItem(ctx, meetingID, ActionItemMutation{Task: "Prepare rehearsal laptop", Owner: "Bob", Status: "completed"})
	if err != nil {
		t.Fatal(err)
	}
	mergeTwo, err := store.CreateActionItem(ctx, meetingID, ActionItemMutation{Task: "Prepare HDMI adapter", Owner: "Carol", Status: "open"})
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = server.executeInteractionCommandWithEventID(ctx, meetingID, InteractionCommand{
		Intent:    "merge_action_items",
		SourceIDs: []string{mergeOne.ID, mergeTwo.ID},
		Task:      "Prepare rehearsal devices",
	}, "ie_merge_action")
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = server.executeInteractionCommand(ctx, meetingID, InteractionCommand{Intent: "undo_last_interaction", TargetType: "interaction"})
	if err != nil {
		t.Fatal(err)
	}
	items, err = store.ListActionItems(ctx, meetingID)
	if err != nil {
		t.Fatal(err)
	}
	foundOne := false
	foundTwo := false
	for _, item := range items {
		if item.ID == mergeOne.ID && item.Task == mergeOne.Task && item.Status == mergeOne.Status {
			foundOne = true
		}
		if item.ID == mergeTwo.ID && item.Task == mergeTwo.Task && item.Status == mergeTwo.Status {
			foundTwo = true
		}
	}
	if !foundOne || !foundTwo {
		t.Fatalf("expected merged action items to be restored, got %#v", items)
	}
}

func TestUndoDecisionMergeAndMoveToActionItem(t *testing.T) {
	ctx := context.Background()
	store, meetingID := newInteractionStore(t, ctx)
	server := &Server{store: store, hub: NewHub()}

	first, err := store.CreateDecision(ctx, meetingID, "Use cloud ASR in the demo.", "test")
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.CreateDecision(ctx, meetingID, "Keep interaction parsing separate.", "test")
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = server.executeInteractionCommandWithEventID(ctx, meetingID, InteractionCommand{
		Intent:    "merge_decisions",
		SourceIDs: []string{first.ID, second.ID},
	}, "ie_merge_decision")
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = server.executeInteractionCommand(ctx, meetingID, InteractionCommand{Intent: "undo_last_interaction", TargetType: "interaction"})
	if err != nil {
		t.Fatal(err)
	}
	decisions, err := store.ListDecisions(ctx, meetingID)
	if err != nil {
		t.Fatal(err)
	}
	foundFirst := false
	foundSecond := false
	for _, decision := range decisions {
		if decision.ID == first.ID && decision.Content == first.Content {
			foundFirst = true
		}
		if decision.ID == second.ID && decision.Content == second.Content {
			foundSecond = true
		}
	}
	if !foundFirst || !foundSecond {
		t.Fatalf("expected merged decisions to be restored, got %#v", decisions)
	}

	_, _, err = server.executeInteractionCommandWithEventID(ctx, meetingID, InteractionCommand{
		Intent:     "move_decision_to_action_item",
		TargetType: "decision",
		TargetID:   first.ID,
		Task:       "Evaluate cloud ASR demo result",
		Owner:      "Dana",
	}, "ie_move_decision")
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = server.executeInteractionCommand(ctx, meetingID, InteractionCommand{Intent: "undo_last_interaction", TargetType: "interaction"})
	if err != nil {
		t.Fatal(err)
	}
	decisions, err = store.ListDecisions(ctx, meetingID)
	if err != nil {
		t.Fatal(err)
	}
	foundFirst = false
	for _, decision := range decisions {
		if decision.ID == first.ID && decision.Content == first.Content {
			foundFirst = true
		}
	}
	if !foundFirst {
		t.Fatalf("expected moved decision to be restored, got %#v", decisions)
	}
	items, err := store.ListActionItems(ctx, meetingID)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		if item.Task == "Evaluate cloud ASR demo result" {
			t.Fatalf("expected action item created by move to be removed, got %#v", items)
		}
	}
}

func TestInteractionCommandControlsDynamicSections(t *testing.T) {
	ctx := context.Background()
	store, meetingID := newInteractionStore(t, ctx)
	server := &Server{store: store, hub: NewHub()}

	snapshot, err := store.SaveIntelligenceExtraction(ctx, meetingID, IntelligenceExtraction{
		DynamicSections: []DynamicSectionExtraction{{
			SectionKey:  "budget_constraints",
			Title:       "Budget Constraints",
			Description: "Budget constraints discussed in the meeting.",
			Items: []DynamicSectionItemExtraction{{
				Title:   "Demo budget ceiling",
				Content: "The demo budget should stay below 80,000 yuan.",
			}},
		}},
	}, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.DynamicSections) != 1 || snapshot.DynamicSections[0].Visibility != "visible" {
		t.Fatalf("expected visible dynamic section, got %#v", snapshot.DynamicSections)
	}

	snapshot, _, err = server.executeInteractionCommand(ctx, meetingID, InteractionCommand{
		Intent:       "show_dynamic_section",
		TargetType:   "dynamic_section",
		SectionTitle: "Budget Constraints",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.DynamicSections) != 1 || snapshot.DynamicSections[0].Visibility != "visible" {
		t.Fatalf("expected dynamic section to become visible, got %#v", snapshot.DynamicSections)
	}

	snapshot, _, err = server.executeInteractionCommand(ctx, meetingID, InteractionCommand{
		Intent:     "hide_all_dynamic_sections",
		TargetType: "dynamic_section",
	})
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.DynamicSections[0].Visibility != "hidden" {
		t.Fatalf("expected dynamic section to become hidden, got %#v", snapshot.DynamicSections[0])
	}

	snapshot, _, err = server.executeInteractionCommand(ctx, meetingID, InteractionCommand{
		Intent:     "undo_last_interaction",
		TargetType: "interaction",
	})
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.DynamicSections[0].Visibility != "visible" {
		t.Fatalf("expected undo to restore visible dynamic section, got %#v", snapshot.DynamicSections[0])
	}
}

func TestInteractionCommandCreatesDynamicSectionAndUndo(t *testing.T) {
	ctx := context.Background()
	store, meetingID := newInteractionStore(t, ctx)
	server := &Server{store: store, hub: NewHub()}

	snapshot, _, err := server.executeInteractionCommand(ctx, meetingID, InteractionCommand{
		Intent:       "create_dynamic_section",
		TargetType:   "dynamic_section",
		SectionTitle: "Budget Constraints",
		Content:      "Cloud service cost must stay below 5,000 yuan this month.",
		Instruction:  "Create a budget constraints module.",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.DynamicSections) != 1 {
		t.Fatalf("expected one dynamic section, got %#v", snapshot.DynamicSections)
	}
	section := snapshot.DynamicSections[0]
	if section.Title != "Budget Constraints" || section.Source != "user" || section.Visibility != "visible" {
		t.Fatalf("expected visible user dynamic section, got %#v", section)
	}
	if len(section.Items) != 1 || !strings.Contains(section.Items[0].Content, "5,000") {
		t.Fatalf("expected initial dynamic section item, got %#v", section.Items)
	}

	snapshot, _, err = server.executeInteractionCommand(ctx, meetingID, InteractionCommand{
		Intent:       "add_dynamic_section_item",
		TargetType:   "dynamic_section",
		TargetID:     section.ID,
		SectionTitle: section.Title,
		Content:      "Hardware purchase budget should stay below 20,000 yuan.",
		Instruction:  "Add one item to the budget constraints module.",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.DynamicSections) != 1 || len(snapshot.DynamicSections[0].Items) != 2 {
		t.Fatalf("expected appended dynamic section item, got %#v", snapshot.DynamicSections)
	}
	section = snapshot.DynamicSections[0]

	snapshot, _, err = server.executeInteractionCommand(ctx, meetingID, InteractionCommand{
		Intent:       "update_dynamic_section",
		TargetType:   "dynamic_section",
		TargetID:     section.ID,
		SectionTitle: section.Title,
		Content:      "Track budget limits, approval owners, and exception rules.",
		Instruction:  "Update the budget constraints module description.",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(snapshot.DynamicSections[0].Description, "approval owners") {
		t.Fatalf("expected dynamic section description to update, got %#v", snapshot.DynamicSections[0])
	}

	snapshot, _, err = server.executeInteractionCommand(ctx, meetingID, InteractionCommand{
		Intent:     "undo_last_interaction",
		TargetType: "interaction",
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(snapshot.DynamicSections[0].Description, "approval owners") {
		t.Fatalf("expected undo to restore dynamic section description, got %#v", snapshot.DynamicSections[0])
	}

	snapshot, _, err = server.executeInteractionCommand(ctx, meetingID, InteractionCommand{
		Intent:        "update_dynamic_section_item",
		TargetType:    "dynamic_section",
		TargetID:      section.ID,
		SectionTitle:  section.Title,
		SourceIndices: []int{2},
		Content:       "Hardware purchase budget must stay below 18,000 yuan.",
		Instruction:   "Update the second item in the budget constraints module.",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(snapshot.DynamicSections[0].Items[1].Content, "18,000") {
		t.Fatalf("expected dynamic section item to update, got %#v", snapshot.DynamicSections[0].Items)
	}

	snapshot, _, err = server.executeInteractionCommand(ctx, meetingID, InteractionCommand{
		Intent:     "undo_last_interaction",
		TargetType: "interaction",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(snapshot.DynamicSections[0].Items[1].Content, "20,000") {
		t.Fatalf("expected undo to restore dynamic section item, got %#v", snapshot.DynamicSections[0].Items)
	}

	snapshot, _, err = server.executeInteractionCommand(ctx, meetingID, InteractionCommand{
		Intent:     "undo_last_interaction",
		TargetType: "interaction",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.DynamicSections) != 1 || len(snapshot.DynamicSections[0].Items) != 1 {
		t.Fatalf("expected undo to remove appended item, got %#v", snapshot.DynamicSections)
	}

	snapshot, _, err = server.executeInteractionCommand(ctx, meetingID, InteractionCommand{
		Intent:     "undo_last_interaction",
		TargetType: "interaction",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.DynamicSections) != 0 {
		t.Fatalf("expected undo to remove created dynamic section, got %#v", snapshot.DynamicSections)
	}
}

func TestLocalDynamicSectionParserDoesNotTreatAddOneItemAsFirstModule(t *testing.T) {
	ctx := context.Background()
	store, meetingID := newInteractionStore(t, ctx)
	server := &Server{store: store, hub: NewHub()}

	if _, err := store.CreateDynamicSection(ctx, meetingID, "预算约束", "", "记录预算边界", "云服务费用先控制在三千以内", "test"); err != nil {
		t.Fatal(err)
	}
	supplier, err := store.CreateDynamicSection(ctx, meetingID, "供应商限制", "", "记录供应商侧限制", "临时采购需要主管确认", "test")
	if err != nil {
		t.Fatal(err)
	}

	command, _, err := server.parseInteractionCommand(ctx, meetingID, "给供应商限制模块补充一条，摄像头到货时间最晚不能超过周三", newInteractionTiming(interactionTimingSeed{}))
	if err != nil {
		t.Fatal(err)
	}
	if command.Intent != "add_dynamic_section_item" {
		t.Fatalf("expected add dynamic section item intent, got %#v", command)
	}
	if command.TargetID != supplier.ID {
		t.Fatalf("expected supplier module target %s, got %#v", supplier.ID, command)
	}
	if strings.Contains(command.Content, "补充一条") || !strings.Contains(command.Content, "摄像头到货时间") {
		t.Fatalf("expected only appended content, got %#v", command)
	}

	snapshot, _, err := server.executeInteractionCommand(ctx, meetingID, command)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.DynamicSections) != 2 {
		t.Fatalf("expected two dynamic sections, got %#v", snapshot.DynamicSections)
	}
	if len(snapshot.DynamicSections[0].Items) != 1 {
		t.Fatalf("expected first module to stay unchanged, got %#v", snapshot.DynamicSections[0])
	}
	if len(snapshot.DynamicSections[1].Items) != 2 || !strings.Contains(snapshot.DynamicSections[1].Items[1].Content, "摄像头到货时间") {
		t.Fatalf("expected supplier module to receive item, got %#v", snapshot.DynamicSections[1])
	}
}

func TestInteractionCommandDeletesAndMergesDynamicSections(t *testing.T) {
	ctx := context.Background()
	store, meetingID := newInteractionStore(t, ctx)
	server := &Server{store: store, hub: NewHub()}

	first, err := store.CreateDynamicSection(ctx, meetingID, "预算约束", "", "记录预算边界", "云服务费用先控制在三千以内", "test")
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.CreateDynamicSection(ctx, meetingID, "供应商限制", "", "记录供应商侧限制", "临时采购需要主管确认", "test")
	if err != nil {
		t.Fatal(err)
	}
	third, err := store.CreateDynamicSection(ctx, meetingID, "客户关注点", "", "记录客户侧重点", "客户更关注离线演示稳定性", "test")
	if err != nil {
		t.Fatal(err)
	}

	command, _, err := server.parseInteractionCommand(ctx, meetingID, "删除第 3 个动态模块", newInteractionTiming(interactionTimingSeed{}))
	if err != nil {
		t.Fatal(err)
	}
	if command.Intent != "delete_dynamic_section" || command.TargetID != third.ID {
		t.Fatalf("expected delete third dynamic section command, got %#v", command)
	}
	snapshot, _, err := server.executeInteractionCommand(ctx, meetingID, command)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.DynamicSections) != 2 {
		t.Fatalf("expected one dynamic section deleted, got %#v", snapshot.DynamicSections)
	}
	snapshot, _, err = server.executeInteractionCommand(ctx, meetingID, InteractionCommand{Intent: "undo_last_interaction", TargetType: "interaction"})
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.DynamicSections) != 3 {
		t.Fatalf("expected dynamic section delete undo to restore 3 sections, got %#v", snapshot.DynamicSections)
	}

	snapshot, _, err = server.executeInteractionCommand(ctx, meetingID, InteractionCommand{
		Intent:        "merge_dynamic_sections",
		TargetType:    "dynamic_section",
		SourceIDs:     []string{first.ID, second.ID},
		SectionTitle:  "预算与供应商限制",
		Content:       "统一记录预算边界和供应商审批限制。",
		Instruction:   "合并预算约束和供应商限制模块。",
		SourceIndices: []int{1, 2},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.DynamicSections) != 2 {
		t.Fatalf("expected merged dynamic sections, got %#v", snapshot.DynamicSections)
	}
	merged := snapshot.DynamicSections[0]
	if merged.Title != "预算与供应商限制" || len(merged.Items) != 2 {
		t.Fatalf("expected merged section with two items, got %#v", merged)
	}
	snapshot, _, err = server.executeInteractionCommand(ctx, meetingID, InteractionCommand{Intent: "undo_last_interaction", TargetType: "interaction"})
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.DynamicSections) != 3 {
		t.Fatalf("expected dynamic section merge undo to restore 3 sections, got %#v", snapshot.DynamicSections)
	}
}

func TestDynamicSectionMergeUsesOnlyExplicitSources(t *testing.T) {
	ctx := context.Background()
	store, meetingID := newInteractionStore(t, ctx)
	server := &Server{store: store, hub: NewHub()}

	first, err := store.CreateDynamicSection(ctx, meetingID, "预算约束", "", "记录预算边界", "云服务费用先控制在三千以内", "test")
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.CreateDynamicSection(ctx, meetingID, "供应商限制", "", "记录供应商侧限制", "临时采购需要主管确认", "test")
	if err != nil {
		t.Fatal(err)
	}
	third, err := store.CreateDynamicSection(ctx, meetingID, "成本控制", "", "记录成本侧约束", "差旅成本先不扩大", "test")
	if err != nil {
		t.Fatal(err)
	}

	command, _, err := server.parseInteractionCommand(ctx, meetingID, "把第一条和第二条动态模块合并为一条，改名为成本控制结果", newInteractionTiming(interactionTimingSeed{}))
	if err != nil {
		t.Fatal(err)
	}
	if command.Intent != "merge_dynamic_sections" {
		t.Fatalf("expected merge dynamic sections intent, got %#v", command)
	}
	if len(command.SourceIDs) != 2 || command.SourceIDs[0] != first.ID || command.SourceIDs[1] != second.ID {
		t.Fatalf("expected only first and second source IDs, got %#v", command)
	}
	if command.SectionTitle != "成本控制结果" {
		t.Fatalf("expected merge title 成本控制结果, got %#v", command)
	}

	snapshot, _, err := server.executeInteractionCommand(ctx, meetingID, command)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.DynamicSections) != 2 {
		t.Fatalf("expected two dynamic sections after merge, got %#v", snapshot.DynamicSections)
	}
	if !containsDynamicSectionTitle(snapshot.DynamicSections, "成本控制结果") {
		t.Fatalf("expected merged section title, got %#v", snapshot.DynamicSections)
	}
	if !containsDynamicSectionID(snapshot.DynamicSections, third.ID) {
		t.Fatalf("expected unrelated cost section to remain, got %#v", snapshot.DynamicSections)
	}
}

func TestDynamicSectionRenameAndCleanupAreGeneric(t *testing.T) {
	ctx := context.Background()
	store, meetingID := newInteractionStore(t, ctx)
	server := &Server{store: store, hub: NewHub()}

	section, err := store.CreateDynamicSection(ctx, meetingID, "本控制", "", "旧识别结果", "", "test")
	if err != nil {
		t.Fatal(err)
	}
	command, _, err := server.parseInteractionCommand(ctx, meetingID, "把第一个动态模块改名为成本控制", newInteractionTiming(interactionTimingSeed{}))
	if err != nil {
		t.Fatal(err)
	}
	if command.Intent != "update_dynamic_section" || command.TargetID != section.ID || command.Title != "成本控制" {
		t.Fatalf("expected dynamic section rename command, got %#v", command)
	}
	snapshot, _, err := server.executeInteractionCommand(ctx, meetingID, command)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.DynamicSections) != 1 || snapshot.DynamicSections[0].Title != "成本控制" {
		t.Fatalf("expected section renamed to 成本控制, got %#v", snapshot.DynamicSections)
	}
	if got := cleanupDynamicSectionValue("成本控制模块要记录预算上限"); !strings.HasPrefix(got, "成本控制") {
		t.Fatalf("expected cleanup to keep 成本 prefix, got %q", got)
	}
}

func TestDynamicSectionVoiceSourceTextIsGeneric(t *testing.T) {
	ctx := context.Background()
	store, meetingID := newInteractionStore(t, ctx)
	server := &Server{store: store, hub: NewHub()}

	section, err := store.CreateDynamicSection(ctx, meetingID, "供应商限制", "", "记录供应商侧限制", "", "test")
	if err != nil {
		t.Fatal(err)
	}
	snapshot, _, err := server.executeInteractionCommandWithEventID(ctx, meetingID, InteractionCommand{
		Intent:       "add_dynamic_section_item",
		TargetType:   "dynamic_section",
		TargetID:     section.ID,
		SectionTitle: section.Title,
		Content:      "摄像头到货时间最晚不能超过周三",
		Instruction:  "给供应商限制模块补充一条，摄像头到货时间最晚不能超过周三",
	}, "ie_voice_source")
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.DynamicSections) != 1 || len(snapshot.DynamicSections[0].Items) != 1 {
		t.Fatalf("expected one dynamic item, got %#v", snapshot.DynamicSections)
	}
	if snapshot.DynamicSections[0].Items[0].SourceText != dynamicSectionVoiceSourceText {
		t.Fatalf("expected generic voice source text, got %#v", snapshot.DynamicSections[0].Items[0])
	}
}

func TestLocalRiskMutationParserUsesRequestedIndex(t *testing.T) {
	ctx := context.Background()
	store, meetingID := newInteractionStore(t, ctx)
	server := &Server{store: store, hub: NewHub()}

	if _, err := store.CreateRisk(ctx, meetingID, RiskMutation{Content: "Backup power may be insufficient", Status: "open"}); err != nil {
		t.Fatal(err)
	}
	command, _, err := server.parseInteractionCommand(ctx, meetingID, "修改第一条风险的方案，改成提前准备备用麦克风并安排现场复测", newInteractionTiming(interactionTimingSeed{}))
	if err != nil {
		t.Fatal(err)
	}
	if command.Intent != "update_risk" || command.TargetIndex != 1 || command.Mitigation == "" {
		t.Fatalf("expected local risk mitigation update for first risk, got %#v", command)
	}
	snapshot, _, err := server.executeInteractionCommand(ctx, meetingID, command)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(snapshot.Risks[0].Mitigation, "备用麦克风") {
		t.Fatalf("expected first risk mitigation to change, got %#v", snapshot.Risks)
	}
	if snapshot.Risks[2].Mitigation != "" {
		t.Fatalf("expected third risk to stay unchanged, got %#v", snapshot.Risks[2])
	}
}

func TestLocalOpenQuestionParserBeatsRiskFallback(t *testing.T) {
	ctx := context.Background()
	store, meetingID := newInteractionStore(t, ctx)
	server := &Server{store: store, hub: NewHub()}

	if _, err := store.CreateOpenQuestion(ctx, meetingID, OpenQuestionMutation{Question: "客户是否允许现场录音留存", Status: "open"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateOpenQuestion(ctx, meetingID, OpenQuestionMutation{Question: "第四条确认问题是否需要法务复核", Status: "open"}); err != nil {
		t.Fatal(err)
	}

	command, _, err := server.parseInteractionCommand(ctx, meetingID, "把第四条待确认问题的截止时间改为周三下午。", newInteractionTiming(interactionTimingSeed{}))
	if err != nil {
		t.Fatal(err)
	}
	if command.Intent != "update_open_question" || command.TargetType != "open_question" || command.TargetIndex != 4 || command.DueDate != "周三下午" {
		t.Fatalf("expected local open question due date update, got %#v", command)
	}
	snapshot, _, err := server.executeInteractionCommand(ctx, meetingID, command)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.OpenQuestions) < 4 || snapshot.OpenQuestions[3].DueDate != "周三下午" {
		t.Fatalf("expected fourth open question due date to change, got %#v", snapshot.OpenQuestions)
	}
	for _, risk := range snapshot.Risks {
		if risk.DueDate == "周三下午" {
			t.Fatalf("expected risks to stay unchanged, got %#v", snapshot.Risks)
		}
	}
}

func TestLocalOpenQuestionParserDeletesMultipleExplicitTargets(t *testing.T) {
	ctx := context.Background()
	store, meetingID := newInteractionStore(t, ctx)
	server := &Server{store: store, hub: NewHub()}

	third, err := store.CreateOpenQuestion(ctx, meetingID, OpenQuestionMutation{Question: "第三条需要确认", Status: "open"})
	if err != nil {
		t.Fatal(err)
	}
	fourth, err := store.CreateOpenQuestion(ctx, meetingID, OpenQuestionMutation{Question: "第四条需要确认", Status: "open"})
	if err != nil {
		t.Fatal(err)
	}
	fifth, err := store.CreateOpenQuestion(ctx, meetingID, OpenQuestionMutation{Question: "第五条应该保留", Status: "open"})
	if err != nil {
		t.Fatal(err)
	}

	command, _, err := server.parseInteractionCommand(ctx, meetingID, "删除第 3 和第 4 条待确认问题", newInteractionTiming(interactionTimingSeed{}))
	if err != nil {
		t.Fatal(err)
	}
	if command.Intent != "delete_open_questions" || len(command.SourceIDs) != 2 {
		t.Fatalf("expected bulk open question delete, got %#v", command)
	}
	snapshot, _, err := server.executeInteractionCommand(ctx, meetingID, command)
	if err != nil {
		t.Fatal(err)
	}
	if containsOpenQuestion(snapshot.OpenQuestions, third.Question) || containsOpenQuestion(snapshot.OpenQuestions, fourth.Question) {
		t.Fatalf("expected third and fourth questions deleted, got %#v", snapshot.OpenQuestions)
	}
	if !containsOpenQuestion(snapshot.OpenQuestions, fifth.Question) {
		t.Fatalf("expected fifth question to remain, got %#v", snapshot.OpenQuestions)
	}
}

func TestDynamicSectionTitleKeepsCostPrefix(t *testing.T) {
	command, ok := parseDynamicSectionMutationLocally("新增一个成本控制模块。", &IntelligenceSnapshot{})
	if !ok {
		t.Fatal("expected dynamic section command")
	}
	if command.SectionTitle != "成本控制" {
		t.Fatalf("expected 成本控制 title, got %#v", command)
	}
}

func TestDynamicSectionTitleAfterModuleNameMarker(t *testing.T) {
	cases := []string{
		"新增一个动态模块叫观众反馈",
		"新增一个模块叫观众反馈",
		"新增一个动态模块命名为观众反馈",
		"新增一个叫观众反馈的动态模块",
	}
	for _, instruction := range cases {
		command, ok := parseDynamicSectionMutationLocally(instruction, &IntelligenceSnapshot{})
		if !ok {
			t.Fatalf("expected dynamic section command for %q", instruction)
		}
		if command.Intent != "create_dynamic_section" || command.SectionTitle != "观众反馈" || command.Title != "观众反馈" {
			t.Fatalf("expected title 观众反馈 for %q, got %#v", instruction, command)
		}
		if command.Content != "" {
			t.Fatalf("expected empty content for title-only command %q, got %#v", instruction, command)
		}
	}
}

func TestRecentInteractionEchoIsConsumedAfterCommandFires(t *testing.T) {
	ctx := context.Background()
	store, meetingID := newInteractionStore(t, ctx)
	server := &Server{
		store:               store,
		hub:                 NewHub(),
		pendingInteractions: map[string]*PendingInteraction{},
		recentInteractions:  map[string][]recentInteractionTranscript{},
	}

	if !server.consumeInteractionTranscript(stt.TranscriptResult{MeetingID: meetingID, Text: "小艾小艾，新增一个成本控制模块", IsFinal: true, Provider: "test"}) {
		t.Fatal("expected wake command to be consumed")
	}
	waitFor(t, 5*time.Second, func() bool {
		events, err := store.ListInteractionEvents(ctx, meetingID)
		return err == nil && len(events) == 1
	})
	if !server.consumeInteractionTranscript(stt.TranscriptResult{MeetingID: meetingID, Text: "新增一个成本控制模块", IsFinal: true, Provider: "test"}) {
		t.Fatal("expected late ASR echo to be consumed")
	}
}

func TestInteractionLikeExtractionIsNotStoredAsOpenQuestion(t *testing.T) {
	ctx := context.Background()
	store, meetingID := newInteractionStore(t, ctx)

	if _, err := store.SaveIntelligenceExtraction(ctx, meetingID, IntelligenceExtraction{
		OpenQuestions: []OpenQuestionExtraction{
			{Question: "把第四条待确认问题的截止时间改为周三下午", SourceText: "把第四条待确认问题的截止时间改为周三下午", Status: "open"},
			{Question: "删除第 4 和第 5 条待确认问题", SourceText: "删除第 4 和第 5 条待确认问题", Status: "open"},
		},
	}, 3); err != nil {
		t.Fatal(err)
	}
	snapshot, err := store.GetIntelligenceSnapshot(ctx, meetingID)
	if err != nil {
		t.Fatal(err)
	}
	if containsOpenQuestion(snapshot.OpenQuestions, "把第四条待确认问题") || containsOpenQuestion(snapshot.OpenQuestions, "删除第 4") {
		t.Fatalf("expected interaction-like open questions to be skipped, got %#v", snapshot.OpenQuestions)
	}
}

func TestConciseSummaryCommandActuallyShortensSummary(t *testing.T) {
	ctx := context.Background()
	store, meetingID := newInteractionStore(t, ctx)
	server := &Server{store: store, hub: NewHub()}

	longSummary := "会议摘要：本次会议围绕展会现场演示准备展开，重点讨论了转写稳定性、网络权限、现场设备、演示流程和会中交互展示方式。风险提示：麦克风底噪、现场网络限制和设备到货时间都可能影响演示。缺失项提醒：部分任务还缺负责人和截止时间，需要在会后继续确认。下一步：团队需要完成现场联调、演示脚本确认和备用方案准备。"
	if _, err := store.RewriteSummary(ctx, meetingID, longSummary); err != nil {
		t.Fatal(err)
	}
	command, _, err := server.parseInteractionCommand(ctx, meetingID, "把摘要改的简短一点", newInteractionTiming(interactionTimingSeed{}))
	if err != nil {
		t.Fatal(err)
	}
	if command.Intent != "rewrite_summary_section" || !commandRequestsConciseSummary(command) {
		t.Fatalf("expected concise summary command, got %#v", command)
	}
	snapshot, _, err := server.executeInteractionCommand(ctx, meetingID, command)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Summary == nil || len([]rune(snapshot.Summary.Content)) >= len([]rune(longSummary)) {
		t.Fatalf("expected shorter summary, got %#v", snapshot.Summary)
	}
	if strings.Contains(snapshot.Summary.Content, "精简摘要") {
		t.Fatalf("expected generated concise summary instead of instruction text, got %q", snapshot.Summary.Content)
	}
}

func TestPlaceholderStructuredItemsAreReplacedByConcreteItems(t *testing.T) {
	ctx := context.Background()
	store, meetingID := newInteractionStore(t, ctx)

	if _, err := store.SaveIntelligenceExtraction(ctx, meetingID, IntelligenceExtraction{
		ActionItems:   []ActionItemExtraction{{Task: "补充一条现场网络待办", Status: "open"}},
		Risks:         []RiskExtraction{{Content: "补充一条麦克风风险", Status: "open"}},
		OpenQuestions: []OpenQuestionExtraction{{Question: "补充一条客户确认问题", Status: "open"}},
	}, 2); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SaveIntelligenceExtraction(ctx, meetingID, IntelligenceExtraction{
		ActionItems: []ActionItemExtraction{{
			Task:    "现场网络由赵强在周三下午完成权限验证",
			Owner:   "赵强",
			DueDate: "周三下午",
			Status:  "open",
		}},
		Risks: []RiskExtraction{{
			Content:    "麦克风底噪可能影响老板演示",
			Mitigation: "提前准备备用麦克风并现场复测",
			Status:     "open",
		}},
		OpenQuestions: []OpenQuestionExtraction{{
			Question: "客户是否允许现场录音留存",
			DueDate:  "周三下午",
			Status:   "open",
		}},
	}, 3); err != nil {
		t.Fatal(err)
	}

	snapshot, err := store.GetIntelligenceSnapshot(ctx, meetingID)
	if err != nil {
		t.Fatal(err)
	}
	if containsActionTask(snapshot.ActionItems, "补充一条现场网络待办") ||
		containsRiskContent(snapshot.Risks, "补充一条麦克风风险") ||
		containsOpenQuestion(snapshot.OpenQuestions, "补充一条客户确认问题") {
		t.Fatalf("expected placeholder items to be replaced, got %#v", snapshot)
	}
	if !containsActionTask(snapshot.ActionItems, "现场网络由赵强在周三下午完成权限验证") ||
		!containsRiskContent(snapshot.Risks, "麦克风底噪可能影响老板演示") ||
		!containsOpenQuestion(snapshot.OpenQuestions, "客户是否允许现场录音留存") {
		t.Fatalf("expected concrete items to be kept, got %#v", snapshot)
	}
}

func TestDynamicSectionDeleteUndoRestoresAllItems(t *testing.T) {
	ctx := context.Background()
	store, meetingID := newInteractionStore(t, ctx)
	server := &Server{store: store, hub: NewHub()}

	section, err := store.CreateDynamicSection(ctx, meetingID, "供应商限制", "", "记录供应商限制", "", "test")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.AddDynamicSectionItem(ctx, meetingID, section.ID, "", "临时设备采购费用不能超过三千", "test"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.AddDynamicSectionItem(ctx, meetingID, section.ID, "", "摄像头到货时间最晚不能超过周三", "test"); err != nil {
		t.Fatal(err)
	}

	snapshot, _, err := server.executeInteractionCommand(ctx, meetingID, InteractionCommand{
		Intent:      "delete_dynamic_section",
		TargetType:  "dynamic_section",
		TargetID:    section.ID,
		TargetIndex: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.DynamicSections) != 0 {
		t.Fatalf("expected dynamic section to be deleted, got %#v", snapshot.DynamicSections)
	}
	snapshot, _, err = server.executeInteractionCommand(ctx, meetingID, InteractionCommand{Intent: "undo_last_interaction", TargetType: "interaction"})
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.DynamicSections) != 1 || len(snapshot.DynamicSections[0].Items) != 2 {
		t.Fatalf("expected undo to restore dynamic section with two items, got %#v", snapshot.DynamicSections)
	}
}

func TestDynamicSectionPlaceholderItemIsUpdatedInPlace(t *testing.T) {
	ctx := context.Background()
	store, meetingID := newInteractionStore(t, ctx)

	section, err := store.CreateDynamicSection(ctx, meetingID, "预算限制", "", "记录预算约束", "", "test")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.AddDynamicSectionItem(ctx, meetingID, section.ID, "", "给预算限制模块补充一条", dynamicSectionVoiceSourceText); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.AddDynamicSectionItem(ctx, meetingID, section.ID, "", "临时设备采购费用不能超过3,000", dynamicSectionVoiceSourceText); err != nil {
		t.Fatal(err)
	}
	sections, err := store.ListDynamicSections(ctx, meetingID)
	if err != nil {
		t.Fatal(err)
	}
	if len(sections) != 1 || len(sections[0].Items) != 1 || !strings.Contains(sections[0].Items[0].Content, "3,000") {
		t.Fatalf("expected placeholder dynamic item to be updated in place, got %#v", sections)
	}
}

func TestOpenQuestionCreateKeepsShiFouAndDeduplicates(t *testing.T) {
	ctx := context.Background()
	store, meetingID := newInteractionStore(t, ctx)
	server := &Server{store: store, hub: NewHub()}
	before, err := store.ListOpenQuestions(ctx, meetingID)
	if err != nil {
		t.Fatal(err)
	}

	command, _, err := server.parseInteractionCommand(ctx, meetingID, "新增一个待确认问题，客户现场是否允许接入外网？", newInteractionTiming(interactionTimingSeed{}))
	if err != nil {
		t.Fatal(err)
	}
	if command.Intent != "create_open_question" || !strings.Contains(command.Question, "是否允许接入外网") {
		t.Fatalf("expected open question create command with 是否, got %#v", command)
	}
	snapshot, _, err := server.executeInteractionCommand(ctx, meetingID, command)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.OpenQuestions) != len(before)+1 || !containsOpenQuestion(snapshot.OpenQuestions, "客户现场是否允许接入外网") {
		t.Fatalf("expected one complete open question, got %#v", snapshot.OpenQuestions)
	}
	snapshot, _, err = server.executeInteractionCommand(ctx, meetingID, command)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.OpenQuestions) != len(before)+1 {
		t.Fatalf("expected repeated command to stay deduplicated, got %#v", snapshot.OpenQuestions)
	}
}

func TestDynamicSectionShortItemIsMergedWithExpandedItem(t *testing.T) {
	ctx := context.Background()
	store, meetingID := newInteractionStore(t, ctx)

	section, err := store.CreateDynamicSection(ctx, meetingID, "硬件设备", "", "记录硬件依赖", "", "test")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.AddDynamicSectionItem(ctx, meetingID, section.ID, "", "硬件设备", dynamicSectionVoiceSourceText); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.AddDynamicSectionItem(ctx, meetingID, section.ID, "", "硬件设备周五前到齐", dynamicSectionVoiceSourceText); err != nil {
		t.Fatal(err)
	}
	sections, err := store.ListDynamicSections(ctx, meetingID)
	if err != nil {
		t.Fatal(err)
	}
	if len(sections) != 1 || len(sections[0].Items) != 1 || !strings.Contains(sections[0].Items[0].Content, "周五前到齐") {
		t.Fatalf("expected expanded dynamic item to replace short item, got %#v", sections)
	}
}

func TestCleanupMergesExistingDynamicSectionDuplicates(t *testing.T) {
	ctx := context.Background()
	store, meetingID := newInteractionStore(t, ctx)

	section, err := store.CreateDynamicSection(ctx, meetingID, "硬件设备", "", "记录硬件依赖", "", "test")
	if err != nil {
		t.Fatal(err)
	}
	now := nowString()
	if _, err := store.db.ExecContext(ctx, `INSERT INTO meeting_dynamic_section_items
		(id, meeting_id, section_id, title, content, fields_json, source_text, confidence, status, display_order, is_deleted, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, '', ?, 1, 'open', 1, 0, ?, ?),
			(?, ?, ?, ?, ?, '', ?, 1, 'open', 2, 0, ?, ?)`,
		newID("secitem"), meetingID, section.ID, "硬件设备", "硬件设备", dynamicSectionVoiceSourceText, now, now,
		newID("secitem"), meetingID, section.ID, "硬件设备周五前到齐", "硬件设备周五前到齐", dynamicSectionVoiceSourceText, now, now); err != nil {
		t.Fatal(err)
	}
	if err := store.CleanupIntelligenceDuplicates(ctx, meetingID); err != nil {
		t.Fatal(err)
	}
	sections, err := store.ListDynamicSections(ctx, meetingID)
	if err != nil {
		t.Fatal(err)
	}
	if len(sections) != 1 || len(sections[0].Items) != 1 || !strings.Contains(sections[0].Items[0].Content, "周五前到齐") {
		t.Fatalf("expected cleanup to merge existing dynamic duplicates, got %#v", sections)
	}
}

func TestInteractionCommandUpdatesFrameworkSections(t *testing.T) {
	if !meetingFrameworkFeatureEnabled() {
		t.Skip("meeting framework feature is temporarily disabled")
	}
	ctx := context.Background()
	store, meetingID := newInteractionStore(t, ctx)
	server := &Server{store: store, hub: NewHub()}

	if _, err := store.UpdateMeetingType(ctx, meetingID, "project_followup", true); err != nil {
		t.Fatal(err)
	}

	snapshot, message, err := server.executeInteractionCommand(ctx, meetingID, InteractionCommand{
		Intent:       "update_framework_section",
		TargetType:   "framework_section",
		SectionTitle: "当前进展",
		Content:      "实时转写已经稳定，结构化提取还在优化。",
		Instruction:  "小艾小艾，把当前进展改成实时转写已经稳定，结构化提取还在优化。",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(message, "已更新") {
		t.Fatalf("expected update message, got %q", message)
	}
	progress := findFrameworkSection(snapshot.FrameworkSections, "current_progress")
	if progress == nil || !strings.Contains(progress.Content, "实时转写") || !progress.Locked {
		t.Fatalf("expected current progress to be updated and locked, got %#v", progress)
	}

	snapshot, _, err = server.executeInteractionCommand(ctx, meetingID, InteractionCommand{
		Intent:      "append_framework_section",
		TargetType:  "framework_section",
		SectionKey:  "current_progress",
		Content:     "本周重点是验证会中交互链路。",
		Instruction: "小艾小艾，补充当前进展，本周重点是验证会中交互链路。",
	})
	if err != nil {
		t.Fatal(err)
	}
	progress = findFrameworkSection(snapshot.FrameworkSections, "current_progress")
	if progress == nil || !strings.Contains(progress.Content, "实时转写") || !strings.Contains(progress.Content, "会中交互") {
		t.Fatalf("expected current progress to include appended content, got %#v", progress)
	}

	snapshot, _, err = server.executeInteractionCommand(ctx, meetingID, InteractionCommand{
		Intent:       "clear_framework_section",
		TargetType:   "framework_section",
		SectionTitle: "当前进展",
		Instruction:  "小艾小艾，清空当前进展。",
	})
	if err != nil {
		t.Fatal(err)
	}
	progress = findFrameworkSection(snapshot.FrameworkSections, "current_progress")
	if progress == nil || progress.Content != "" || !progress.Locked {
		t.Fatalf("expected current progress to be cleared and locked, got %#v", progress)
	}

	snapshot, _, err = server.executeInteractionCommand(ctx, meetingID, InteractionCommand{
		Intent:     "undo_last_interaction",
		TargetType: "interaction",
	})
	if err != nil {
		t.Fatal(err)
	}
	progress = findFrameworkSection(snapshot.FrameworkSections, "current_progress")
	if progress == nil || !strings.Contains(progress.Content, "会中交互") {
		t.Fatalf("expected undo to restore appended current progress, got %#v", progress)
	}
}

func TestLocalFrameworkSectionParserCoversMeetingTypes(t *testing.T) {
	if !meetingFrameworkFeatureEnabled() {
		t.Skip("meeting framework feature is temporarily disabled")
	}
	ctx := context.Background()
	store, meetingID := newInteractionStore(t, ctx)
	server := &Server{store: store, hub: NewHub()}

	cases := []struct {
		meetingType   string
		instruction   string
		wantIntent    string
		wantKey       string
		wantContent   string
		wantNoContent bool
	}{
		{
			meetingType: "exploration",
			instruction: "把核心问题改成是否把会中交互作为正式版主卖点",
			wantIntent:  "update_framework_section",
			wantKey:     "core_question",
			wantContent: "是否把会中交互作为正式版主卖点",
		},
		{
			meetingType: "exploration",
			instruction: "补充支持理由，语音修改比手动编辑更适合会议现场",
			wantIntent:  "append_framework_section",
			wantKey:     "supporting_reasons",
			wantContent: "语音修改比手动编辑更适合会议现场",
		},
		{
			meetingType: "retrospective",
			instruction: "重写可沉淀经验为演示前要提前锁定网络、麦克风和模型配置",
			wantIntent:  "rewrite_framework_section",
			wantKey:     "lessons_learned",
			wantContent: "演示前要提前锁定网络、麦克风和模型配置",
		},
		{
			meetingType:   "retrospective",
			instruction:   "清空暴露的问题",
			wantIntent:    "clear_framework_section",
			wantKey:       "exposed_problems",
			wantNoContent: true,
		},
		{
			meetingType: "general",
			instruction: "未解决问题：二维码公网访问策略还没有定",
			wantIntent:  "update_framework_section",
			wantKey:     "unresolved_issues",
			wantContent: "二维码公网访问策略还没有定",
		},
	}

	for _, tc := range cases {
		if _, err := store.UpdateMeetingType(ctx, meetingID, tc.meetingType, true); err != nil {
			t.Fatal(err)
		}
		command, _, err := server.parseInteractionCommand(ctx, meetingID, tc.instruction, newInteractionTiming(interactionTimingSeed{}))
		if err != nil {
			t.Fatalf("%s parse failed: %v", tc.instruction, err)
		}
		if command.Intent != tc.wantIntent || command.TargetType != "framework_section" || command.SectionKey != tc.wantKey {
			t.Fatalf("unexpected command for %q: %#v", tc.instruction, command)
		}
		if tc.wantNoContent {
			if command.Content != "" {
				t.Fatalf("expected empty content for %q, got %q", tc.instruction, command.Content)
			}
			continue
		}
		if command.Content != tc.wantContent {
			t.Fatalf("unexpected content for %q: got %q want %q", tc.instruction, command.Content, tc.wantContent)
		}
	}
}

func TestInteractionCommandUpdatesNewMeetingTypeFrameworkSections(t *testing.T) {
	if !meetingFrameworkFeatureEnabled() {
		t.Skip("meeting framework feature is temporarily disabled")
	}
	ctx := context.Background()
	store, meetingID := newInteractionStore(t, ctx)
	server := &Server{store: store, hub: NewHub()}

	cases := []struct {
		meetingType string
		title       string
		content     string
		key         string
	}{
		{"exploration", "已提出想法", "先做固定命令，再把复杂请求交给工具编排。", "proposed_ideas"},
		{"retrospective", "改进方案", "演示前增加设备自检和配置校验。", "improvement_plan"},
		{"general", "关键观点", "会议助手要让老板看见系统正在工作。", "key_points"},
	}
	for _, tc := range cases {
		if _, err := store.UpdateMeetingType(ctx, meetingID, tc.meetingType, true); err != nil {
			t.Fatal(err)
		}
		snapshot, _, err := server.executeInteractionCommand(ctx, meetingID, InteractionCommand{
			Intent:       "update_framework_section",
			TargetType:   "framework_section",
			SectionTitle: tc.title,
			Content:      tc.content,
			Instruction:  "本地测试框架模块更新",
		})
		if err != nil {
			t.Fatalf("update %s %s failed: %v", tc.meetingType, tc.title, err)
		}
		section := findFrameworkSection(snapshot.FrameworkSections, tc.key)
		if section == nil || section.Content != tc.content || !section.Locked {
			t.Fatalf("expected updated locked framework section, got %#v", section)
		}
	}
}

func containsActionTask(items []MeetingActionItem, text string) bool {
	for _, item := range items {
		if strings.Contains(item.Task, text) {
			return true
		}
	}
	return false
}

func containsRiskContent(risks []MeetingRisk, text string) bool {
	for _, risk := range risks {
		if strings.Contains(risk.Content, text) {
			return true
		}
	}
	return false
}

func containsOpenQuestion(questions []MeetingOpenQuestion, text string) bool {
	for _, question := range questions {
		if strings.Contains(question.Question, text) {
			return true
		}
	}
	return false
}

func containsDynamicSectionTitle(sections []MeetingDynamicSection, title string) bool {
	for _, section := range sections {
		if section.Title == title {
			return true
		}
	}
	return false
}

func containsDynamicSectionID(sections []MeetingDynamicSection, id string) bool {
	for _, section := range sections {
		if section.ID == id {
			return true
		}
	}
	return false
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
		Summary: SummaryExtraction{Brief: "Initial summary."},
		ActionItems: []ActionItemExtraction{
			{Task: "Prepare meeting materials", Owner: "Zhang San", Status: "open"},
			{Task: "Book meeting room", Owner: "Li Si", Status: "open"},
			{Task: "Investigate ASR connection keepalive", Owner: "Wang Wu", Status: "open"},
		},
		Risks: []RiskExtraction{
			{Content: "Microphone stability may affect the demo", Impact: "Demo may be interrupted", Level: "medium", Status: "open"},
			{Content: "Venue network may be unstable", Impact: "Remote access may fail", Level: "high", Status: "open"},
		},
		OpenQuestions: []OpenQuestionExtraction{
			{Question: "Who owns the venue network check", Status: "open"},
			{Question: "Whether offline mode is needed for the demo", Status: "open"},
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
