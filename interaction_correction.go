package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// 本文件负责处理会中语音指令里的口误纠错。
// 纠错不是让 LLM 直接改库，而是先撤销上一条可撤销操作，再把修正后的自然语言重新走一次 Interaction 解析和执行链路。

func (s *Server) executeCorrectionInteraction(ctx context.Context, meetingID, eventID, originalInstruction, correctedInstruction string, timing *InteractionTiming) (*IntelligenceSnapshot, string, InteractionCommand, string, error) {
	correctedInstruction = cleanupCorrectionValue(correctedInstruction)
	if correctedInstruction == "" {
		return nil, "没有识别到纠正后的指令。", InteractionCommand{}, "", fmt.Errorf("correction instruction is empty")
	}

	undoStarted := time.Now()
	undoSnapshot, undoMessage, err := s.undoLastInteraction(ctx, meetingID, eventID)
	if timing != nil {
		timing.addStage("correction_undo", "撤销上一条误操作", time.Since(undoStarted), "")
	}
	if err != nil {
		return undoSnapshot, undoMessage, InteractionCommand{}, "", err
	}

	parseStarted := time.Now()
	correctedCommand, correctedJSON, err := s.parseInteractionCommand(ctx, meetingID, correctedInstruction, timing)
	if timing != nil {
		timing.addStage("correction_parse", "解析纠正后的指令", time.Since(parseStarted), correctedCommand.Intent)
	}
	if err != nil {
		return undoSnapshot, "已撤销上一条操作，但纠正后的指令理解失败。", correctedCommand, correctedJSON, err
	}
	correctedCommand.Instruction = strings.TrimSpace(correctedInstruction)
	if correctedCommand.Intent == "correct_last_interaction" || correctedCommand.Intent == "undo_last_interaction" {
		return undoSnapshot, "已撤销上一条操作，但纠正后的指令不能再次是撤销或纠错。", correctedCommand, correctedJSON, fmt.Errorf("nested correction is not supported")
	}

	executeStarted := time.Now()
	snapshot, message, err := s.executeInteractionCommandWithEventID(ctx, meetingID, correctedCommand, eventID)
	if timing != nil {
		timing.addStage("correction_execute", "执行纠正后的指令", time.Since(executeStarted), correctedCommand.Intent)
	}
	if err != nil {
		return snapshot, "已撤销上一条操作，但执行纠正后的指令失败：" + message, correctedCommand, correctedJSON, err
	}
	if message == "" {
		message = correctedCommand.Reply
	}
	if message == "" {
		message = "纠正后的指令已执行。"
	}
	return snapshot, fmt.Sprintf("已纠正口误：%s\n%s", undoMessage, message), correctedCommand, correctedJSON, nil
}

func interactionCommandJSON(command InteractionCommand) string {
	bytes, err := json.Marshal(command)
	if err != nil {
		return ""
	}
	return string(bytes)
}
