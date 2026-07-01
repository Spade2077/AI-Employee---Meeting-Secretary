package main

import (
	"strings"
	"testing"
)

func TestParseIntelligenceExtractionAcceptsLooseLLMShapes(t *testing.T) {
	content := `{
		"title": "客户现场演示准备会",
		"meetingType": "project_followup",
		"summary": {
			"brief": "会议确认演示准备重点。",
			"key_points": "先保障云端链路；突出会中交互",
			"changes": "本地 ASR 不在演示前切换",
			"nextFollowUp": "周三下午同步检查结果"
		},
		"decisions": {
			"content": "下周演示版本先不切本地 ASR。",
			"source_text": "第一版还是优先使用云端 ASR。"
		},
		"action_items": {
			"task": "确认客户现场网络能否访问公网",
			"owner": "王磊",
			"due_date": "周三前",
			"status": "open"
		},
		"risks": "客户现场公网受限可能影响云端 ASR 和 LLM 调用",
		"openQuestions": {
			"question": "客户现场是否允许外接热点",
			"owner": "王磊"
		},
		"dynamicSections": {
			"title": "预算约束",
			"description": "记录临时采购和审批限制",
			"items": "临时设备采购费用控制在三千以内"
		}
	}`

	result, err := parseIntelligenceExtraction(content)
	if err != nil {
		t.Fatal(err)
	}
	if result.Title != "客户现场演示准备会" || result.MeetingType != "project_followup" {
		t.Fatalf("unexpected meeting metadata: %#v", result)
	}
	if len(result.Summary.KeyPoints) != 2 || !strings.Contains(result.Summary.KeyPoints[0], "云端链路") {
		t.Fatalf("expected key_points string to become list, got %#v", result.Summary.KeyPoints)
	}
	if len(result.Summary.Changes) != 1 || !strings.Contains(result.Summary.Changes[0], "本地 ASR") {
		t.Fatalf("expected changes string to become list, got %#v", result.Summary.Changes)
	}
	if len(result.Decisions) != 1 || !strings.Contains(result.Decisions[0].Content, "本地 ASR") {
		t.Fatalf("expected single decision object to become list, got %#v", result.Decisions)
	}
	if len(result.ActionItems) != 1 || result.ActionItems[0].Owner != "王磊" {
		t.Fatalf("expected single action item object to become list, got %#v", result.ActionItems)
	}
	if len(result.Risks) != 1 || !strings.Contains(result.Risks[0].Content, "公网受限") {
		t.Fatalf("expected risk string to become one risk, got %#v", result.Risks)
	}
	if len(result.OpenQuestions) != 1 || !strings.Contains(result.OpenQuestions[0].Question, "外接热点") {
		t.Fatalf("expected camel openQuestions object to become list, got %#v", result.OpenQuestions)
	}
	if len(result.DynamicSections) != 1 ||
		len(result.DynamicSections[0].Items) != 1 ||
		!strings.Contains(result.DynamicSections[0].Items[0].Content, "三千以内") {
		t.Fatalf("expected dynamic section object and item string to normalize, got %#v", result.DynamicSections)
	}
}
