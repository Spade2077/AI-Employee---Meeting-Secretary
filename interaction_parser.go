// 本文件负责会中语音指令的本地解析、LLM 路由解析和命令规范化。
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"regexp"
	"strconv"
	"strings"
	"time"
)

func (s *Server) parseInteractionCommand(ctx context.Context, meetingID, instruction string, timing *InteractionTiming) (InteractionCommand, string, error) {
	contextStarted := time.Now()
	snapshot, err := s.store.GetIntelligenceSnapshot(ctx, meetingID)
	timing.addStage("load_context", "读取结构化上下文", time.Since(contextStarted), "")
	if err != nil {
		return InteractionCommand{}, "", err
	}

	localStarted := time.Now()
	if command, ok := parseUndoLocally(instruction); ok {
		timing.addStage("local_parse", "本地解析指令", time.Since(localStarted), "未调用 LLM；本地撤销指令")
		command = normalizeInteractionCommand(command)
		bytes, _ := json.Marshal(command)
		return command, string(bytes), nil
	}

	configStarted := time.Now()
	cfg, err := s.store.GetLLMConfigFor(ctx, LLMUseCaseInteraction)
	timing.addStage("load_llm_config", "读取交互 LLM 配置", time.Since(configStarted), interactionLLMDetail(cfg))
	if err != nil {
		return InteractionCommand{}, "", err
	}
	if strings.TrimSpace(cfg.BaseURL) != "" && strings.TrimSpace(cfg.Model) != "" {
		llmCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
		defer cancel()
		llmStarted := time.Now()
		command, content, err := s.callLLMForInteraction(llmCtx, cfg, instruction, snapshot)
		timing.addStage("interaction_llm", "调用交互 LLM", time.Since(llmStarted), interactionLLMDetail(cfg))
		if err == nil {
			return normalizeInteractionCommand(command), content, nil
		}
		log.Printf("interaction llm failed meeting=%s: %v", meetingID, err)
	}

	fallbackStarted := time.Now()
	command, err := parseDeleteActionItemLocally(instruction)
	timing.addStage("local_fallback_parse", "本地兜底解析", time.Since(fallbackStarted), "未调用 LLM；本地删除待办兜底")
	if err != nil {
		return InteractionCommand{}, "", err
	}
	command = normalizeInteractionCommand(command)
	bytes, _ := json.Marshal(command)
	return command, string(bytes), nil
}

func normalizeInteractionCommand(command InteractionCommand) InteractionCommand {
	command.Intent = strings.TrimSpace(command.Intent)
	command.TargetType = strings.TrimSpace(command.TargetType)
	command.TargetID = strings.TrimSpace(command.TargetID)
	command.Task = strings.TrimSpace(command.Task)
	command.Owner = strings.TrimSpace(command.Owner)
	command.DueDate = strings.TrimSpace(command.DueDate)
	command.Status = normalizeActionItemStatus(command.Status)
	command.Content = strings.TrimSpace(command.Content)
	command.Reply = strings.TrimSpace(command.Reply)
	command.Reason = strings.TrimSpace(command.Reason)
	command.Instruction = strings.TrimSpace(command.Instruction)
	command.SourceIDs = uniqueNonEmptyStrings(command.SourceIDs)
	command.SourceIndices = positiveUniqueInts(command.SourceIndices)

	switch command.Intent {
	case "delete_action_item", "update_action_item", "create_action_item", "complete_action_item", "move_action_item_to_decision", "merge_action_items":
		if command.TargetType == "" {
			command.TargetType = "action_item"
		}
	case "delete_decision", "update_decision", "create_decision", "merge_decisions", "move_decision_to_action_item":
		if command.TargetType == "" {
			command.TargetType = "decision"
		}
	case "rewrite_summary_section":
		if command.TargetType == "" {
			command.TargetType = "summary"
		}
	case "undo_last_interaction":
		if command.TargetType == "" {
			command.TargetType = "interaction"
		}
	}
	return command
}

func parseUndoLocally(instruction string) (InteractionCommand, bool) {
	key := normalizeForCompare(instruction)
	if key == "" {
		return InteractionCommand{}, false
	}
	undoPhrases := []string{
		"撤销",
		"撤回",
		"恢复上一步",
		"取消刚刚",
		"还原刚刚",
		"undo",
		"revert",
	}
	for _, phrase := range undoPhrases {
		if strings.Contains(key, normalizeForCompare(phrase)) {
			return InteractionCommand{
				Intent:      "undo_last_interaction",
				TargetType:  "interaction",
				Confidence:  0.8,
				NeedConfirm: false,
				Reply:       "已识别为撤销上一步会中交互操作。",
			}, true
		}
	}
	return InteractionCommand{}, false
}

func parseDeleteActionItemLocally(instruction string) (InteractionCommand, error) {
	key := normalizeForCompare(instruction)
	if !strings.Contains(key, "待办") || !(strings.Contains(key, "删") || strings.Contains(key, "删除") || strings.Contains(key, "去掉")) {
		return InteractionCommand{Intent: "unsupported", TargetType: "action_item", Confidence: 0.2, NeedConfirm: true}, fmt.Errorf("unsupported local interaction instruction")
	}
	index := parseOrdinalIndex(instruction)
	if index <= 0 {
		return InteractionCommand{Intent: "delete_action_item", TargetType: "action_item", Confidence: 0.4, NeedConfirm: true}, nil
	}
	return InteractionCommand{
		Intent:      "delete_action_item",
		TargetType:  "action_item",
		TargetIndex: index,
		Confidence:  0.7,
		NeedConfirm: false,
		Reply:       fmt.Sprintf("已识别为删除第 %d 条待办。", index),
	}, nil
}

func parseOrdinalIndex(value string) int {
	value = strings.TrimSpace(value)
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`第\s*([0-9]+|[一二两三四五六七八九十]{1,3})\s*[条项个]`),
		regexp.MustCompile(`([0-9]+|[一二两三四五六七八九十]{1,3})\s*[条项个]`),
	}
	for _, pattern := range patterns {
		matches := pattern.FindStringSubmatch(value)
		if len(matches) < 2 {
			continue
		}
		if number, err := strconv.Atoi(matches[1]); err == nil {
			return number
		}
		if number := chineseNumber(matches[1]); number > 0 {
			return number
		}
	}
	return 0
}

func chineseNumber(value string) int {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	digits := map[rune]int{
		'一': 1,
		'二': 2,
		'两': 2,
		'三': 3,
		'四': 4,
		'五': 5,
		'六': 6,
		'七': 7,
		'八': 8,
		'九': 9,
	}
	if value == "十" {
		return 10
	}
	runes := []rune(value)
	if len(runes) == 1 {
		return digits[runes[0]]
	}
	parts := strings.Split(value, "十")
	if len(parts) != 2 {
		return 0
	}
	tens := 1
	if parts[0] != "" {
		tens = digits[[]rune(parts[0])[0]]
	}
	ones := 0
	if parts[1] != "" {
		ones = digits[[]rune(parts[1])[0]]
	}
	if tens == 0 {
		return 0
	}
	return tens*10 + ones
}

func positiveUniqueInts(values []int) []int {
	seen := map[int]bool{}
	result := make([]int, 0, len(values))
	for _, value := range values {
		if value <= 0 || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	return result
}
