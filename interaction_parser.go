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
	if command, ok := parseMeetingTitleLocally(instruction); ok {
		timing.addStage("local_parse", "本地解析指令", time.Since(localStarted), "未调用 LLM；本地会议标题指令")
		command = normalizeInteractionCommand(command)
		bytes, _ := json.Marshal(command)
		return command, string(bytes), nil
	}
	if command, ok := parseEndMeetingLocally(instruction); ok {
		timing.addStage("local_parse", "本地解析指令", time.Since(localStarted), "未调用 LLM；本地结束会议确认指令")
		command = normalizeInteractionCommand(command)
		bytes, _ := json.Marshal(command)
		return command, string(bytes), nil
	}
	if command, ok := parseShowShareQRLocally(instruction); ok {
		timing.addStage("local_parse", "本地解析指令", time.Since(localStarted), "未调用 LLM；本地二维码展示指令")
		command = normalizeInteractionCommand(command)
		bytes, _ := json.Marshal(command)
		return command, string(bytes), nil
	}

	if command, ok := parseMeetingViewLocally(instruction); ok {
		timing.addStage("local_parse", "本地解析指令", time.Since(localStarted), "未调用 LLM；本地页面切换指令")
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
	if strings.TrimSpace(command.Status) != "" {
		command.Status = normalizeActionItemStatus(command.Status)
	}
	command.Impact = strings.TrimSpace(command.Impact)
	if strings.TrimSpace(command.Level) != "" {
		command.Level = normalizeRiskLevel(command.Level)
	}
	command.Mitigation = strings.TrimSpace(command.Mitigation)
	command.Question = strings.TrimSpace(command.Question)
	command.Title = normalizeMeetingTitleInput(command.Title)
	command.Content = strings.TrimSpace(command.Content)
	command.Reply = strings.TrimSpace(command.Reply)
	command.Reason = strings.TrimSpace(command.Reason)
	command.Instruction = strings.TrimSpace(command.Instruction)
	command.SourceIDs = uniqueNonEmptyStrings(command.SourceIDs)
	command.SourceIndices = positiveUniqueInts(command.SourceIndices)

	switch command.Intent {
	case "update_meeting_title":
		if command.TargetType == "" {
			command.TargetType = "meeting"
		}
		if command.Title == "" {
			command.Title = normalizeMeetingTitleInput(command.Content)
		}
	case "switch_meeting_view":
		if command.TargetType == "" {
			command.TargetType = "meeting_view"
		}
		view := normalizeMeetingView(command.TargetID)
		if view == "" {
			view = normalizeMeetingView(command.Content)
		}
		command.TargetID = view
		command.Content = view
	case "show_share_qr":
		if command.TargetType == "" {
			command.TargetType = "share"
		}
		if command.TargetID == "" {
			command.TargetID = "minutes_qr"
		}
	case "request_end_meeting":
		if command.TargetType == "" {
			command.TargetType = "meeting"
		}
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

func parseMeetingTitleLocally(instruction string) (InteractionCommand, bool) {
	text := strings.TrimSpace(instruction)
	if text == "" {
		return InteractionCommand{}, false
	}
	key := normalizeForCompare(text)
	if !strings.Contains(key, "标题") && !strings.Contains(key, "会议名") && !strings.Contains(key, "会议名称") {
		return InteractionCommand{}, false
	}
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`(?:把)?(?:会议)?标题(?:改成|改为|设置为|设为|叫做|叫|定为)\s*(.+)$`),
		regexp.MustCompile(`(?:设置|修改|更改)(?:会议)?标题(?:为|成)?\s*(.+)$`),
		regexp.MustCompile(`(?:会议名|会议名称)(?:改成|改为|设置为|设为|叫做|叫|定为)\s*(.+)$`),
	}
	for _, pattern := range patterns {
		matches := pattern.FindStringSubmatch(text)
		if len(matches) < 2 {
			continue
		}
		title := cleanupParsedMeetingTitle(matches[1])
		if title == "" {
			continue
		}
		return InteractionCommand{
			Intent:      "update_meeting_title",
			TargetType:  "meeting",
			Title:       title,
			Content:     title,
			Confidence:  0.85,
			NeedConfirm: false,
			Reply:       "已识别为修改会议标题。",
		}, true
	}
	return InteractionCommand{}, false
}

func cleanupParsedMeetingTitle(value string) string {
	value = strings.TrimSpace(value)
	stoppers := []string{"。", "，", ",", "；", ";", "谢谢", "就这样", "可以吗"}
	for _, stopper := range stoppers {
		if index := strings.Index(value, stopper); index >= 0 {
			value = value[:index]
		}
	}
	return normalizeMeetingTitleInput(value)
}

func parseEndMeetingLocally(instruction string) (InteractionCommand, bool) {
	text := strings.TrimSpace(instruction)
	if text == "" {
		return InteractionCommand{}, false
	}
	key := strings.ToLower(text)
	hasEnd := strings.Contains(key, "结束") ||
		strings.Contains(key, "停止") ||
		strings.Contains(key, "关闭会议") ||
		strings.Contains(key, "关闭本次会议") ||
		strings.Contains(key, "end")
	hasMeeting := strings.Contains(key, "会议") ||
		strings.Contains(key, "本次") ||
		strings.Contains(key, "记录") ||
		strings.Contains(key, "meeting")
	if !hasEnd || !hasMeeting {
		return InteractionCommand{}, false
	}
	return InteractionCommand{
		Intent:      "request_end_meeting",
		TargetType:  "meeting",
		Confidence:  0.9,
		NeedConfirm: false,
		Reply:       "已收到结束会议请求，需要确认后才会结束。",
	}, true
}

func parseShowShareQRLocally(instruction string) (InteractionCommand, bool) {
	text := strings.TrimSpace(instruction)
	if text == "" {
		return InteractionCommand{}, false
	}
	key := strings.ToLower(text)
	hasQR := strings.Contains(key, "二维码") ||
		strings.Contains(key, "扫码") ||
		strings.Contains(key, "qr") ||
		strings.Contains(key, "下载纪要") ||
		strings.Contains(key, "纪要下载")
	hasVerb := strings.Contains(key, "展示") ||
		strings.Contains(key, "显示") ||
		strings.Contains(key, "打开") ||
		strings.Contains(key, "给我看") ||
		strings.Contains(key, "show")
	if !hasQR || !hasVerb {
		return InteractionCommand{}, false
	}
	return InteractionCommand{
		Intent:      "show_share_qr",
		TargetType:  "share",
		TargetID:    "minutes_qr",
		Confidence:  0.9,
		NeedConfirm: false,
		Reply:       "已收到展示会议纪要二维码请求。",
	}, true
}

func parseMeetingViewLocally(instruction string) (InteractionCommand, bool) {
	text := strings.TrimSpace(instruction)
	if text == "" {
		return InteractionCommand{}, false
	}
	lower := strings.ToLower(text)
	hasViewVerb := strings.Contains(lower, "切换") ||
		strings.Contains(lower, "打开") ||
		strings.Contains(lower, "展示") ||
		strings.Contains(lower, "显示") ||
		strings.Contains(lower, "进入") ||
		strings.Contains(lower, "回到") ||
		strings.Contains(lower, "看一下") ||
		strings.Contains(lower, "给我看") ||
		strings.Contains(lower, "show") ||
		strings.Contains(lower, "switch")
	if !hasViewVerb {
		return InteractionCommand{}, false
	}
	view := normalizeMeetingView(lower)
	if view == "" {
		return InteractionCommand{}, false
	}
	return InteractionCommand{
		Intent:      "switch_meeting_view",
		TargetType:  "meeting_view",
		TargetID:    view,
		Content:     view,
		Confidence:  0.86,
		NeedConfirm: false,
		Reply:       "已识别为页面切换指令。",
	}, true
}

func normalizeMeetingView(value string) string {
	key := strings.ToLower(strings.TrimSpace(value))
	switch {
	case key == "live" || strings.Contains(key, "实时") || strings.Contains(key, "转写") || strings.Contains(key, "记录"):
		return "live"
	case key == "summary" || strings.Contains(key, "总结") || strings.Contains(key, "摘要") || strings.Contains(key, "待办") || strings.Contains(key, "决策") || strings.Contains(key, "风险") || strings.Contains(key, "确认"):
		return "summary"
	default:
		return ""
	}
}

func meetingViewLabel(view string) string {
	switch normalizeMeetingView(view) {
	case "live":
		return "实时记录页"
	default:
		return "会议总结页"
	}
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
