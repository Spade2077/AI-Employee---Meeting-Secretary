// 本文件负责解析和规范化会议结构化 LLM 输出。
package main

import (
	"encoding/json"
	"strings"
)

func parseIntelligenceExtraction(content string) (*IntelligenceExtraction, error) {
	jsonText := extractJSONObject(content)
	var result IntelligenceExtraction
	if err := json.Unmarshal([]byte(jsonText), &result); err != nil {
		return nil, err
	}
	var alt struct {
		ActionItems   []ActionItemExtraction   `json:"actionItems"`
		MeetingType   string                   `json:"meetingType"`
		OpenQuestions []OpenQuestionExtraction `json:"openQuestions"`
	}
	if err := json.Unmarshal([]byte(jsonText), &alt); err == nil {
		if len(result.ActionItems) == 0 && len(alt.ActionItems) > 0 {
			result.ActionItems = alt.ActionItems
		}
		if result.MeetingType == "" {
			result.MeetingType = alt.MeetingType
		}
		if len(result.OpenQuestions) == 0 {
			result.OpenQuestions = alt.OpenQuestions
		}
	}
	normalizeIntelligenceExtraction(&result)
	return &result, nil
}

func normalizeIntelligenceExtraction(result *IntelligenceExtraction) {
	if result == nil {
		return
	}
	result.Title = strings.TrimSpace(result.Title)
	result.MeetingType = normalizeMeetingType(result.MeetingType)
	for i := range result.ActionItems {
		result.ActionItems[i].Task = strings.TrimSpace(result.ActionItems[i].Task)
		result.ActionItems[i].Owner = strings.TrimSpace(result.ActionItems[i].Owner)
		result.ActionItems[i].DueDate = strings.TrimSpace(result.ActionItems[i].DueDate)
		result.ActionItems[i].SourceText = strings.TrimSpace(result.ActionItems[i].SourceText)
		if strings.TrimSpace(result.ActionItems[i].Status) == "" {
			result.ActionItems[i].Status = "open"
		}
		result.ActionItems[i].Status = normalizeActionItemStatus(result.ActionItems[i].Status)
	}
	for i := range result.Risks {
		result.Risks[i].Content = strings.TrimSpace(result.Risks[i].Content)
		result.Risks[i].Impact = strings.TrimSpace(result.Risks[i].Impact)
		result.Risks[i].Level = normalizeRiskLevel(result.Risks[i].Level)
		result.Risks[i].Owner = strings.TrimSpace(result.Risks[i].Owner)
		result.Risks[i].Mitigation = strings.TrimSpace(result.Risks[i].Mitigation)
		result.Risks[i].DueDate = strings.TrimSpace(result.Risks[i].DueDate)
		result.Risks[i].Status = normalizeStructuredStatus(result.Risks[i].Status)
		result.Risks[i].SourceText = strings.TrimSpace(result.Risks[i].SourceText)
	}
	for i := range result.OpenQuestions {
		result.OpenQuestions[i].Question = strings.TrimSpace(result.OpenQuestions[i].Question)
		result.OpenQuestions[i].Owner = strings.TrimSpace(result.OpenQuestions[i].Owner)
		result.OpenQuestions[i].DueDate = strings.TrimSpace(result.OpenQuestions[i].DueDate)
		result.OpenQuestions[i].Status = normalizeStructuredStatus(result.OpenQuestions[i].Status)
		result.OpenQuestions[i].SourceText = strings.TrimSpace(result.OpenQuestions[i].SourceText)
	}
}
