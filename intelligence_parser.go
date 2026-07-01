// 本文件负责解析和规范化会议结构化 LLM 输出。
package main

import (
	"encoding/json"
	"fmt"
	"strings"
)

type flexibleStringList []string

func (list *flexibleStringList) UnmarshalJSON(data []byte) error {
	values, err := decodeFlexibleStringList(data)
	if err != nil {
		return err
	}
	*list = values
	return nil
}

func decodeFlexibleStringList(data []byte) ([]string, error) {
	text := strings.TrimSpace(string(data))
	if text == "" || text == "null" {
		return nil, nil
	}
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		return nil, err
	}
	return flexibleValueToStringList(value), nil
}

func flexibleValueToStringList(value any) []string {
	switch v := value.(type) {
	case nil:
		return nil
	case string:
		return splitFlexibleListText(v)
	case []any:
		values := []string{}
		for _, item := range v {
			values = append(values, flexibleValueToStringList(item)...)
		}
		return uniqueNonEmptyStrings(values)
	default:
		return uniqueNonEmptyStrings([]string{flexibleValueToString(value)})
	}
}

func flexibleValueToString(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(v)
	case map[string]any:
		for _, key := range []string{"content", "text", "title", "task", "question", "brief", "summary", "value", "source_text", "sourceText"} {
			if text := flexibleValueToString(v[key]); text != "" {
				return text
			}
		}
		bytes, err := json.Marshal(v)
		if err != nil {
			return ""
		}
		return strings.TrimSpace(string(bytes))
	case []any:
		return strings.Join(flexibleValueToStringList(v), "；")
	default:
		return strings.TrimSpace(fmt.Sprint(v))
	}
}

func splitFlexibleListText(value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	value = strings.ReplaceAll(value, "\r\n", "\n")
	value = strings.ReplaceAll(value, "\r", "\n")
	separators := []string{"\n", "；", ";"}
	for _, separator := range separators {
		if !strings.Contains(value, separator) {
			continue
		}
		parts := strings.Split(value, separator)
		values := make([]string, 0, len(parts))
		for _, part := range parts {
			part = trimFlexibleListItem(part)
			if part != "" {
				values = append(values, part)
			}
		}
		if len(values) > 0 {
			return uniqueNonEmptyStrings(values)
		}
	}
	return []string{trimFlexibleListItem(value)}
}

func trimFlexibleListItem(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimLeft(value, "-•* \t")
	for _, prefix := range []string{"1.", "2.", "3.", "4.", "5.", "6.", "7.", "8.", "9.", "1、", "2、", "3、", "4、", "5、", "6、", "7、", "8、", "9、"} {
		value = strings.TrimPrefix(value, prefix)
	}
	return strings.TrimSpace(value)
}

type decisionExtractionList []DecisionExtraction

func (list *decisionExtractionList) UnmarshalJSON(data []byte) error {
	text := strings.TrimSpace(string(data))
	if text == "" || text == "null" {
		return nil
	}
	var values []DecisionExtraction
	if err := json.Unmarshal(data, &values); err == nil {
		*list = values
		return nil
	}
	var value DecisionExtraction
	if err := json.Unmarshal(data, &value); err == nil {
		*list = []DecisionExtraction{value}
		return nil
	}
	var legacy string
	if err := json.Unmarshal(data, &legacy); err == nil {
		legacy = strings.TrimSpace(legacy)
		if legacy == "" {
			return nil
		}
		*list = []DecisionExtraction{{Content: legacy}}
		return nil
	}
	return fmt.Errorf("invalid decisions shape")
}

type actionItemExtractionList []ActionItemExtraction

func (list *actionItemExtractionList) UnmarshalJSON(data []byte) error {
	text := strings.TrimSpace(string(data))
	if text == "" || text == "null" {
		return nil
	}
	var values []ActionItemExtraction
	if err := json.Unmarshal(data, &values); err == nil {
		*list = values
		return nil
	}
	var value ActionItemExtraction
	if err := json.Unmarshal(data, &value); err == nil {
		*list = []ActionItemExtraction{value}
		return nil
	}
	var legacy string
	if err := json.Unmarshal(data, &legacy); err == nil {
		legacy = strings.TrimSpace(legacy)
		if legacy == "" {
			return nil
		}
		*list = []ActionItemExtraction{{Task: legacy}}
		return nil
	}
	return fmt.Errorf("invalid action_items shape")
}

type riskExtractionList []RiskExtraction

func (list *riskExtractionList) UnmarshalJSON(data []byte) error {
	text := strings.TrimSpace(string(data))
	if text == "" || text == "null" {
		return nil
	}
	var values []RiskExtraction
	if err := json.Unmarshal(data, &values); err == nil {
		*list = values
		return nil
	}
	var value RiskExtraction
	if err := json.Unmarshal(data, &value); err == nil {
		*list = []RiskExtraction{value}
		return nil
	}
	var legacy string
	if err := json.Unmarshal(data, &legacy); err == nil {
		legacy = strings.TrimSpace(legacy)
		if legacy == "" {
			return nil
		}
		*list = []RiskExtraction{{Content: legacy}}
		return nil
	}
	return fmt.Errorf("invalid risks shape")
}

type openQuestionExtractionList []OpenQuestionExtraction

func (list *openQuestionExtractionList) UnmarshalJSON(data []byte) error {
	text := strings.TrimSpace(string(data))
	if text == "" || text == "null" {
		return nil
	}
	var values []OpenQuestionExtraction
	if err := json.Unmarshal(data, &values); err == nil {
		*list = values
		return nil
	}
	var value OpenQuestionExtraction
	if err := json.Unmarshal(data, &value); err == nil {
		*list = []OpenQuestionExtraction{value}
		return nil
	}
	var legacy string
	if err := json.Unmarshal(data, &legacy); err == nil {
		legacy = strings.TrimSpace(legacy)
		if legacy == "" {
			return nil
		}
		*list = []OpenQuestionExtraction{{Question: legacy}}
		return nil
	}
	return fmt.Errorf("invalid open_questions shape")
}

type frameworkSectionExtractionList []FrameworkSectionExtraction

func (list *frameworkSectionExtractionList) UnmarshalJSON(data []byte) error {
	text := strings.TrimSpace(string(data))
	if text == "" || text == "null" {
		return nil
	}
	var values []FrameworkSectionExtraction
	if err := json.Unmarshal(data, &values); err == nil {
		*list = values
		return nil
	}
	var value FrameworkSectionExtraction
	if err := json.Unmarshal(data, &value); err == nil {
		*list = []FrameworkSectionExtraction{value}
		return nil
	}
	var legacy string
	if err := json.Unmarshal(data, &legacy); err == nil {
		legacy = strings.TrimSpace(legacy)
		if legacy == "" {
			return nil
		}
		*list = []FrameworkSectionExtraction{{Content: legacy}}
		return nil
	}
	return fmt.Errorf("invalid framework_sections shape")
}

type dynamicSectionExtractionList []DynamicSectionExtraction

func (list *dynamicSectionExtractionList) UnmarshalJSON(data []byte) error {
	text := strings.TrimSpace(string(data))
	if text == "" || text == "null" {
		return nil
	}
	var values []DynamicSectionExtraction
	if err := json.Unmarshal(data, &values); err == nil {
		*list = values
		return nil
	}
	var value DynamicSectionExtraction
	if err := json.Unmarshal(data, &value); err == nil {
		*list = []DynamicSectionExtraction{value}
		return nil
	}
	var legacy string
	if err := json.Unmarshal(data, &legacy); err == nil {
		legacy = strings.TrimSpace(legacy)
		if legacy == "" {
			return nil
		}
		*list = []DynamicSectionExtraction{{Title: legacy}}
		return nil
	}
	return fmt.Errorf("invalid dynamic_sections shape")
}

type dynamicSectionItemExtractionList []DynamicSectionItemExtraction

func (list *dynamicSectionItemExtractionList) UnmarshalJSON(data []byte) error {
	text := strings.TrimSpace(string(data))
	if text == "" || text == "null" {
		return nil
	}
	var values []DynamicSectionItemExtraction
	if err := json.Unmarshal(data, &values); err == nil {
		*list = values
		return nil
	}
	var value DynamicSectionItemExtraction
	if err := json.Unmarshal(data, &value); err == nil {
		*list = []DynamicSectionItemExtraction{value}
		return nil
	}
	var legacy string
	if err := json.Unmarshal(data, &legacy); err == nil {
		legacy = strings.TrimSpace(legacy)
		if legacy == "" {
			return nil
		}
		*list = []DynamicSectionItemExtraction{{Content: legacy}}
		return nil
	}
	return fmt.Errorf("invalid dynamic section items shape")
}

func (result *IntelligenceExtraction) UnmarshalJSON(data []byte) error {
	var raw struct {
		Title                  string                         `json:"title"`
		MeetingType            string                         `json:"meeting_type"`
		MeetingTypeCamel       string                         `json:"meetingType"`
		Summary                SummaryExtraction              `json:"summary"`
		Decisions              decisionExtractionList         `json:"decisions"`
		ActionItems            actionItemExtractionList       `json:"action_items"`
		ActionItemsCamel       actionItemExtractionList       `json:"actionItems"`
		Risks                  riskExtractionList             `json:"risks"`
		OpenQuestions          openQuestionExtractionList     `json:"open_questions"`
		OpenQuestionsCamel     openQuestionExtractionList     `json:"openQuestions"`
		FrameworkSections      frameworkSectionExtractionList `json:"framework_sections"`
		FrameworkSectionsCamel frameworkSectionExtractionList `json:"frameworkSections"`
		DynamicSections        dynamicSectionExtractionList   `json:"dynamic_sections"`
		DynamicSectionsCamel   dynamicSectionExtractionList   `json:"dynamicSections"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	result.Title = raw.Title
	result.MeetingType = raw.MeetingType
	if result.MeetingType == "" {
		result.MeetingType = raw.MeetingTypeCamel
	}
	result.Summary = raw.Summary
	result.Decisions = []DecisionExtraction(raw.Decisions)
	result.ActionItems = append([]ActionItemExtraction(raw.ActionItems), []ActionItemExtraction(raw.ActionItemsCamel)...)
	result.Risks = []RiskExtraction(raw.Risks)
	result.OpenQuestions = append([]OpenQuestionExtraction(raw.OpenQuestions), []OpenQuestionExtraction(raw.OpenQuestionsCamel)...)
	result.FrameworkSections = append([]FrameworkSectionExtraction(raw.FrameworkSections), []FrameworkSectionExtraction(raw.FrameworkSectionsCamel)...)
	result.DynamicSections = append([]DynamicSectionExtraction(raw.DynamicSections), []DynamicSectionExtraction(raw.DynamicSectionsCamel)...)
	return nil
}

func (section *DynamicSectionExtraction) UnmarshalJSON(data []byte) error {
	text := strings.TrimSpace(string(data))
	if text == "" || text == "null" {
		return nil
	}
	var legacy string
	if err := json.Unmarshal(data, &legacy); err == nil {
		section.Title = strings.TrimSpace(legacy)
		return nil
	}
	var raw struct {
		SectionKey      string                           `json:"section_key"`
		SectionKeyCamel string                           `json:"sectionKey"`
		Title           string                           `json:"title"`
		Description     string                           `json:"description"`
		Items           dynamicSectionItemExtractionList `json:"items"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	section.SectionKey = strings.TrimSpace(raw.SectionKey)
	if section.SectionKey == "" {
		section.SectionKey = strings.TrimSpace(raw.SectionKeyCamel)
	}
	section.Title = strings.TrimSpace(raw.Title)
	section.Description = strings.TrimSpace(raw.Description)
	section.Items = []DynamicSectionItemExtraction(raw.Items)
	return nil
}

func (item *DynamicSectionItemExtraction) UnmarshalJSON(data []byte) error {
	text := strings.TrimSpace(string(data))
	if text == "" || text == "null" {
		return nil
	}
	var legacy string
	if err := json.Unmarshal(data, &legacy); err == nil {
		item.Content = strings.TrimSpace(legacy)
		return nil
	}
	type rawItem DynamicSectionItemExtraction
	var raw rawItem
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*item = DynamicSectionItemExtraction(raw)
	return nil
}

func (summary *SummaryExtraction) UnmarshalJSON(data []byte) error {
	text := strings.TrimSpace(string(data))
	if text == "" || text == "null" {
		return nil
	}
	var legacy string
	if err := json.Unmarshal(data, &legacy); err == nil {
		summary.Brief = strings.TrimSpace(legacy)
		return nil
	}
	var raw struct {
		Brief             string             `json:"brief"`
		Content           string             `json:"content"`
		Background        string             `json:"background"`
		KeyPoints         flexibleStringList `json:"key_points"`
		KeyPointsCamel    flexibleStringList `json:"keyPoints"`
		Progress          string             `json:"progress"`
		Changes           flexibleStringList `json:"changes"`
		Conclusion        string             `json:"conclusion"`
		NextFollowUp      string             `json:"next_follow_up"`
		NextFollowUpCamel string             `json:"nextFollowUp"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	summary.Brief = strings.TrimSpace(raw.Brief)
	if summary.Brief == "" {
		summary.Brief = strings.TrimSpace(raw.Content)
	}
	summary.Background = strings.TrimSpace(raw.Background)
	summary.KeyPoints = uniqueNonEmptyStrings(append([]string(raw.KeyPoints), []string(raw.KeyPointsCamel)...))
	summary.Progress = strings.TrimSpace(raw.Progress)
	summary.Changes = uniqueNonEmptyStrings([]string(raw.Changes))
	summary.Conclusion = strings.TrimSpace(raw.Conclusion)
	summary.NextFollowUp = strings.TrimSpace(raw.NextFollowUp)
	if summary.NextFollowUp == "" {
		summary.NextFollowUp = strings.TrimSpace(raw.NextFollowUpCamel)
	}
	return nil
}

func (summary SummaryExtraction) IsEmpty() bool {
	return strings.TrimSpace(summary.Brief) == "" &&
		strings.TrimSpace(summary.Background) == "" &&
		len(uniqueNonEmptyStrings(summary.KeyPoints)) == 0 &&
		strings.TrimSpace(summary.Progress) == "" &&
		len(uniqueNonEmptyStrings(summary.Changes)) == 0 &&
		strings.TrimSpace(summary.Conclusion) == "" &&
		strings.TrimSpace(summary.NextFollowUp) == ""
}

func (summary SummaryExtraction) PlainText() string {
	parts := []string{}
	if text := strings.TrimSpace(summary.Brief); text != "" {
		parts = append(parts, text)
	}
	if text := strings.TrimSpace(summary.Background); text != "" {
		parts = append(parts, "背景："+text)
	}
	for _, point := range uniqueNonEmptyStrings(summary.KeyPoints) {
		parts = append(parts, "要点："+point)
	}
	if text := strings.TrimSpace(summary.Progress); text != "" {
		parts = append(parts, "进展："+text)
	}
	for _, change := range uniqueNonEmptyStrings(summary.Changes) {
		parts = append(parts, "变化："+change)
	}
	if text := strings.TrimSpace(summary.Conclusion); text != "" {
		parts = append(parts, "结论："+text)
	}
	if text := strings.TrimSpace(summary.NextFollowUp); text != "" {
		parts = append(parts, "下次跟进："+text)
	}
	return strings.Join(parts, "\n")
}

func normalizeSummaryExtraction(summary SummaryExtraction) SummaryExtraction {
	summary.Brief = limitSummaryText(summary.Brief, 180)
	summary.Background = limitSummaryText(summary.Background, 240)
	summary.KeyPoints = limitSummaryList(summary.KeyPoints, 8, 160)
	summary.Progress = limitSummaryText(summary.Progress, 240)
	summary.Changes = limitSummaryList(summary.Changes, 8, 160)
	summary.Conclusion = limitSummaryText(summary.Conclusion, 240)
	summary.NextFollowUp = limitSummaryText(summary.NextFollowUp, 180)
	if summary.Brief == "" {
		switch {
		case summary.Conclusion != "":
			summary.Brief = summary.Conclusion
		case summary.Progress != "":
			summary.Brief = summary.Progress
		case len(summary.KeyPoints) > 0:
			summary.Brief = summary.KeyPoints[0]
		}
	}
	return summary
}

func limitSummaryList(values []string, maxItems, maxRunes int) []string {
	values = uniqueNonEmptyStrings(values)
	if len(values) > maxItems {
		values = values[:maxItems]
	}
	for index := range values {
		values[index] = limitSummaryText(values[index], maxRunes)
	}
	return uniqueNonEmptyStrings(values)
}

func limitSummaryText(value string, maxRunes int) string {
	value = strings.TrimSpace(value)
	if maxRunes <= 0 {
		return value
	}
	runes := []rune(value)
	if len(runes) <= maxRunes {
		return value
	}
	return strings.TrimSpace(string(runes[:maxRunes]))
}

func parseIntelligenceExtraction(content string) (*IntelligenceExtraction, error) {
	jsonText := extractJSONObject(content)
	var result IntelligenceExtraction
	if err := json.Unmarshal([]byte(jsonText), &result); err != nil {
		return nil, err
	}
	var alt struct {
		ActionItems       []ActionItemExtraction       `json:"actionItems"`
		MeetingType       string                       `json:"meetingType"`
		OpenQuestions     []OpenQuestionExtraction     `json:"openQuestions"`
		FrameworkSections []FrameworkSectionExtraction `json:"frameworkSections"`
		DynamicSections   []DynamicSectionExtraction   `json:"dynamicSections"`
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
		if len(result.FrameworkSections) == 0 {
			result.FrameworkSections = alt.FrameworkSections
		}
		if len(result.DynamicSections) == 0 {
			result.DynamicSections = alt.DynamicSections
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
	result.Summary = normalizeSummaryExtraction(result.Summary)
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
	normalizedFramework := make([]FrameworkSectionExtraction, 0, len(result.FrameworkSections))
	for _, section := range result.FrameworkSections {
		section.SectionKey = strings.TrimSpace(section.SectionKey)
		section.Title = strings.TrimSpace(section.Title)
		section.Content = strings.TrimSpace(section.Content)
		section.SourceText = strings.TrimSpace(section.SourceText)
		if section.SectionKey == "" && section.Title == "" {
			continue
		}
		if section.Content == "" && section.SourceText == "" {
			continue
		}
		normalizedFramework = append(normalizedFramework, section)
	}
	result.FrameworkSections = normalizedFramework
	for i := range result.DynamicSections {
		result.DynamicSections[i].SectionKey = strings.TrimSpace(result.DynamicSections[i].SectionKey)
		result.DynamicSections[i].Title = strings.TrimSpace(result.DynamicSections[i].Title)
		result.DynamicSections[i].Description = strings.TrimSpace(result.DynamicSections[i].Description)
		normalizedItems := make([]DynamicSectionItemExtraction, 0, len(result.DynamicSections[i].Items))
		for _, item := range result.DynamicSections[i].Items {
			item.Title = strings.TrimSpace(item.Title)
			item.Content = strings.TrimSpace(item.Content)
			item.SourceText = strings.TrimSpace(item.SourceText)
			if item.Title == "" && item.Content == "" {
				continue
			}
			normalizedItems = append(normalizedItems, item)
		}
		result.DynamicSections[i].Items = normalizedItems
	}
}
