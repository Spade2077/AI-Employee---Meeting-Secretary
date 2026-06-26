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
	if len(result.ActionItems) == 0 {
		var alt struct {
			ActionItems []ActionItemExtraction `json:"actionItems"`
		}
		if err := json.Unmarshal([]byte(jsonText), &alt); err == nil && len(alt.ActionItems) > 0 {
			result.ActionItems = alt.ActionItems
		}
	}
	normalizeIntelligenceExtraction(&result)
	return &result, nil
}

func normalizeIntelligenceExtraction(result *IntelligenceExtraction) {
	if result == nil {
		return
	}
	for i := range result.ActionItems {
		if strings.TrimSpace(result.ActionItems[i].Status) == "" {
			result.ActionItems[i].Status = "open"
		}
	}
}
