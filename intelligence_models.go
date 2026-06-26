// 本文件负责会议结构化分析的数据结构定义。
package main

type IntelligenceSnapshot struct {
	Type        string              `json:"type"`
	MeetingID   string              `json:"meetingId"`
	Summary     *MeetingSummary     `json:"summary,omitempty"`
	Decisions   []MeetingDecision   `json:"decisions"`
	ActionItems []MeetingActionItem `json:"actionItems"`
	Timestamp   string              `json:"timestamp"`
}

type IntelligenceExtraction struct {
	Summary     string                 `json:"summary"`
	Decisions   []DecisionExtraction   `json:"decisions"`
	ActionItems []ActionItemExtraction `json:"action_items"`
}

type DecisionExtraction struct {
	Content    string `json:"content"`
	SourceText string `json:"source_text"`
}

type ActionItemExtraction struct {
	Task       string `json:"task"`
	Owner      string `json:"owner"`
	DueDate    string `json:"due_date"`
	Status     string `json:"status"`
	SourceText string `json:"source_text"`
}
