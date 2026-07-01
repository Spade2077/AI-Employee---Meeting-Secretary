// 本文件负责会议结构化分析的数据结构定义。
package main

type IntelligenceSnapshot struct {
	Type               string                    `json:"type"`
	MeetingID          string                    `json:"meetingId"`
	Title              string                    `json:"title"`
	MeetingType        string                    `json:"meetingType"`
	Summary            *MeetingSummary           `json:"summary,omitempty"`
	Decisions          []MeetingDecision         `json:"decisions"`
	ActionItems        []MeetingActionItem       `json:"actionItems"`
	Risks              []MeetingRisk             `json:"risks"`
	OpenQuestions      []MeetingOpenQuestion     `json:"openQuestions"`
	FrameworkSections  []MeetingFrameworkSection `json:"frameworkSections"`
	DynamicSections    []MeetingDynamicSection   `json:"dynamicSections"`
	MissingFieldAlerts []MissingFieldAlert       `json:"missingFieldAlerts"`
	Timestamp          string                    `json:"timestamp"`
}

type IntelligenceExtraction struct {
	Title             string                       `json:"title"`
	MeetingType       string                       `json:"meeting_type"`
	Summary           SummaryExtraction            `json:"summary"`
	Decisions         []DecisionExtraction         `json:"decisions"`
	ActionItems       []ActionItemExtraction       `json:"action_items"`
	Risks             []RiskExtraction             `json:"risks"`
	OpenQuestions     []OpenQuestionExtraction     `json:"open_questions"`
	FrameworkSections []FrameworkSectionExtraction `json:"framework_sections"`
	DynamicSections   []DynamicSectionExtraction   `json:"dynamic_sections"`
}

type SummaryExtraction struct {
	Brief        string   `json:"brief"`
	Background   string   `json:"background"`
	KeyPoints    []string `json:"key_points"`
	Progress     string   `json:"progress"`
	Changes      []string `json:"changes"`
	Conclusion   string   `json:"conclusion"`
	NextFollowUp string   `json:"next_follow_up"`
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

type FrameworkSectionExtraction struct {
	SectionKey string `json:"section_key"`
	Title      string `json:"title"`
	Content    string `json:"content"`
	SourceText string `json:"source_text"`
}

type MeetingFrameworkSection struct {
	ID           string `json:"id"`
	MeetingID    string `json:"meetingId"`
	MeetingType  string `json:"meetingType"`
	SectionKey   string `json:"sectionKey"`
	Title        string `json:"title"`
	Content      string `json:"content"`
	SourceText   string `json:"sourceText,omitempty"`
	Source       string `json:"source"`
	Locked       bool   `json:"locked"`
	DisplayOrder int    `json:"displayOrder"`
	IsDeleted    bool   `json:"isDeleted"`
	CreatedAt    string `json:"createdAt,omitempty"`
	UpdatedAt    string `json:"updatedAt,omitempty"`
}

type DynamicSectionExtraction struct {
	SectionKey  string                         `json:"section_key"`
	Title       string                         `json:"title"`
	Description string                         `json:"description"`
	Items       []DynamicSectionItemExtraction `json:"items"`
}

type DynamicSectionItemExtraction struct {
	Title      string `json:"title"`
	Content    string `json:"content"`
	SourceText string `json:"source_text"`
}

type MeetingDynamicSection struct {
	ID           string                      `json:"id"`
	MeetingID    string                      `json:"meetingId"`
	SectionKey   string                      `json:"sectionKey"`
	Title        string                      `json:"title"`
	Description  string                      `json:"description"`
	Source       string                      `json:"source"`
	Visibility   string                      `json:"visibility"`
	Confidence   float64                     `json:"confidence"`
	DisplayOrder int                         `json:"displayOrder"`
	IsDeleted    bool                        `json:"isDeleted"`
	CreatedAt    string                      `json:"createdAt"`
	UpdatedAt    string                      `json:"updatedAt"`
	Items        []MeetingDynamicSectionItem `json:"items"`
}

type MeetingDynamicSectionItem struct {
	ID           string  `json:"id"`
	MeetingID    string  `json:"meetingId"`
	SectionID    string  `json:"sectionId"`
	Title        string  `json:"title"`
	Content      string  `json:"content"`
	FieldsJSON   string  `json:"fieldsJson"`
	SourceText   string  `json:"sourceText"`
	Confidence   float64 `json:"confidence"`
	Status       string  `json:"status"`
	DisplayOrder int     `json:"displayOrder"`
	IsDeleted    bool    `json:"isDeleted"`
	CreatedAt    string  `json:"createdAt"`
	UpdatedAt    string  `json:"updatedAt"`
}
