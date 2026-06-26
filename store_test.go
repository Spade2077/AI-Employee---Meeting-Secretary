// 本文件负责验证本地存储、去重合并和转写保存行为。
package main

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"meeting-core-service/internal/stt"

	_ "modernc.org/sqlite"
)

func TestSaveIntelligenceExtractionMergesNearDuplicateActionItems(t *testing.T) {
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
	meeting, _, err := store.CreateMeeting(ctx, "dedupe", "test", "test")
	if err != nil {
		t.Fatal(err)
	}

	_, err = store.SaveIntelligenceExtraction(ctx, meeting.ID, IntelligenceExtraction{
		Summary: "讨论火山 ASR 连接保持。",
		ActionItems: []ActionItemExtraction{{
			Task:   "继续排查Volcano ASR连接保持逻辑，重点关注是否应该在暂停期间保持连接以及是否需要等待",
			Owner:  "王磊",
			Status: "open",
		}},
	}, 1)
	if err != nil {
		t.Fatal(err)
	}

	snapshot, err := store.SaveIntelligenceExtraction(ctx, meeting.ID, IntelligenceExtraction{
		Summary: "继续讨论火山 ASR 连接保持。",
		ActionItems: []ActionItemExtraction{{
			Task:    "排查Volcano ASR连接保持逻辑，重点关注是否应该在暂停期间保持连接以及是否需要等待会议结束时的服务器正常关闭，并在本周五前给出结论。",
			Owner:   "王磊",
			DueDate: "本周五前",
			Status:  "open",
		}},
	}, 2)
	if err != nil {
		t.Fatal(err)
	}

	if len(snapshot.ActionItems) != 1 {
		t.Fatalf("expected 1 merged action item, got %d: %#v", len(snapshot.ActionItems), snapshot.ActionItems)
	}
	item := snapshot.ActionItems[0]
	if !strings.Contains(item.Task, "服务器正常关闭") {
		t.Fatalf("expected richer task to be retained, got %q", item.Task)
	}
	if item.Owner != "王磊" {
		t.Fatalf("expected owner to stay filled, got %q", item.Owner)
	}
	if item.DueDate != "本周五前" {
		t.Fatalf("expected due date to be filled, got %q", item.DueDate)
	}
}

func TestAddTranscriptMergesNearbySimilarFinalResults(t *testing.T) {
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
	meeting, _, err := store.CreateMeeting(ctx, "transcript-dedupe", "test", "test")
	if err != nil {
		t.Fatal(err)
	}

	first, err := store.AddTranscript(ctx, stt.TranscriptResult{
		MeetingID:    meeting.ID,
		SpeakerLabel: "unknown",
		Text:         "继续排查Volcano ASR连接保持逻辑，重点关注是否应该在暂停期间保持连接以及是否需要等待",
		IsFinal:      true,
		Provider:     "volcano",
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.AddTranscript(ctx, stt.TranscriptResult{
		MeetingID:    meeting.ID,
		SpeakerLabel: "unknown",
		Text:         "排查Volcano ASR连接保持逻辑，重点关注是否应该在暂停期间保持连接以及是否需要等待会议结束时的服务器正常关闭，并在本周五前给出结论。",
		IsFinal:      true,
		Provider:     "volcano",
	})
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID {
		t.Fatalf("expected similar ASR revisions to update the same utterance, got %s and %s", first.ID, second.ID)
	}

	utterances, err := store.ListUtterances(ctx, meeting.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(utterances) != 1 {
		t.Fatalf("expected 1 merged utterance, got %d: %#v", len(utterances), utterances)
	}
	if !strings.Contains(utterances[0].Text, "服务器正常关闭") {
		t.Fatalf("expected richer transcript to be retained, got %q", utterances[0].Text)
	}
}
