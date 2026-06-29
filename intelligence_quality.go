// 本文件负责结构化会议数据的字段规范化、缺失项识别和质量提醒生成。
package main

import (
	"encoding/json"
	"strings"
)

func normalizeMeetingType(value string) string {
	key := strings.ToLower(strings.TrimSpace(value))
	switch key {
	case "project_followup", "project", "项目推进会", "推进会":
		return "project_followup"
	case "exploration", "explore", "探索讨论会", "讨论会":
		return "exploration"
	case "retrospective", "review", "复盘总结会", "复盘会":
		return "retrospective"
	case "general", "normal", "普通会议", "例会":
		return "general"
	default:
		return "unknown"
	}
}

func shouldUpdateMeetingTitle(existing, incoming string) bool {
	existing = strings.TrimSpace(existing)
	incoming = strings.TrimSpace(incoming)
	if incoming == "" {
		return false
	}
	if existing == "" || existing == "未命名会议" || existing == "调试会议" {
		return true
	}
	return len([]rune(incoming)) >= 4 && len([]rune(existing)) < 8
}

func normalizeMeetingTitleInput(value string) string {
	value = strings.TrimSpace(value)
	value = strings.Trim(value, "“”\"'`《》")
	value = strings.ReplaceAll(value, "\r", " ")
	value = strings.ReplaceAll(value, "\n", " ")
	value = strings.Join(strings.Fields(value), " ")
	runes := []rune(value)
	if len(runes) > 40 {
		value = string(runes[:40])
	}
	return strings.TrimSpace(value)
}

func normalizeStructuredStatus(value string) string {
	key := strings.ToLower(strings.TrimSpace(value))
	switch key {
	case "", "open", "todo", "pending", "pending_info":
		return "open"
	case "done", "complete", "completed", "closed", "resolved", "answered":
		return "completed"
	case "blocked":
		return "blocked"
	default:
		return strings.TrimSpace(value)
	}
}

func normalizeRiskLevel(value string) string {
	key := strings.ToLower(strings.TrimSpace(value))
	switch key {
	case "high", "高", "严重":
		return "high"
	case "low", "低", "轻微":
		return "low"
	default:
		return "medium"
	}
}

func preferRiskLevel(existing, incoming string) string {
	existing = normalizeRiskLevel(existing)
	incoming = normalizeRiskLevel(incoming)
	if riskLevelRank(incoming) > riskLevelRank(existing) {
		return incoming
	}
	return existing
}

func riskLevelRank(value string) int {
	switch normalizeRiskLevel(value) {
	case "high":
		return 3
	case "medium":
		return 2
	case "low":
		return 1
	default:
		return 0
	}
}

func isRevisionSourceText(sourceText string) bool {
	text := strings.TrimSpace(sourceText)
	if text == "" {
		return false
	}
	triggers := []string{
		"不行", "不对", "不是", "否掉", "取消",
		"改成", "改为", "改到", "调整为", "换成",
		"延到", "延期到", "推迟到", "提前到",
		"那就", "就按", "还是", "最终", "最后",
		"确定", "确认", "统一", "为准",
	}
	for _, trigger := range triggers {
		if strings.Contains(text, trigger) {
			return true
		}
	}
	return false
}

func missingFieldsForActionItem(item *MeetingActionItem) []string {
	if item == nil {
		return nil
	}
	fields := []string{}
	if strings.TrimSpace(item.Owner) == "" {
		fields = append(fields, "owner")
	}
	if strings.TrimSpace(item.DueDate) == "" {
		fields = append(fields, "due_date")
	}
	return fields
}

func missingFieldsForRisk(risk *MeetingRisk) []string {
	if risk == nil {
		return nil
	}
	fields := []string{}
	if strings.TrimSpace(risk.Owner) == "" {
		fields = append(fields, "owner")
	}
	if strings.TrimSpace(risk.Mitigation) == "" {
		fields = append(fields, "mitigation")
	}
	if strings.TrimSpace(risk.DueDate) == "" {
		fields = append(fields, "due_date")
	}
	return fields
}

func missingFieldsForOpenQuestion(question *MeetingOpenQuestion) []string {
	if question == nil {
		return nil
	}
	fields := []string{}
	if strings.TrimSpace(question.Owner) == "" {
		fields = append(fields, "owner")
	}
	if strings.TrimSpace(question.DueDate) == "" {
		fields = append(fields, "due_date")
	}
	return fields
}

func computeMissingFieldAlerts(actionItems []MeetingActionItem, risks []MeetingRisk, questions []MeetingOpenQuestion) []MissingFieldAlert {
	alerts := []MissingFieldAlert{}
	for _, item := range actionItems {
		for _, field := range missingFieldsForActionItem(&item) {
			alerts = append(alerts, MissingFieldAlert{
				TargetType:  "action_item",
				TargetID:    item.ID,
				TargetTitle: item.Task,
				Field:       field,
				Message:     "待办「" + item.Task + "」缺" + missingFieldLabel(field),
				Level:       "warning",
			})
		}
	}
	for _, risk := range risks {
		for _, field := range missingFieldsForRisk(&risk) {
			alerts = append(alerts, MissingFieldAlert{
				TargetType:  "risk",
				TargetID:    risk.ID,
				TargetTitle: risk.Content,
				Field:       field,
				Message:     riskMissingFieldMessage(risk, field),
				Level:       risk.Level,
			})
		}
	}
	for _, question := range questions {
		for _, field := range missingFieldsForOpenQuestion(&question) {
			alerts = append(alerts, MissingFieldAlert{
				TargetType:  "open_question",
				TargetID:    question.ID,
				TargetTitle: question.Question,
				Field:       field,
				Message:     "待确认问题「" + question.Question + "」缺" + missingFieldLabel(field),
				Level:       "info",
			})
		}
	}
	return alerts
}

func riskMissingFieldMessage(risk MeetingRisk, field string) string {
	if field == "mitigation" && normalizeStructuredStatus(risk.Status) == "open" {
		return "风险「" + risk.Content + "」未闭环：缺" + missingFieldLabel(field)
	}
	return "风险「" + risk.Content + "」缺" + missingFieldLabel(field)
}

func missingFieldLabel(field string) string {
	switch field {
	case "owner":
		return "负责人"
	case "due_date":
		return "截止时间"
	case "mitigation":
		return "处理方案"
	default:
		return field
	}
}

func encodeStringList(values []string) string {
	values = uniqueNonEmptyStrings(values)
	if len(values) == 0 {
		return ""
	}
	bytes, err := json.Marshal(values)
	if err != nil {
		return ""
	}
	return string(bytes)
}

func matchingRiskIndex(existing []MeetingRisk, content string) int {
	for index, risk := range existing {
		if similarStructuredText(risk.Content, content) {
			return index
		}
	}
	return -1
}

func matchingOpenQuestionIndex(existing []MeetingOpenQuestion, question string) int {
	for index, item := range existing {
		if similarStructuredText(item.Question, question) {
			return index
		}
	}
	return -1
}
