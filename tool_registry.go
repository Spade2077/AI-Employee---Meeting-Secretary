// 本文件负责注册 Agent 可调用的后端工具，并定义统一工具执行协议。
package main

import (
	"context"
	"fmt"
	"strings"
)

type ToolName string

const (
	ToolMeetingGetSnapshot ToolName = "meeting.get_snapshot"
	ToolMeetingUpdateTitle ToolName = "meeting.update_title"
	ToolMissingAlertList   ToolName = "missing_alert.list"
	ToolRiskUpdate         ToolName = "risk.update"
	ToolRiskClose          ToolName = "risk.close"
	ToolOpenQuestionUpdate ToolName = "open_question.update"
	ToolOpenQuestionClose  ToolName = "open_question.close"
)

type ToolHandler func(ctx context.Context, input ToolInput) (*ToolResult, error)

type ToolDefinition struct {
	Name        ToolName
	Description string
	Readonly    bool
	Undoable    bool
	ParamSchema ToolParamSchema
	Handler     ToolHandler
}

type ToolParamSchema struct {
	Required []string
	Allowed  []string
}

func (schema ToolParamSchema) Validate(args map[string]any) error {
	if args == nil {
		args = map[string]any{}
	}
	allowed := map[string]bool{}
	for _, key := range schema.Allowed {
		allowed[key] = true
	}
	for key := range args {
		if len(allowed) > 0 && !allowed[key] {
			return fmt.Errorf("unknown tool arg: %s", key)
		}
	}
	for _, key := range schema.Required {
		if isEmptyToolArg(args[key]) {
			return fmt.Errorf("missing required tool arg: %s", key)
		}
	}
	return nil
}

type ToolRegistry struct {
	tools map[ToolName]ToolDefinition
}

func NewToolRegistry() *ToolRegistry {
	return &ToolRegistry{tools: map[ToolName]ToolDefinition{}}
}

func (registry *ToolRegistry) Register(definition ToolDefinition) {
	definition.Name = ToolName(strings.TrimSpace(string(definition.Name)))
	if definition.Name == "" {
		return
	}
	registry.tools[definition.Name] = definition
}

func (registry *ToolRegistry) Get(name ToolName) (ToolDefinition, bool) {
	definition, ok := registry.tools[name]
	return definition, ok
}

func NewMeetingToolRegistry(server *Server) *ToolRegistry {
	registry := NewToolRegistry()
	registry.Register(ToolDefinition{
		Name:        ToolMeetingGetSnapshot,
		Description: "读取当前会议的结构化快照。",
		Readonly:    true,
		Undoable:    false,
		ParamSchema: ToolParamSchema{Allowed: []string{}},
		Handler:     server.toolMeetingGetSnapshot,
	})
	registry.Register(ToolDefinition{
		Name:        ToolMeetingUpdateTitle,
		Description: "设置会议标题，并锁定标题，避免后续自动结构化覆盖。",
		Readonly:    false,
		Undoable:    true,
		ParamSchema: ToolParamSchema{Required: []string{"title"}, Allowed: []string{"title", "reason"}},
		Handler:     server.toolMeetingUpdateTitle,
	})
	registry.Register(ToolDefinition{
		Name:        ToolMissingAlertList,
		Description: "读取当前会议的缺失项提醒，可按对象类型和字段过滤。",
		Readonly:    true,
		Undoable:    false,
		ParamSchema: ToolParamSchema{Allowed: []string{"target_type", "field"}},
		Handler:     server.toolMissingAlertList,
	})
	registry.Register(ToolDefinition{
		Name:        ToolRiskUpdate,
		Description: "更新风险内容、影响、等级、负责人、处理方案、截止时间或状态。",
		Readonly:    false,
		Undoable:    true,
		ParamSchema: ToolParamSchema{Allowed: []string{"target_index", "target_id", "content", "impact", "level", "owner", "mitigation", "due_date", "status", "reason"}},
		Handler:     server.toolRiskUpdate,
	})
	registry.Register(ToolDefinition{
		Name:        ToolRiskClose,
		Description: "将一个风险标记为已关闭或已解决。",
		Readonly:    false,
		Undoable:    true,
		ParamSchema: ToolParamSchema{Allowed: []string{"target_index", "target_id", "reason"}},
		Handler:     server.toolRiskClose,
	})
	registry.Register(ToolDefinition{
		Name:        ToolOpenQuestionUpdate,
		Description: "更新待确认问题的问题内容、负责人、确认时间或状态。",
		Readonly:    false,
		Undoable:    true,
		ParamSchema: ToolParamSchema{Allowed: []string{"target_index", "target_id", "question", "owner", "due_date", "status", "reason"}},
		Handler:     server.toolOpenQuestionUpdate,
	})
	registry.Register(ToolDefinition{
		Name:        ToolOpenQuestionClose,
		Description: "将一个待确认问题标记为已确认或已关闭。",
		Readonly:    false,
		Undoable:    true,
		ParamSchema: ToolParamSchema{Allowed: []string{"target_index", "target_id", "reason"}},
		Handler:     server.toolOpenQuestionClose,
	})
	return registry
}

type ToolExecutionInput struct {
	MeetingID          string
	InteractionEventID string
	StepIndex          int
	ToolName           ToolName
	Args               map[string]any
}

type ToolInput struct {
	MeetingID          string
	InteractionEventID string
	StepIndex          int
	Args               map[string]any
}

type ToolResult struct {
	ToolName ToolName                `json:"toolName"`
	Success  bool                    `json:"success"`
	Message  string                  `json:"message"`
	Data     any                     `json:"data,omitempty"`
	Before   any                     `json:"before,omitempty"`
	After    any                     `json:"after,omitempty"`
	Undo     *interactionUndoPayload `json:"undo,omitempty"`
}

func isEmptyToolArg(value any) bool {
	switch typed := value.(type) {
	case nil:
		return true
	case string:
		return strings.TrimSpace(typed) == ""
	case int:
		return typed == 0
	case int64:
		return typed == 0
	case float64:
		return typed == 0
	default:
		return false
	}
}
