// 本文件负责实现第一批 Agent 工具的后端处理函数。
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

const dynamicSectionVoiceSourceText = "后期语音输入"

func (s *Server) toolMeetingGetSnapshot(ctx context.Context, input ToolInput) (*ToolResult, error) {
	snapshot, err := s.store.GetIntelligenceSnapshot(ctx, input.MeetingID)
	if err != nil {
		return nil, err
	}
	return &ToolResult{
		ToolName: ToolMeetingGetSnapshot,
		Success:  true,
		Message:  "已读取当前会议结构化快照。",
		Data:     snapshot,
		After:    snapshot,
	}, nil
}

func (s *Server) toolMissingAlertList(ctx context.Context, input ToolInput) (*ToolResult, error) {
	snapshot, err := s.store.GetIntelligenceSnapshot(ctx, input.MeetingID)
	if err != nil {
		return nil, err
	}
	targetType := stringToolArg(input.Args, "target_type")
	field := stringToolArg(input.Args, "field")
	alerts := make([]MissingFieldAlert, 0, len(snapshot.MissingFieldAlerts))
	for _, alert := range snapshot.MissingFieldAlerts {
		if targetType != "" && alert.TargetType != targetType {
			continue
		}
		if field != "" && alert.Field != field {
			continue
		}
		alerts = append(alerts, alert)
	}
	return &ToolResult{
		ToolName: ToolMissingAlertList,
		Success:  true,
		Message:  fmt.Sprintf("已读取 %d 条缺失项提醒。", len(alerts)),
		Data:     alerts,
		After:    alerts,
	}, nil
}

func (s *Server) toolMeetingUpdateTitle(ctx context.Context, input ToolInput) (*ToolResult, error) {
	title := normalizeMeetingTitleInput(stringToolArg(input.Args, "title"))
	if title == "" {
		return nil, fmt.Errorf("meeting title is required")
	}
	before, err := s.store.GetMeeting(ctx, input.MeetingID)
	if err != nil {
		return nil, err
	}
	after, err := s.store.UpdateMeetingTitle(ctx, input.MeetingID, title, true)
	if err != nil {
		return nil, err
	}
	return &ToolResult{
		ToolName: ToolMeetingUpdateTitle,
		Success:  true,
		Message:  fmt.Sprintf("已将会议标题修改为：%s", after.Title),
		Before:   before,
		After:    after,
		Undo: &interactionUndoPayload{
			Type:    "restore_meeting_title",
			Meeting: before,
		},
	}, nil
}

func (s *Server) toolDynamicSectionCreate(ctx context.Context, input ToolInput) (*ToolResult, error) {
	title := stringToolArg(input.Args, "title")
	if title == "" {
		return nil, fmt.Errorf("dynamic section title is required")
	}
	sourceText := dynamicSectionSourceText(input)
	section, err := s.store.CreateDynamicSection(
		ctx,
		input.MeetingID,
		title,
		stringToolArg(input.Args, "section_key"),
		stringToolArg(input.Args, "description"),
		stringToolArg(input.Args, "content"),
		sourceText,
	)
	if err != nil {
		return nil, err
	}
	return &ToolResult{
		ToolName: ToolDynamicSectionCreate,
		Success:  true,
		Message:  fmt.Sprintf("已新增动态模块：%s", section.Title),
		After:    section,
		Undo: &interactionUndoPayload{
			Type:            "delete_dynamic_section",
			DynamicSections: []MeetingDynamicSection{*section},
		},
	}, nil
}

func (s *Server) toolDynamicSectionAddItem(ctx context.Context, input ToolInput) (*ToolResult, error) {
	command := dynamicSectionCommandFromToolArgs(input.Args)
	index, section, err := s.resolveDynamicSectionTarget(ctx, input.MeetingID, command)
	if err != nil {
		return nil, err
	}
	sourceText := dynamicSectionSourceText(input)
	item, created, err := s.store.AddDynamicSectionItem(
		ctx,
		input.MeetingID,
		section.ID,
		stringToolArg(input.Args, "title"),
		stringToolArg(input.Args, "content"),
		sourceText,
	)
	if err != nil {
		return nil, err
	}
	if !created || item == nil {
		return &ToolResult{
			ToolName: ToolDynamicSectionAddItem,
			Success:  true,
			Message:  fmt.Sprintf("动态模块「%s」中已存在相似内容。", section.Title),
			Before:   section,
			After:    section,
		}, nil
	}
	return &ToolResult{
		ToolName: ToolDynamicSectionAddItem,
		Success:  true,
		Message:  fmt.Sprintf("已向第 %d 个动态模块「%s」追加内容：%s", index, section.Title, item.Content),
		Before:   section,
		After:    item,
		Undo: &interactionUndoPayload{
			Type:               "delete_dynamic_section_item",
			DynamicSectionItem: item,
		},
	}, nil
}

func (s *Server) toolDynamicSectionUpdate(ctx context.Context, input ToolInput) (*ToolResult, error) {
	command := dynamicSectionCommandFromToolArgs(input.Args)
	index, before, err := s.resolveDynamicSectionTarget(ctx, input.MeetingID, command)
	if err != nil {
		return nil, err
	}
	title := stringToolArg(input.Args, "title")
	description := stringToolArg(input.Args, "description")
	if description == "" {
		description = stringToolArg(input.Args, "content")
	}
	if title == "" && description == "" {
		return nil, fmt.Errorf("empty dynamic section update")
	}
	if title == "" {
		title = before.Title
	}
	if description == "" {
		description = before.Description
	}
	after, err := s.store.UpdateDynamicSection(ctx, input.MeetingID, before.ID, title, description)
	if err != nil {
		return nil, err
	}
	return &ToolResult{
		ToolName: ToolDynamicSectionUpdate,
		Success:  true,
		Message:  fmt.Sprintf("Updated dynamic module %d: %s", index, after.Title),
		Before:   before,
		After:    after,
		Undo: &interactionUndoPayload{
			Type:            "restore_dynamic_section",
			DynamicSections: []MeetingDynamicSection{*before},
		},
	}, nil
}

func (s *Server) toolDynamicSectionItemUpdate(ctx context.Context, input ToolInput) (*ToolResult, error) {
	command := dynamicSectionCommandFromToolArgs(input.Args)
	sectionIndex, section, itemIndex, before, err := s.resolveDynamicSectionItemTarget(ctx, input.MeetingID, command)
	if err != nil {
		return nil, err
	}
	title := stringToolArg(input.Args, "title")
	content := stringToolArg(input.Args, "content")
	if title == "" && content == "" {
		return nil, fmt.Errorf("empty dynamic section item update")
	}
	after, err := s.store.UpdateDynamicSectionItem(ctx, input.MeetingID, before.ID, title, content)
	if err != nil {
		return nil, err
	}
	return &ToolResult{
		ToolName: ToolDynamicSectionItemUpdate,
		Success:  true,
		Message:  fmt.Sprintf("Updated dynamic module %d (%s) item %d: %s", sectionIndex, section.Title, itemIndex, after.Content),
		Before:   before,
		After:    after,
		Undo: &interactionUndoPayload{
			Type:               "restore_dynamic_section_item",
			DynamicSectionItem: before,
			DynamicSections:    []MeetingDynamicSection{*section},
		},
	}, nil
}

func (s *Server) toolRiskUpdate(ctx context.Context, input ToolInput) (*ToolResult, error) {
	command := riskCommandFromToolArgs(input.Args)
	index, before, err := s.resolveRiskTarget(ctx, input.MeetingID, command)
	if err != nil {
		return nil, err
	}
	mutation := riskMutationFromToolArgs(input.Args)
	if riskMutationIsEmpty(mutation) {
		return nil, fmt.Errorf("empty risk update")
	}
	after, err := s.store.UpdateRisk(ctx, input.MeetingID, before.ID, mutation)
	if err != nil {
		return nil, err
	}
	return &ToolResult{
		ToolName: ToolRiskUpdate,
		Success:  true,
		Message:  fmt.Sprintf("已更新第 %d 条风险：%s", index, after.Content),
		Before:   before,
		After:    after,
		Undo: &interactionUndoPayload{
			Type: "restore_risk",
			Risk: before,
		},
	}, nil
}

func (s *Server) toolRiskClose(ctx context.Context, input ToolInput) (*ToolResult, error) {
	command := riskCommandFromToolArgs(input.Args)
	index, before, err := s.resolveRiskTarget(ctx, input.MeetingID, command)
	if err != nil {
		return nil, err
	}
	after, err := s.store.CloseRisk(ctx, input.MeetingID, before.ID)
	if err != nil {
		return nil, err
	}
	return &ToolResult{
		ToolName: ToolRiskClose,
		Success:  true,
		Message:  fmt.Sprintf("已关闭第 %d 条风险：%s", index, after.Content),
		Before:   before,
		After:    after,
		Undo: &interactionUndoPayload{
			Type: "restore_risk",
			Risk: before,
		},
	}, nil
}

func (s *Server) toolOpenQuestionUpdate(ctx context.Context, input ToolInput) (*ToolResult, error) {
	command := openQuestionCommandFromToolArgs(input.Args)
	index, before, err := s.resolveOpenQuestionTarget(ctx, input.MeetingID, command)
	if err != nil {
		return nil, err
	}
	mutation := openQuestionMutationFromToolArgs(input.Args)
	if openQuestionMutationIsEmpty(mutation) {
		return nil, fmt.Errorf("empty open question update")
	}
	after, err := s.store.UpdateOpenQuestion(ctx, input.MeetingID, before.ID, mutation)
	if err != nil {
		return nil, err
	}
	return &ToolResult{
		ToolName: ToolOpenQuestionUpdate,
		Success:  true,
		Message:  fmt.Sprintf("已更新第 %d 条待确认问题：%s", index, after.Question),
		Before:   before,
		After:    after,
		Undo: &interactionUndoPayload{
			Type:         "restore_open_question",
			OpenQuestion: before,
		},
	}, nil
}

func (s *Server) toolOpenQuestionClose(ctx context.Context, input ToolInput) (*ToolResult, error) {
	command := openQuestionCommandFromToolArgs(input.Args)
	index, before, err := s.resolveOpenQuestionTarget(ctx, input.MeetingID, command)
	if err != nil {
		return nil, err
	}
	after, err := s.store.CloseOpenQuestion(ctx, input.MeetingID, before.ID)
	if err != nil {
		return nil, err
	}
	return &ToolResult{
		ToolName: ToolOpenQuestionClose,
		Success:  true,
		Message:  fmt.Sprintf("已关闭第 %d 条待确认问题：%s", index, after.Question),
		Before:   before,
		After:    after,
		Undo: &interactionUndoPayload{
			Type:         "restore_open_question",
			OpenQuestion: before,
		},
	}, nil
}

func riskCommandFromToolArgs(args map[string]any) InteractionCommand {
	return InteractionCommand{
		TargetIndex: intToolArg(args, "target_index"),
		TargetID:    stringToolArg(args, "target_id"),
	}
}

func dynamicSectionCommandFromToolArgs(args map[string]any) InteractionCommand {
	return InteractionCommand{
		TargetIndex:   intToolArg(args, "target_index"),
		TargetID:      stringToolArg(args, "target_id"),
		SourceIndices: intSliceToolArg(args, "source_indices"),
		SourceIDs:     stringSliceToolArg(args, "source_ids"),
		SectionKey:    stringToolArg(args, "section_key"),
		SectionTitle:  stringToolArg(args, "section_title"),
		Title:         stringToolArg(args, "title"),
		Content:       stringToolArg(args, "content"),
		Reason:        stringToolArg(args, "reason"),
	}
}

func dynamicSectionSourceText(input ToolInput) string {
	if strings.TrimSpace(input.InteractionEventID) != "" {
		return dynamicSectionVoiceSourceText
	}
	sourceText := stringToolArg(input.Args, "source_text")
	if sourceText == "" {
		sourceText = stringToolArg(input.Args, "reason")
	}
	return sourceText
}

func riskMutationFromToolArgs(args map[string]any) RiskMutation {
	return RiskMutation{
		Content:    stringToolArg(args, "content"),
		Impact:     stringToolArg(args, "impact"),
		Level:      stringToolArg(args, "level"),
		Owner:      stringToolArg(args, "owner"),
		Mitigation: stringToolArg(args, "mitigation"),
		DueDate:    stringToolArg(args, "due_date"),
		Status:     stringToolArg(args, "status"),
		SourceText: stringToolArg(args, "reason"),
	}
}

func openQuestionCommandFromToolArgs(args map[string]any) InteractionCommand {
	return InteractionCommand{
		TargetIndex: intToolArg(args, "target_index"),
		TargetID:    stringToolArg(args, "target_id"),
	}
}

func openQuestionMutationFromToolArgs(args map[string]any) OpenQuestionMutation {
	return OpenQuestionMutation{
		Question:   stringToolArg(args, "question"),
		Owner:      stringToolArg(args, "owner"),
		DueDate:    stringToolArg(args, "due_date"),
		Status:     stringToolArg(args, "status"),
		SourceText: stringToolArg(args, "reason"),
	}
}

func stringToolArg(args map[string]any, key string) string {
	if args == nil {
		return ""
	}
	switch value := args[key].(type) {
	case string:
		return strings.TrimSpace(value)
	case fmt.Stringer:
		return strings.TrimSpace(value.String())
	default:
		return ""
	}
}

func intToolArg(args map[string]any, key string) int {
	if args == nil {
		return 0
	}
	switch value := args[key].(type) {
	case int:
		return value
	case int64:
		return int(value)
	case float64:
		return int(value)
	case json.Number:
		n, _ := value.Int64()
		return int(n)
	default:
		return 0
	}
}

func intSliceToolArg(args map[string]any, key string) []int {
	if args == nil {
		return nil
	}
	switch value := args[key].(type) {
	case []int:
		return positiveUniqueInts(value)
	case []float64:
		items := make([]int, 0, len(value))
		for _, item := range value {
			items = append(items, int(item))
		}
		return positiveUniqueInts(items)
	case []any:
		items := make([]int, 0, len(value))
		for _, item := range value {
			items = append(items, intToolArg(map[string]any{"value": item}, "value"))
		}
		return positiveUniqueInts(items)
	case string:
		matches := regexp.MustCompile(`[0-9]+`).FindAllString(value, -1)
		items := make([]int, 0, len(matches))
		for _, match := range matches {
			n, _ := strconv.Atoi(match)
			items = append(items, n)
		}
		return positiveUniqueInts(items)
	default:
		return nil
	}
}

func stringSliceToolArg(args map[string]any, key string) []string {
	if args == nil {
		return nil
	}
	switch value := args[key].(type) {
	case []string:
		return uniqueNonEmptyStrings(value)
	case []any:
		items := make([]string, 0, len(value))
		for _, item := range value {
			items = append(items, stringToolArg(map[string]any{"value": item}, "value"))
		}
		return uniqueNonEmptyStrings(items)
	case string:
		return uniqueNonEmptyStrings(strings.FieldsFunc(value, func(r rune) bool {
			return r == ',' || r == ';' || r == ' ' || r == '，' || r == '；'
		}))
	default:
		return nil
	}
}
