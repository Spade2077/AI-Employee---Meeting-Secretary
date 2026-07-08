package main

import "testing"

func TestActionItemInferenceFillsEmbeddedOwnerAndDueDate(t *testing.T) {
	item := &MeetingActionItem{
		Task:   "张三下周五前完成测试联调",
		Status: "open",
	}

	completeActionItemFieldsFromText(item)

	if item.Owner != "张三" {
		t.Fatalf("expected owner 张三, got %q", item.Owner)
	}
	if item.DueDate != "下周五前" {
		t.Fatalf("expected due date 下周五前, got %q", item.DueDate)
	}
	if missing := missingFieldsForActionItem(item); len(missing) != 0 {
		t.Fatalf("expected no missing fields after inference, got %#v", missing)
	}
}

func TestActionItemInferenceDoesNotInventOwnerFromDueDate(t *testing.T) {
	item := &MeetingActionItem{
		Task:   "下周五前完成测试联调",
		Status: "open",
	}

	completeActionItemFieldsFromText(item)

	if item.Owner != "" {
		t.Fatalf("expected owner to stay empty, got %q", item.Owner)
	}
	if item.DueDate != "下周五前" {
		t.Fatalf("expected due date 下周五前, got %q", item.DueDate)
	}
}

func TestActionItemInferenceAllowsZhouSurname(t *testing.T) {
	item := &MeetingActionItem{
		Task:   "周明下周五前完成测试联调",
		Status: "open",
	}

	completeActionItemFieldsFromText(item)

	if item.Owner != "周明" {
		t.Fatalf("expected owner 周明, got %q", item.Owner)
	}
	if item.DueDate != "下周五前" {
		t.Fatalf("expected due date 下周五前, got %q", item.DueDate)
	}
}

func TestParseIntelligenceExtractionFillsEmbeddedActionItemFields(t *testing.T) {
	result, err := parseIntelligenceExtraction(`{
		"title": "",
		"meeting_type": "general",
		"summary": {},
		"decisions": [],
		"action_items": [{
			"task": "张三下周五前完成测试联调",
			"owner": "",
			"due_date": "",
			"status": "open",
			"source_text": "张三下周五前完成测试联调"
		}],
		"risks": [],
		"open_questions": [],
		"framework_sections": [],
		"dynamic_sections": []
	}`)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ActionItems) != 1 {
		t.Fatalf("expected one action item, got %#v", result.ActionItems)
	}
	item := result.ActionItems[0]
	if item.Owner != "张三" || item.DueDate != "下周五前" {
		t.Fatalf("expected owner/due date to be inferred, got %#v", item)
	}
}
