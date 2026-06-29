// 本文件负责统一执行已注册工具、记录操作流水和保存撤销载荷。
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

type ToolExecutor struct {
	registry *ToolRegistry
	store    *Store
}

func NewToolExecutor(registry *ToolRegistry, store *Store) *ToolExecutor {
	return &ToolExecutor{registry: registry, store: store}
}

func (executor *ToolExecutor) Execute(ctx context.Context, input ToolExecutionInput) (*ToolResult, error) {
	if input.Args == nil {
		input.Args = map[string]any{}
	}
	tool, ok := executor.registry.Get(input.ToolName)
	if !ok {
		err := fmt.Errorf("unknown tool: %s", input.ToolName)
		result := &ToolResult{ToolName: input.ToolName, Success: false, Message: err.Error()}
		_ = executor.saveJournal(ctx, input, result, err)
		return result, err
	}
	if err := tool.ParamSchema.Validate(input.Args); err != nil {
		result := &ToolResult{ToolName: tool.Name, Success: false, Message: err.Error()}
		_ = executor.saveJournal(ctx, input, result, err)
		return result, err
	}

	result, err := tool.Handler(ctx, ToolInput{
		MeetingID:          input.MeetingID,
		InteractionEventID: input.InteractionEventID,
		StepIndex:          input.StepIndex,
		Args:               input.Args,
	})
	if result == nil {
		result = &ToolResult{}
	}
	result.ToolName = tool.Name
	result.Success = err == nil
	if err != nil && result.Message == "" {
		result.Message = err.Error()
	}

	if journalErr := executor.saveJournal(ctx, input, result, err); journalErr != nil && err == nil {
		err = journalErr
		result.Success = false
		result.Message = journalErr.Error()
	}
	if err == nil && !tool.Readonly && tool.Undoable && result.Undo != nil {
		if undoErr := executor.saveUndo(ctx, input, result.Undo); undoErr != nil {
			err = undoErr
			result.Success = false
			result.Message = undoErr.Error()
		}
	}
	return result, err
}

func (executor *ToolExecutor) saveJournal(ctx context.Context, input ToolExecutionInput, result *ToolResult, execErr error) error {
	status := "success"
	errorMessage := ""
	if execErr != nil || result == nil || !result.Success {
		status = "failed"
		if execErr != nil {
			errorMessage = execErr.Error()
		}
	}
	if result == nil {
		result = &ToolResult{}
	}
	_, err := executor.store.SaveOperationJournalRecord(ctx, OperationJournalRecord{
		MeetingID:          input.MeetingID,
		InteractionEventID: input.InteractionEventID,
		StepIndex:          input.StepIndex,
		ToolName:           string(input.ToolName),
		ArgsJSON:           marshalToolJSON(input.Args),
		BeforeJSON:         marshalToolJSON(result.Before),
		AfterJSON:          marshalToolJSON(result.After),
		UndoJSON:           marshalToolJSON(result.Undo),
		Status:             status,
		ErrorMessage:       errorMessage,
	})
	return err
}

func (executor *ToolExecutor) saveUndo(ctx context.Context, input ToolExecutionInput, payload *interactionUndoPayload) error {
	payload.Type = normalizeToolString(payload.Type)
	if payload.Type == "" {
		return fmt.Errorf("tool %s returned empty undo payload type", input.ToolName)
	}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = executor.store.SaveUndoRecord(ctx, InteractionUndoRecord{
		MeetingID:          input.MeetingID,
		InteractionEventID: input.InteractionEventID,
		UndoType:           payload.Type,
		UndoJSON:           string(payloadBytes),
	})
	return err
}

func marshalToolJSON(value any) string {
	if value == nil {
		return ""
	}
	bytes, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	return string(bytes)
}

func normalizeToolString(value string) string {
	return strings.TrimSpace(value)
}
