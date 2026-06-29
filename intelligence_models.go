// 本文件负责会议结构化分析的数据结构定义。
package main

type IntelligenceSnapshot struct {
	Type               string                `json:"type"`
	MeetingID          string                `json:"meetingId"`
	Title              string                `json:"title"`
	MeetingType        string                `json:"meetingType"`
	Summary            *MeetingSummary       `json:"summary,omitempty"`
	Decisions          []MeetingDecision     `json:"decisions"`
	ActionItems        []MeetingActionItem   `json:"actionItems"`
	Risks              []MeetingRisk         `json:"risks"`
	OpenQuestions      []MeetingOpenQuestion `json:"openQuestions"`
	MissingFieldAlerts []MissingFieldAlert   `json:"missingFieldAlerts"`
	Timestamp          string                `json:"timestamp"`
}

type IntelligenceExtraction struct {
	Title         string                   `json:"title"`
	MeetingType   string                   `json:"meeting_type"`
	Summary       string                   `json:"summary"`
	Decisions     []DecisionExtraction     `json:"decisions"`
	ActionItems   []ActionItemExtraction   `json:"action_items"`
	Risks         []RiskExtraction         `json:"risks"`
	OpenQuestions []OpenQuestionExtraction `json:"open_questions"`
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

type RiskExtraction struct {
	Content    string `json:"content"`
	Impact     string `json:"impact"`
	Level      string `json:"level"`
	Owner      string `json:"owner"`
	Mitigation string `json:"mitigation"`
	DueDate    string `json:"due_date"`
	Status     string `json:"status"`
	SourceText string `json:"source_text"`
}

type OpenQuestionExtraction struct {
	Question   string `json:"question"`
	Owner      string `json:"owner"`
	DueDate    string `json:"due_date"`
	Status     string `json:"status"`
	SourceText string `json:"source_text"`
}
