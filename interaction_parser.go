// 本文件负责会中语音指令的本地解析、LLM 路由解析和命令规范化。
package main

import (
	"context"
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
		return InteractionCommand{}, "", err
	}

	return InteractionCommand{}, "", fmt.Errorf("interaction LLM is not configured")
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
	command.SectionKey = strings.TrimSpace(command.SectionKey)
	command.SectionTitle = strings.TrimSpace(command.SectionTitle)
	command.Content = strings.TrimSpace(command.Content)
	command.Reply = strings.TrimSpace(command.Reply)
	command.Reason = strings.TrimSpace(command.Reason)
	command.Instruction = strings.TrimSpace(command.Instruction)
	command.SourceIDs = uniqueNonEmptyStrings(command.SourceIDs)
	command.SourceIndices = positiveUniqueInts(command.SourceIndices)

	switch command.Intent {
	case "start_meeting":
		if command.TargetType == "" {
			command.TargetType = "meeting"
		}
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
	case "set_transcript_timestamps":
		if command.TargetType == "" {
			command.TargetType = "transcript_timestamp"
		}
		target := normalizeToggleTarget(command.TargetID)
		if target == "" {
			target = normalizeToggleTarget(command.Content)
		}
		command.TargetID = target
		command.Content = target
	case "scroll_summary":
		if command.TargetType == "" {
			command.TargetType = "meeting_view"
		}
		action := normalizeScrollAction(command.TargetID)
		if action == "" {
			action = normalizeScrollAction(command.Content)
		}
		command.TargetID = action
		command.Content = action
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
	case "create_dynamic_section", "add_dynamic_section_item", "update_dynamic_section", "update_dynamic_section_item", "delete_dynamic_section", "merge_dynamic_sections", "show_dynamic_section", "hide_dynamic_section", "pin_dynamic_section", "unpin_dynamic_section", "show_all_dynamic_sections", "hide_all_dynamic_sections":
		if command.TargetType == "" {
			command.TargetType = "dynamic_section"
		}
		if command.SectionTitle == "" {
			command.SectionTitle = strings.TrimSpace(command.Title)
		}
		if command.SectionTitle == "" && isDynamicSectionVisibilityIntent(command.Intent) {
			command.SectionTitle = strings.TrimSpace(command.Content)
		}
	case "update_framework_section", "append_framework_section", "rewrite_framework_section", "clear_framework_section":
		if command.TargetType == "" {
			command.TargetType = "framework_section"
		}
		if command.SectionTitle == "" {
			command.SectionTitle = strings.TrimSpace(command.Title)
		}
	case "delete_action_item", "update_action_item", "create_action_item", "complete_action_item", "move_action_item_to_decision", "merge_action_items":
		if command.TargetType == "" {
			command.TargetType = "action_item"
		}
	case "delete_decision", "update_decision", "create_decision", "merge_decisions", "move_decision_to_action_item":
		if command.TargetType == "" {
			command.TargetType = "decision"
		}
	case "create_risk", "update_risk", "close_risk", "delete_risk", "merge_risks":
		if command.TargetType == "" {
			command.TargetType = "risk"
		}
	case "create_open_question", "update_open_question", "close_open_question", "delete_open_question", "delete_open_questions", "merge_open_questions":
		if command.TargetType == "" {
			command.TargetType = "open_question"
		}
	case "rewrite_summary_section":
		if command.TargetType == "" {
			command.TargetType = "summary"
		}
	case "undo_last_interaction":
		if command.TargetType == "" {
			command.TargetType = "interaction"
		}
	case "correct_last_interaction":
		if command.TargetType == "" {
			command.TargetType = "interaction"
		}
	}
	return command
}

func isDynamicSectionVisibilityIntent(intent string) bool {
	switch intent {
	case "show_dynamic_section", "hide_dynamic_section", "pin_dynamic_section", "unpin_dynamic_section":
		return true
	default:
		return false
	}
}

func parseCorrectionLocally(instruction string) (InteractionCommand, bool) {
	text := strings.TrimSpace(instruction)
	if text == "" {
		return InteractionCommand{}, false
	}

	patterns := []*regexp.Regexp{
		regexp.MustCompile(`(?i)^不是\s*(.*?)\s*[，,。；;：:\s]*(?:应该是|应当是|改成|换成|更正为|纠正为|是)\s*(.+)$`),
		regexp.MustCompile(`(?i)^(?:刚才|刚刚|上一句|上一步|前面|刚才那个|刚才那条|刚刚那个).*(?:说错了|不对|错了|搞错了|弄错了).*(?:应该是|应当是|改成|换成|更正为|纠正为)\s*(.+)$`),
		regexp.MustCompile(`(?i)^(?:纠正一下|更正一下|改口一下|我说错了|刚才说错了|刚刚说错了|上一句说错了|上一步说错了)[，,。；;：:\s]*(.+)$`),
	}
	for _, pattern := range patterns {
		matches := pattern.FindStringSubmatch(text)
		if len(matches) == 0 {
			continue
		}
		content := matches[len(matches)-1]
		reason := ""
		if len(matches) > 2 {
			reason = matches[1]
		}
		if command, ok := newCorrectionCommand(content, reason); ok {
			return command, true
		}
	}

	prefixes := []string{
		"刚才那个不对",
		"刚刚那个不对",
		"刚才那条不对",
		"刚刚那条不对",
		"上一句不对",
		"上一步不对",
		"刚才那个错了",
		"刚刚那个错了",
	}
	for _, prefix := range prefixes {
		if strings.HasPrefix(text, prefix) {
			if command, ok := newCorrectionCommand(strings.TrimPrefix(text, prefix), prefix); ok {
				return command, true
			}
		}
	}
	return InteractionCommand{}, false
}

func newCorrectionCommand(content, reason string) (InteractionCommand, bool) {
	content = cleanupCorrectionValue(content)
	reason = cleanupCorrectionValue(reason)
	if content == "" {
		return InteractionCommand{}, false
	}
	contentKey := normalizeForCompare(content)
	if contentKey == "" || containsNormalizedPhrase(contentKey, "撤销", "撤回", "undo") {
		return InteractionCommand{}, false
	}
	return InteractionCommand{
		Intent:      "correct_last_interaction",
		TargetType:  "interaction",
		Content:     content,
		Confidence:  0.85,
		NeedConfirm: false,
		Reply:       "已识别为口误纠错，将先撤销上一条操作再执行纠正后的指令。",
		Reason:      reason,
	}, true
}

func cleanupCorrectionValue(value string) string {
	value = strings.TrimSpace(value)
	value = strings.Trim(value, "，。！？、；;,.!?：: ")
	for _, prefix := range []string{"应该是", "应当是", "应该", "应当", "改成", "换成", "更正为", "纠正为", "是", "为"} {
		value = strings.TrimPrefix(value, prefix)
		value = strings.TrimSpace(value)
		value = strings.Trim(value, "，。！？、；;,.!?：: ")
	}
	for _, suffix := range []string{"谢谢", "就这样", "先这样"} {
		value = strings.TrimSuffix(value, suffix)
		value = strings.TrimSpace(value)
		value = strings.Trim(value, "，。！？、；;,.!?：: ")
	}
	return value
}

func parseUndoLocally(instruction string) (InteractionCommand, bool) {
	if !isPrimaryUndoInstruction(instruction) {
		return InteractionCommand{}, false
	}
	return InteractionCommand{
		Intent:      "undo_last_interaction",
		TargetType:  "interaction",
		Confidence:  0.88,
		NeedConfirm: false,
		Reply:       "已识别为撤销上一步会中交互操作。",
	}, true
}

func isPrimaryUndoInstruction(instruction string) bool {
	key := normalizeForCompare(instruction)
	if key == "" {
		return false
	}
	key = trimNormalizedPoliteLead(key)
	switch key {
	case "撤销", "撤回", "撤销操作", "撤回操作", "undo", "revert":
		return true
	}
	prefixes := []string{
		"撤销刚刚", "撤销刚才", "撤销上一条", "撤销上一步", "撤销上次", "撤销最近", "撤销前一步",
		"撤回刚刚", "撤回刚才", "撤回上一条", "撤回上一步", "撤回上次", "撤回最近", "撤回前一步",
		"取消刚刚", "取消刚才", "取消上一条", "取消上一步", "取消上次", "取消最近",
		"还原刚刚", "还原刚才", "还原上一条", "还原上一步", "还原上次",
		"恢复上一步", "恢复上一条", "恢复刚才", "恢复刚刚",
	}
	for _, prefix := range prefixes {
		if strings.HasPrefix(key, normalizeForCompare(prefix)) {
			return true
		}
	}
	phrases := []string{
		"刚刚的操作撤销", "刚才的操作撤销", "上一条操作撤销", "上一步操作撤销",
		"刚刚的操作撤回", "刚才的操作撤回", "上一条操作撤回", "上一步操作撤回",
		"把刚刚的操作撤销", "把刚才的操作撤销", "把上一条操作撤销", "把上一步操作撤销",
	}
	for _, phrase := range phrases {
		if strings.Contains(key, normalizeForCompare(phrase)) {
			return true
		}
	}
	return false
}

func trimNormalizedPoliteLead(key string) string {
	for {
		changed := false
		for _, prefix := range []string{"帮我", "请帮我", "请", "麻烦", "麻烦你", "会议助手"} {
			normalized := normalizeForCompare(prefix)
			if strings.HasPrefix(key, normalized) {
				key = strings.TrimPrefix(key, normalized)
				changed = true
			}
		}
		if !changed {
			return key
		}
	}
}

func parseStartMeetingLocally(instruction string) (InteractionCommand, bool) {
	text := strings.TrimSpace(instruction)
	if text == "" || looksLikeBusinessMutationInstruction(text) || looksLikeNegatedSystemControl(text) {
		return InteractionCommand{}, false
	}
	key := normalizeForCompare(text)
	if key == "" {
		return InteractionCommand{}, false
	}
	hasStart := containsNormalizedPhrase(key, "开始", "启动", "开启", "进入记录", "开始记录", "开始转写", "start")
	hasMeeting := containsNormalizedPhrase(key, "会议", "本次", "记录", "转写", "meeting")
	if !hasStart || !hasMeeting {
		return InteractionCommand{}, false
	}
	return InteractionCommand{
		Intent:      "start_meeting",
		TargetType:  "meeting",
		Confidence:  0.92,
		NeedConfirm: false,
		Reply:       "已识别为开始会议。",
	}, true
}

func parseTranscriptTimestampLocally(instruction string) (InteractionCommand, bool) {
	text := strings.TrimSpace(instruction)
	if text == "" || looksLikeNegatedSystemControl(text) {
		return InteractionCommand{}, false
	}
	key := normalizeForCompare(text)
	if key == "" || !containsNormalizedPhrase(key, "时间戳", "时间码", "timestamp", "timestamps") {
		return InteractionCommand{}, false
	}
	target := ""
	if containsNormalizedPhrase(key, "打开", "开启", "显示", "展示", "加上", "带上", "显示出来", "show", "enable", "on") {
		target = "on"
	}
	if containsNormalizedPhrase(key, "关闭", "隐藏", "去掉", "不要显示", "取消", "关掉", "hide", "disable", "off") {
		target = "off"
	}
	if target == "" {
		return InteractionCommand{}, false
	}
	return InteractionCommand{
		Intent:      "set_transcript_timestamps",
		TargetType:  "transcript_timestamp",
		TargetID:    target,
		Content:     target,
		Confidence:  0.9,
		NeedConfirm: false,
		Reply:       transcriptTimestampReply(target),
	}, true
}

func transcriptTimestampReply(target string) string {
	if target == "off" {
		return "已关闭转写时间戳。"
	}
	return "已打开转写时间戳。"
}

func parseSummaryScrollLocally(instruction string) (InteractionCommand, bool) {
	text := strings.TrimSpace(instruction)
	if text == "" || looksLikeNegatedSystemControl(text) {
		return InteractionCommand{}, false
	}
	key := normalizeForCompare(text)
	if key == "" || !containsNormalizedPhrase(key, "总结", "摘要", "纪要", "会议总结", "summary") {
		return InteractionCommand{}, false
	}
	action := normalizeScrollAction(key)
	if action == "" {
		return InteractionCommand{}, false
	}
	return InteractionCommand{
		Intent:      "scroll_summary",
		TargetType:  "meeting_view",
		TargetID:    action,
		Content:     action,
		Confidence:  0.88,
		NeedConfirm: false,
		Reply:       summaryScrollReply(action),
	}, true
}

func summaryScrollReply(action string) string {
	switch action {
	case "up":
		return "会议总结向上滚动。"
	case "down":
		return "会议总结向下滚动。"
	default:
		return "已停止会议总结滚动。"
	}
}

func normalizeToggleTarget(value string) string {
	key := normalizeForCompare(value)
	switch key {
	case "on", "open", "enable", "enabled", "true", "1", "打开", "开启", "显示", "展示":
		return "on"
	case "off", "close", "disable", "disabled", "false", "0", "关闭", "关掉", "隐藏", "取消":
		return "off"
	default:
		return ""
	}
}

func normalizeScrollAction(value string) string {
	key := normalizeForCompare(value)
	if key == "" {
		return ""
	}
	if containsNormalizedPhrase(key, "停止", "停下", "暂停滚动", "停止滚动", "不要动", "stop") {
		return "stop"
	}
	if containsNormalizedPhrase(key, "上滑", "往下", "向下", "向下滚", "往下滚", "下滚", "继续往下", "scroll down", "down") {
		return "down"
	}
	if containsNormalizedPhrase(key, "下滑", "往上", "向上", "向上滚", "往上滚", "上滚", "继续往上", "scroll up", "up") {
		return "up"
	}
	return ""
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
	if looksLikeBusinessMutationInstruction(text) || looksLikeNegatedSystemControl(text) {
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
	if looksLikeBusinessMutationInstruction(text) || looksLikeNegatedSystemControl(text) {
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
	if looksLikeBusinessMutationInstruction(lower) || looksLikeNegatedSystemControl(text) {
		return InteractionCommand{}, false
	}
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

func looksLikeBusinessMutationInstruction(text string) bool {
	key := normalizeForCompare(text)
	if key == "" {
		return false
	}
	hasBusinessTarget := containsNormalizedPhrase(key,
		"待办", "任务", "决策", "风险", "待确认问题", "待确认", "动态模块", "自定义模块", "模块", "摘要", "总结", "会议标题")
	hasMutationVerb := containsNormalizedPhrase(key,
		"新增", "添加", "补充", "创建", "删除", "删掉", "去掉", "移除", "修改", "改成", "改为",
		"调整为", "更新为", "设置为", "设为", "合并", "关闭", "完成", "解决", "负责人", "截止", "方案")
	return hasBusinessTarget && hasMutationVerb
}

func parseSummaryRewriteLocally(instruction string) (InteractionCommand, bool) {
	text := strings.TrimSpace(instruction)
	if text == "" {
		return InteractionCommand{}, false
	}
	key := normalizeForCompare(text)
	if !containsNormalizedPhrase(key, "摘要", "总结", "会议纪要") {
		return InteractionCommand{}, false
	}
	if containsNormalizedPhrase(key, "简短", "精简", "短一点", "短一些", "简洁", "压缩", "浓缩") {
		return InteractionCommand{
			Intent:      "rewrite_summary_section",
			TargetType:  "summary",
			Content:     "精简摘要",
			Confidence:  0.86,
			NeedConfirm: false,
			Reply:       "已识别为精简会议摘要。",
			Reason:      "make_concise",
		}, true
	}
	if !containsNormalizedPhrase(key, "改成", "改为", "写成", "重写", "重新写", "调整为", "更新为") {
		return InteractionCommand{}, false
	}
	content := textAfterAnyMarker(text, []string{
		"摘要改成", "摘要改为", "摘要写成", "摘要调整为", "摘要更新为",
		"总结改成", "总结改为", "总结写成", "总结调整为", "总结更新为",
		"重写成", "重写为", "重新写成", "改成", "改为", "写成", "调整为", "更新为",
	})
	content = cleanupDynamicSectionValue(content)
	if content == "" {
		return InteractionCommand{}, false
	}
	return InteractionCommand{
		Intent:      "rewrite_summary_section",
		TargetType:  "summary",
		Content:     content,
		Confidence:  0.84,
		NeedConfirm: false,
		Reply:       "已识别为改写会议摘要。",
	}, true
}

type frameworkSectionLocalMatch struct {
	Index   int
	Section MeetingFrameworkSection
	Phrase  string
	Start   int
	End     int
}

func parseFrameworkSectionMutationLocally(instruction string, snapshot *IntelligenceSnapshot) (InteractionCommand, bool) {
	if !meetingFrameworkFeatureEnabled() {
		return InteractionCommand{}, false
	}
	text := strings.TrimSpace(instruction)
	if text == "" || snapshot == nil || len(snapshot.FrameworkSections) == 0 {
		return InteractionCommand{}, false
	}
	match, ok := matchFrameworkSectionFromInstruction(text, snapshot.FrameworkSections)
	if !ok {
		return InteractionCommand{}, false
	}

	mode := detectFrameworkMutationMode(text)
	if mode == "" && instructionHasContentSeparatorAfter(text, match.End) {
		mode = "replace"
	}
	if mode == "" {
		return InteractionCommand{}, false
	}

	intent := "update_framework_section"
	switch mode {
	case "append":
		intent = "append_framework_section"
	case "clear":
		intent = "clear_framework_section"
	case "rewrite":
		intent = "rewrite_framework_section"
	}

	content := ""
	if mode != "clear" {
		content = extractFrameworkSectionContent(text, match, mode)
		if content == "" {
			return InteractionCommand{}, false
		}
	}

	return InteractionCommand{
		Intent:       intent,
		TargetType:   "framework_section",
		TargetIndex:  match.Index,
		TargetID:     match.Section.ID,
		SectionKey:   match.Section.SectionKey,
		SectionTitle: match.Section.Title,
		Content:      content,
		Confidence:   0.86,
		NeedConfirm:  false,
		Reply:        frameworkSectionLocalReply(intent, match.Section.Title),
	}, true
}

func matchFrameworkSectionFromInstruction(text string, sections []MeetingFrameworkSection) (frameworkSectionLocalMatch, bool) {
	best := frameworkSectionLocalMatch{}
	bestScore := -1
	for index, section := range sections {
		for _, phrase := range frameworkSectionVoiceCandidates(section) {
			pos := strings.Index(text, phrase)
			if pos < 0 {
				continue
			}
			score := len([]rune(normalizeForCompare(phrase))) * 10
			if strings.TrimSpace(phrase) == strings.TrimSpace(section.Title) {
				score += 5
			}
			if score <= bestScore {
				continue
			}
			bestScore = score
			best = frameworkSectionLocalMatch{
				Index:   index + 1,
				Section: section,
				Phrase:  phrase,
				Start:   pos,
				End:     pos + len(phrase),
			}
		}
	}
	return best, bestScore > 0
}

func frameworkSectionVoiceCandidates(section MeetingFrameworkSection) []string {
	title := strings.TrimSpace(section.Title)
	candidates := []string{
		title,
		strings.ReplaceAll(title, "/", ""),
		strings.ReplaceAll(title, "与", "和"),
	}
	switch section.SectionKey {
	case "background":
		candidates = append(candidates, "当前项目背景", "事项背景", "项目背景")
	case "meeting_goal":
		candidates = append(candidates, "会议目标", "本次目标")
	case "current_progress":
		candidates = append(candidates, "项目进展", "事项进展")
	case "confirmed_items":
		candidates = append(candidates, "确认事项", "已确认内容")
	case "next_followup":
		candidates = append(candidates, "跟进节点", "下次跟进")
	case "discussion_background":
		candidates = append(candidates, "讨论背景", "探索背景")
	case "core_question":
		candidates = append(candidates, "核心问题", "核心议题")
	case "proposed_ideas":
		candidates = append(candidates, "提出想法", "已提出方案", "已有想法", "想法列表")
	case "supporting_reasons":
		candidates = append(candidates, "支持理由", "支持意见", "支持点")
	case "opposing_risks":
		candidates = append(candidates, "反对意见", "反对理由", "反对风险", "风险模块", "风险部分")
	case "hypotheses_to_validate":
		candidates = append(candidates, "验证假设", "待验证假设", "待验证内容")
	case "next_exploration_actions":
		candidates = append(candidates, "探索动作", "下一步探索", "下一步探索动作")
	case "goal_vs_result":
		candidates = append(candidates, "目标和实际结果", "实际结果", "目标结果")
	case "positives":
		candidates = append(candidates, "做得好的地方", "做得好", "亮点")
	case "exposed_problems":
		candidates = append(candidates, "暴露的问题", "暴露问题", "复盘问题")
	case "possible_causes":
		candidates = append(candidates, "可能原因", "原因分析")
	case "improvement_plan":
		candidates = append(candidates, "改进方案", "改进计划")
	case "follow_up_actions":
		candidates = append(candidates, "后续行动", "后续动作")
	case "lessons_learned":
		candidates = append(candidates, "可沉淀经验", "沉淀经验", "经验沉淀")
	case "timeline_summary":
		candidates = append(candidates, "时间线摘要", "时间线")
	case "key_points":
		candidates = append(candidates, "关键观点", "重点观点")
	case "unresolved_issues":
		candidates = append(candidates, "未解决问题", "遗留问题", "未闭环问题")
	}
	return uniqueNonEmptyStrings(candidates)
}

func detectFrameworkMutationMode(text string) string {
	key := normalizeForCompare(text)
	if containsNormalizedPhrase(key, "清空", "删掉", "删除", "去掉", "移除") {
		return "clear"
	}
	if containsNormalizedPhrase(key, "补充", "追加", "加上", "增加", "再加", "再补", "写上", "记一下", "记录到") {
		return "append"
	}
	if containsNormalizedPhrase(key, "重写", "重新写", "重新整理") {
		return "rewrite"
	}
	if containsNormalizedPhrase(key, "改成", "改为", "设置为", "设为", "更新为", "写成", "调整为", "替换为", "记为", "定为", "改一下") {
		return "replace"
	}
	return ""
}

func instructionHasContentSeparatorAfter(text string, offset int) bool {
	if offset < 0 || offset >= len(text) {
		return false
	}
	tail := strings.TrimSpace(text[offset:])
	return strings.HasPrefix(tail, "：") || strings.HasPrefix(tail, ":")
}

func extractFrameworkSectionContent(text string, match frameworkSectionLocalMatch, mode string) string {
	if match.End < 0 || match.End > len(text) {
		return ""
	}
	tail := strings.TrimSpace(text[match.End:])
	tail = trimFrameworkContentLead(tail)
	prefixes := frameworkContentVerbPrefixes(mode)
	changed := true
	for changed {
		changed = false
		for _, prefix := range prefixes {
			if strings.HasPrefix(tail, prefix) {
				tail = trimFrameworkContentLead(strings.TrimPrefix(tail, prefix))
				changed = true
			}
		}
	}
	return cleanupParsedFrameworkContent(tail)
}

func trimFrameworkContentLead(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimLeft(value, "，,。；;：: ")
	leadWords := []string{"的内容是", "模块内容是", "这一栏是", "这个模块是", "模块是", "部分是", "内容是", "的内容", "模块内容", "这一栏", "这个模块", "模块", "部分", "内容", "为", "成", "到"}
	changed := true
	for changed {
		changed = false
		for _, word := range leadWords {
			if strings.HasPrefix(value, word) {
				value = strings.TrimSpace(strings.TrimPrefix(value, word))
				value = strings.TrimLeft(value, "，,。；;：: ")
				changed = true
			}
		}
	}
	return value
}

func frameworkContentVerbPrefixes(mode string) []string {
	switch mode {
	case "append":
		return []string{"补充一下", "再补充", "补充为", "补充", "追加为", "追加", "加上", "增加", "再加上", "再加", "写上", "记一下", "记录到", "记录为", "记录成"}
	case "rewrite":
		return []string{"重写为", "重写成", "重新写为", "重新整理为", "重写"}
	default:
		return []string{"改成", "改为", "设置为", "设为", "更新为", "写成", "调整为", "替换为", "记为", "定为", "改一下", "改"}
	}
}

func cleanupParsedFrameworkContent(value string) string {
	value = strings.TrimSpace(value)
	value = strings.Trim(value, "，,。；;：: ")
	suffixes := []string{"谢谢", "就这样", "可以吗", "可以了", "先这样"}
	for _, suffix := range suffixes {
		value = strings.TrimSuffix(value, suffix)
		value = strings.TrimSpace(value)
		value = strings.Trim(value, "，,。；;：: ")
	}
	return value
}

func frameworkSectionLocalReply(intent, title string) string {
	switch intent {
	case "append_framework_section":
		return fmt.Sprintf("已识别为补充框架模块：%s。", title)
	case "clear_framework_section":
		return fmt.Sprintf("已识别为清空框架模块：%s。", title)
	case "rewrite_framework_section":
		return fmt.Sprintf("已识别为重写框架模块：%s。", title)
	default:
		return fmt.Sprintf("已识别为更新框架模块：%s。", title)
	}
}

func parseDynamicSectionDeleteMergeLocally(instruction string, snapshot *IntelligenceSnapshot) (InteractionCommand, bool) {
	text := strings.TrimSpace(instruction)
	if text == "" || snapshot == nil || len(snapshot.DynamicSections) == 0 {
		return InteractionCommand{}, false
	}
	key := normalizeForCompare(text)
	if !containsNormalizedPhrase(key, "动态模块", "自定义模块", "发现模块", "模块", "栏目", "板块", "专题", "section", "module") {
		return InteractionCommand{}, false
	}

	if containsNormalizedPhrase(key, "合并", "归并", "并到一起", "整合") {
		sourceText := dynamicSectionMergeSourceText(text)
		sourceKey := normalizeForCompare(sourceText)
		ids := dynamicSectionIDsMentionedInInstruction(sourceKey, snapshot.DynamicSections)
		indices := parseAllDynamicSectionOrdinals(sourceText)
		for _, index := range indices {
			if index > 0 && index <= len(snapshot.DynamicSections) {
				ids = append(ids, snapshot.DynamicSections[index-1].ID)
			}
		}
		ids = uniqueNonEmptyStrings(ids)
		if len(ids) < 2 {
			return InteractionCommand{}, false
		}
		sourceIndices := make([]int, 0, len(ids))
		for _, id := range ids {
			for index, section := range snapshot.DynamicSections {
				if section.ID == id {
					sourceIndices = append(sourceIndices, index+1)
					break
				}
			}
		}
		title := extractDynamicSectionMergeTitle(text)
		return InteractionCommand{
			Intent:        "merge_dynamic_sections",
			TargetType:    "dynamic_section",
			SourceIDs:     ids,
			SourceIndices: sourceIndices,
			SectionTitle:  title,
			Title:         title,
			Confidence:    0.86,
			NeedConfirm:   false,
			Reply:         fmt.Sprintf("已识别为合并 %d 个动态模块。", len(ids)),
		}, true
	}

	if containsNormalizedPhrase(key, "删除", "删掉", "去掉", "移除") {
		targetIndex, targetSection := matchDynamicSectionFromInstruction(key, text, snapshot.DynamicSections)
		if targetSection == nil {
			return InteractionCommand{}, false
		}
		return InteractionCommand{
			Intent:       "delete_dynamic_section",
			TargetType:   "dynamic_section",
			TargetIndex:  targetIndex,
			TargetID:     targetSection.ID,
			SectionKey:   targetSection.SectionKey,
			SectionTitle: targetSection.Title,
			Confidence:   0.88,
			NeedConfirm:  false,
			Reply:        fmt.Sprintf("已识别为删除动态模块：%s。", targetSection.Title),
		}, true
	}
	return InteractionCommand{}, false
}

func dynamicSectionMergeSourceText(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	mergeIndex, mergeMarker := firstTextMarker(text, []string{"合并", "归并", "并到一起", "整合"})
	if mergeIndex < 0 {
		return text
	}

	// 合并命令里，“合并”前面通常是来源模块，“合并/改名为”后面通常是结果描述。
	// 只用来源片段匹配目标，避免新标题误命中其它已有模块。
	before := strings.TrimSpace(text[:mergeIndex])
	if dynamicSectionMergeSourceLooksExplicit(before) {
		return before
	}

	after := strings.TrimSpace(text[mergeIndex+len(mergeMarker):])
	after = textBeforeAnyMarker(after, []string{
		"改名为", "重命名为", "命名为",
		"名字改成", "名字改为", "名称改成", "名称改为", "标题改成", "标题改为",
		"合并为", "合并成", "整合为", "归并为", "叫做", "叫",
	})
	if strings.TrimSpace(after) != "" {
		return after
	}
	return before
}

func dynamicSectionMergeSourceLooksExplicit(text string) bool {
	if len(parseAllDynamicSectionOrdinals(text)) > 0 {
		return true
	}
	key := normalizeForCompare(text)
	return containsNormalizedPhrase(key, "动态模块", "自定义模块", "发现模块", "模块", "栏目", "板块", "专题")
}

func extractDynamicSectionMergeTitle(text string) string {
	title := extractDynamicSectionTitleAfterMarkers(text, []string{
		"改名为", "重命名为", "命名为",
		"名字改成", "名字改为", "名称改成", "名称改为", "标题改成", "标题改为",
		"叫做", "叫",
		"合并为", "合并成", "整合为", "归并为",
	})
	switch normalizeForCompare(title) {
	case normalizeForCompare("一条"), normalizeForCompare("一项"), normalizeForCompare("一个"), normalizeForCompare("一个模块"), normalizeForCompare("一个动态模块"):
		return ""
	default:
		return title
	}
}

func isDynamicSectionRenameInstruction(key string) bool {
	return containsNormalizedPhrase(key, "改名", "重命名", "命名为", "名称", "名字", "标题", "叫做")
}

func extractDynamicSectionRenameTitle(text string) string {
	return extractDynamicSectionTitleAfterMarkers(text, []string{
		"改名为", "重命名为", "命名为",
		"名字改成", "名字改为", "名称改成", "名称改为", "标题改成", "标题改为",
		"叫做",
	})
}

func extractDynamicSectionTitleAfterMarkers(text string, markers []string) string {
	value := textAfterAnyMarker(text, markers)
	if value == "" {
		return ""
	}
	value = textBeforeAnyMarker(value, []string{"，", "。", "；", ";", ",", ".", "！", "?", "？"})
	return cleanupDynamicSectionTitle(value)
}

func parseDynamicSectionMutationLocally(instruction string, snapshot *IntelligenceSnapshot) (InteractionCommand, bool) {
	text := strings.TrimSpace(instruction)
	if text == "" {
		return InteractionCommand{}, false
	}
	key := normalizeForCompare(text)
	mentionsModule := containsNormalizedPhrase(key, "动态模块", "自定义模块", "发现模块", "模块", "栏目", "板块", "专题", "section", "module")
	if !mentionsModule {
		return InteractionCommand{}, false
	}

	sections := []MeetingDynamicSection{}
	if snapshot != nil {
		sections = snapshot.DynamicSections
	}

	isCreateDynamicSection := containsNormalizedPhrase(key, "新增", "创建", "新建", "添加")
	if !isCreateDynamicSection && containsNormalizedPhrase(key, "修改", "更新", "调整", "改一下", "改成", "改为", "重写", "改名", "重命名", "命名为") {
		targetIndex, targetSection := matchDynamicSectionFromInstruction(key, text, sections)
		if targetSection == nil {
			return InteractionCommand{}, false
		}
		if parseDynamicSectionItemOrdinal(text) == 0 && isDynamicSectionRenameInstruction(key) {
			title := extractDynamicSectionRenameTitle(text)
			if title == "" {
				return InteractionCommand{}, false
			}
			return InteractionCommand{
				Intent:       "update_dynamic_section",
				TargetType:   "dynamic_section",
				TargetIndex:  targetIndex,
				TargetID:     targetSection.ID,
				SectionKey:   targetSection.SectionKey,
				SectionTitle: targetSection.Title,
				Title:        title,
				Confidence:   0.86,
				NeedConfirm:  false,
				Reply:        fmt.Sprintf("识别为将动态模块「%s」改名为「%s」。", targetSection.Title, title),
			}, true
		}
		content := extractDynamicSectionUpdateContent(text)
		if content == "" {
			return InteractionCommand{}, false
		}
		if itemIndex := parseDynamicSectionItemOrdinal(text); itemIndex > 0 {
			return InteractionCommand{
				Intent:        "update_dynamic_section_item",
				TargetType:    "dynamic_section",
				TargetIndex:   targetIndex,
				TargetID:      targetSection.ID,
				SourceIndices: []int{itemIndex},
				SectionKey:    targetSection.SectionKey,
				SectionTitle:  targetSection.Title,
				Content:       content,
				Confidence:    0.86,
				NeedConfirm:   false,
				Reply:         fmt.Sprintf("识别为修改动态模块「%s」第 %d 条。", targetSection.Title, itemIndex),
			}, true
		}
		if containsNormalizedPhrase(key, "描述", "说明", "介绍", "模块内容", "模块说明") {
			return InteractionCommand{
				Intent:       "update_dynamic_section",
				TargetType:   "dynamic_section",
				TargetIndex:  targetIndex,
				TargetID:     targetSection.ID,
				SectionKey:   targetSection.SectionKey,
				SectionTitle: targetSection.Title,
				Content:      content,
				Confidence:   0.84,
				NeedConfirm:  false,
				Reply:        fmt.Sprintf("识别为修改动态模块「%s」的描述。", targetSection.Title),
			}, true
		}
	}

	if containsNormalizedPhrase(key, "加一条", "添加一条", "补充一条", "追加一条", "记录一条", "写一条") {
		targetIndex, targetSection := matchDynamicSectionFromInstruction(key, text, sections)
		if targetSection == nil {
			return InteractionCommand{}, false
		}
		content := extractDynamicSectionItemContent(text)
		if content == "" {
			return InteractionCommand{}, false
		}
		return InteractionCommand{
			Intent:       "add_dynamic_section_item",
			TargetType:   "dynamic_section",
			TargetIndex:  targetIndex,
			TargetID:     targetSection.ID,
			SectionKey:   targetSection.SectionKey,
			SectionTitle: targetSection.Title,
			Content:      content,
			Confidence:   0.88,
			NeedConfirm:  false,
			Reply:        fmt.Sprintf("已识别为向动态模块「%s」追加内容。", targetSection.Title),
		}, true
	}

	if !isCreateDynamicSection {
		return InteractionCommand{}, false
	}
	title := extractDynamicSectionTitle(text)
	if title == "" {
		return InteractionCommand{}, false
	}
	content := extractDynamicSectionCreateContent(text)
	if normalizeForCompare(content) == normalizeForCompare(title) {
		content = ""
	}
	return InteractionCommand{
		Intent:       "create_dynamic_section",
		TargetType:   "dynamic_section",
		SectionTitle: title,
		Title:        title,
		Content:      content,
		Confidence:   0.86,
		NeedConfirm:  false,
		Reply:        fmt.Sprintf("已识别为新增动态模块：%s。", title),
	}, true
}

func extractDynamicSectionTitle(text string) string {
	if title := extractDynamicSectionTitleAfterMarkers(text, []string{
		"动态模块叫做", "动态模块叫", "动态模块命名为", "动态模块名字叫", "动态模块名称叫",
		"自定义模块叫做", "自定义模块叫", "自定义模块命名为", "自定义模块名字叫", "自定义模块名称叫",
		"发现模块叫做", "发现模块叫", "发现模块命名为", "发现模块名字叫", "发现模块名称叫",
		"模块叫做", "模块叫", "模块命名为", "模块名字叫", "模块名称叫",
		"栏目叫做", "栏目叫", "栏目命名为", "板块叫做", "板块叫", "专题叫做", "专题叫",
	}); title != "" {
		return title
	}
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`(?:新增|创建|新建|添加)(?:一个|一条|1个|个)?(?:叫做|叫|命名为|名字叫|名称叫)(.+?)(?:的)?(?:动态模块|自定义模块|发现模块|模块|栏目|板块|专题)?$`),
		regexp.MustCompile(`(?:新增|创建|新建|添加)(?:一个|1个|个)?(.+?)(?:动态模块|自定义模块|发现模块|模块|栏目|板块|专题)`),
		regexp.MustCompile(`(?:把|将)(.+?)(?:作为|设为|做成)(?:动态模块|自定义模块|发现模块|模块|栏目|板块|专题)`),
	}
	for _, pattern := range patterns {
		matches := pattern.FindStringSubmatch(text)
		if len(matches) >= 2 {
			if title := cleanupDynamicSectionTitle(matches[1]); title != "" && !isGenericDynamicSectionTitle(title) {
				return title
			}
		}
	}
	return ""
}

func cleanupDynamicSectionTitle(value string) string {
	value = strings.TrimSpace(value)
	value = strings.Trim(value, "，。！？、；;,.!?：: ")
	for _, prefix := range []string{"在里面", "里面"} {
		value = strings.TrimPrefix(value, prefix)
		value = strings.TrimSpace(value)
		value = strings.Trim(value, "，。！？、；;,.!?：: ")
	}
	for _, suffix := range []string{"动态模块", "自定义模块", "发现模块", "模块", "栏目", "板块", "专题", "谢谢", "就这样", "先这样"} {
		value = strings.TrimSuffix(value, suffix)
		value = strings.TrimSpace(value)
		value = strings.Trim(value, "，。！？、；;,.!?：: ")
	}
	for _, prefix := range []string{"叫做", "叫", "命名为", "名字叫", "名称叫", "标题叫"} {
		value = strings.TrimPrefix(value, prefix)
		value = strings.TrimSpace(value)
		value = strings.Trim(value, "，。！？、；;,.!?：: ")
	}
	return value
}

func isGenericDynamicSectionTitle(value string) bool {
	key := normalizeForCompare(value)
	switch key {
	case "", "动态", "自定义", "发现", "模块", "动态模块", "自定义模块", "发现模块", "栏目", "板块", "专题", "一个", "一条":
		return true
	default:
		return false
	}
}

func extractDynamicSectionCreateContent(text string) string {
	content := textAfterAnyMarker(text, []string{"内容是", "内容为", "里面写", "先写", "先记", "记录一下", "记录", "包括"})
	if content != "" {
		return cleanupDynamicSectionValue(content)
	}
	for _, marker := range []string{"动态模块", "自定义模块", "发现模块", "模块", "栏目", "板块", "专题"} {
		if index := strings.LastIndex(text, marker); index >= 0 {
			tail := strings.TrimSpace(text[index+len(marker):])
			tail = strings.TrimLeft(tail, "，。！？、；;,.!?：: ")
			if startsWithAny(tail, "叫做", "叫", "命名为", "名字叫", "名称叫", "标题叫") {
				return ""
			}
			return cleanupDynamicSectionValue(tail)
		}
	}
	return ""
}

func startsWithAny(value string, prefixes ...string) bool {
	for _, prefix := range prefixes {
		if strings.HasPrefix(value, prefix) {
			return true
		}
	}
	return false
}

func extractDynamicSectionItemContent(text string) string {
	content := textAfterAnyMarker(text, []string{"加一条", "添加一条", "补充一条", "追加一条", "记录一条", "写一条", "加上", "补充", "追加", "记录一下", "记录"})
	if content != "" {
		return cleanupDynamicSectionValue(content)
	}
	return cleanupDynamicSectionValue(text)
}

func extractDynamicSectionUpdateContent(text string) string {
	markers := []string{
		"描述改成",
		"描述改为",
		"说明改成",
		"说明改为",
		"内容改成",
		"内容改为",
		"修改为",
		"更新为",
		"调整为",
		"重写成",
		"改成",
		"改为",
		"写成",
		"记成",
	}
	if content := textAfterAnyMarker(text, markers); content != "" {
		return cleanupDynamicSectionValue(content)
	}
	return ""
}

func parseDynamicSectionItemOrdinal(text string) int {
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`第\s*([0-9]+|[一二两三四五六七八九十]{1,3})\s*(?:条|项)`),
		regexp.MustCompile(`([0-9]+|[一二两三四五六七八九十]{1,3})\s*(?:条|项)`),
	}
	for _, pattern := range patterns {
		matches := pattern.FindStringSubmatch(text)
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

func textAfterAnyMarker(text string, markers []string) string {
	for _, marker := range markers {
		if index := strings.Index(text, marker); index >= 0 {
			return text[index+len(marker):]
		}
	}
	return ""
}

func textBeforeAnyMarker(text string, markers []string) string {
	cut := -1
	for _, marker := range markers {
		if index := strings.Index(text, marker); index >= 0 && (cut < 0 || index < cut) {
			cut = index
		}
	}
	if cut >= 0 {
		return text[:cut]
	}
	return text
}

func firstTextMarker(text string, markers []string) (int, string) {
	bestIndex := -1
	bestMarker := ""
	for _, marker := range markers {
		if index := strings.Index(text, marker); index >= 0 && (bestIndex < 0 || index < bestIndex) {
			bestIndex = index
			bestMarker = marker
		}
	}
	return bestIndex, bestMarker
}

func cleanupDynamicSectionValue(value string) string {
	value = strings.TrimSpace(value)
	value = strings.Trim(value, "，。！？、；;,.!?：: ")
	for _, prefix := range []string{"在里面", "里面"} {
		value = strings.TrimPrefix(value, prefix)
		value = strings.TrimSpace(value)
		value = strings.Trim(value, "，。！？、；;,.!?：: ")
	}
	for _, suffix := range []string{"谢谢", "就这样", "先这样"} {
		value = strings.TrimSuffix(value, suffix)
		value = strings.TrimSpace(value)
		value = strings.Trim(value, "，。！？、；;,.!?：: ")
	}
	return value
}

func parseDynamicSectionVisibilityLocally(instruction string, snapshot *IntelligenceSnapshot) (InteractionCommand, bool) {
	text := strings.TrimSpace(instruction)
	if text == "" {
		return InteractionCommand{}, false
	}
	key := normalizeForCompare(text)
	show := containsNormalizedPhrase(key, "展示", "显示", "打开", "展开", "调出", "show", "open")
	hide := containsNormalizedPhrase(key, "隐藏", "收起", "关闭", "关掉", "hide", "close")
	if !show && !hide {
		return InteractionCommand{}, false
	}

	sections := []MeetingDynamicSection{}
	if snapshot != nil {
		sections = snapshot.DynamicSections
	}
	mentionsModule := containsNormalizedPhrase(key, "动态模块", "发现模块", "自定义模块", "模块", "专题", "栏目", "板块", "section", "module")
	targetIndex, targetSection := matchDynamicSectionFromInstruction(key, text, sections)
	if !mentionsModule && targetSection == nil {
		return InteractionCommand{}, false
	}

	if containsNormalizedPhrase(key, "全部", "所有", "都", "all") {
		intent := "show_all_dynamic_sections"
		reply := "已识别为展示全部动态模块。"
		if hide {
			intent = "hide_all_dynamic_sections"
			reply = "已识别为隐藏全部动态模块。"
		}
		return InteractionCommand{
			Intent:      intent,
			TargetType:  "dynamic_section",
			Confidence:  0.86,
			NeedConfirm: false,
			Reply:       reply,
		}, true
	}

	if targetSection == nil {
		return InteractionCommand{}, false
	}

	intent := "show_dynamic_section"
	reply := fmt.Sprintf("已识别为展示动态模块：%s。", targetSection.Title)
	if hide {
		intent = "hide_dynamic_section"
		reply = fmt.Sprintf("已识别为隐藏动态模块：%s。", targetSection.Title)
	}
	return InteractionCommand{
		Intent:       intent,
		TargetType:   "dynamic_section",
		TargetIndex:  targetIndex,
		TargetID:     targetSection.ID,
		SectionKey:   targetSection.SectionKey,
		SectionTitle: targetSection.Title,
		Confidence:   0.88,
		NeedConfirm:  false,
		Reply:        reply,
	}, true
}

func parseOpenQuestionMutationLocally(instruction string, snapshot *IntelligenceSnapshot) (InteractionCommand, bool) {
	text := strings.TrimSpace(instruction)
	if text == "" {
		return InteractionCommand{}, false
	}
	key := normalizeForCompare(text)
	if containsNormalizedPhrase(key, "风险", "待办", "决策", "动态模块", "模块") {
		return InteractionCommand{}, false
	}
	if !containsNormalizedPhrase(key, "待确认问题", "待确认", "确认问题", "问题") {
		return InteractionCommand{}, false
	}

	questions := []MeetingOpenQuestion{}
	if snapshot != nil {
		questions = snapshot.OpenQuestions
	}

	if containsNormalizedPhrase(key, "新增", "添加", "补充", "创建", "记录") &&
		!containsNormalizedPhrase(key, "改成", "改为", "调整为", "更新为", "设置为", "设为") {
		content := extractOpenQuestionUpdateValue(text)
		if content == "" {
			content = cleanupDynamicSectionValue(textAfterAnyMarker(text, []string{"待确认问题", "确认问题", "问题"}))
		}
		if content == "" {
			return InteractionCommand{}, false
		}
		return InteractionCommand{
			Intent:      "create_open_question",
			TargetType:  "open_question",
			Question:    content,
			Content:     content,
			Confidence:  0.84,
			NeedConfirm: false,
			Reply:       "已识别为新增待确认问题。",
		}, true
	}

	sourceIndices := parseAllDynamicSectionOrdinals(text)
	if containsNormalizedPhrase(key, "合并") && len(sourceIndices) >= 2 && len(questions) > 0 {
		ids := []string{}
		for _, index := range sourceIndices {
			if index > 0 && index <= len(questions) {
				ids = append(ids, questions[index-1].ID)
			}
		}
		if len(uniqueNonEmptyStrings(ids)) >= 2 {
			content := extractOpenQuestionUpdateValue(text)
			return InteractionCommand{
				Intent:        "merge_open_questions",
				TargetType:    "open_question",
				SourceIndices: sourceIndices,
				SourceIDs:     ids,
				Question:      content,
				Content:       content,
				Confidence:    0.86,
				NeedConfirm:   false,
				Reply:         fmt.Sprintf("已识别为合并 %d 条待确认问题。", len(ids)),
			}, true
		}
	}

	if containsNormalizedPhrase(key, "删除", "删掉", "去掉", "移除") && len(questions) > 0 {
		deleteIndices := parseTargetOrdinals(text)
		if len(deleteIndices) >= 2 {
			ids := []string{}
			validIndices := []int{}
			for _, index := range deleteIndices {
				if index > 0 && index <= len(questions) {
					ids = append(ids, questions[index-1].ID)
					validIndices = append(validIndices, index)
				}
			}
			if len(uniqueNonEmptyStrings(ids)) >= 2 {
				return InteractionCommand{
					Intent:        "delete_open_questions",
					TargetType:    "open_question",
					SourceIndices: validIndices,
					SourceIDs:     ids,
					Confidence:    0.86,
					NeedConfirm:   false,
					Reply:         fmt.Sprintf("已识别为删除 %d 条待确认问题。", len(ids)),
				}, true
			}
		}
	}

	index := parseOrdinalIndex(text)
	if index <= 0 || index > len(questions) {
		return InteractionCommand{}, false
	}
	question := questions[index-1]
	if containsNormalizedPhrase(key, "删除", "删掉", "去掉", "移除") {
		return InteractionCommand{
			Intent:      "delete_open_question",
			TargetType:  "open_question",
			TargetIndex: index,
			TargetID:    question.ID,
			Confidence:  0.86,
			NeedConfirm: false,
			Reply:       fmt.Sprintf("已识别为删除第 %d 条待确认问题。", index),
		}, true
	}
	if containsNormalizedPhrase(key, "关闭", "完成", "解决", "确认了", "已确认", "不用确认", "不需要确认") {
		return InteractionCommand{
			Intent:      "close_open_question",
			TargetType:  "open_question",
			TargetIndex: index,
			TargetID:    question.ID,
			Confidence:  0.86,
			NeedConfirm: false,
			Reply:       fmt.Sprintf("已识别为关闭第 %d 条待确认问题。", index),
		}, true
	}
	value := extractOpenQuestionUpdateValue(text)
	if value == "" {
		return InteractionCommand{}, false
	}
	command := InteractionCommand{
		Intent:      "update_open_question",
		TargetType:  "open_question",
		TargetIndex: index,
		TargetID:    question.ID,
		Confidence:  0.86,
		NeedConfirm: false,
		Reply:       fmt.Sprintf("已识别为修改第 %d 条待确认问题。", index),
	}
	switch {
	case containsNormalizedPhrase(key, "负责人", "谁负责", "跟进人"):
		command.Owner = value
	case containsNormalizedPhrase(key, "截止", "时间", "日期", "确认时间"):
		command.DueDate = value
	case containsNormalizedPhrase(key, "状态"):
		command.Status = value
	default:
		command.Question = value
		command.Content = value
	}
	return command, true
}

func parseDecisionMutationLocally(instruction string, snapshot *IntelligenceSnapshot) (InteractionCommand, bool) {
	text := strings.TrimSpace(instruction)
	if text == "" {
		return InteractionCommand{}, false
	}
	key := normalizeForCompare(text)
	if !containsNormalizedPhrase(key, "决策", "决定", "确认事项") {
		return InteractionCommand{}, false
	}
	if isActionItemToDecisionInstruction(text) {
		return InteractionCommand{}, false
	}

	decisions := []MeetingDecision{}
	if snapshot != nil {
		decisions = snapshot.Decisions
	}

	if containsNormalizedPhrase(key, "新增", "添加", "补充", "创建", "记录") &&
		!containsNormalizedPhrase(key, "改成", "改为", "调整为", "更新为", "设置为", "设为") {
		content := extractDecisionUpdateValue(text)
		if content == "" {
			content = cleanupDynamicSectionValue(textAfterAnyMarker(text, []string{"决策", "决定", "确认事项"}))
		}
		if content == "" {
			return InteractionCommand{}, false
		}
		return InteractionCommand{
			Intent:      "create_decision",
			TargetType:  "decision",
			Content:     content,
			Confidence:  0.86,
			NeedConfirm: false,
			Reply:       "已识别为新增会议决策。",
		}, true
	}

	if containsNormalizedPhrase(key, "合并") && len(decisions) > 0 {
		sourceIndices := parseTargetOrdinals(text)
		ids := []string{}
		validIndices := []int{}
		for _, index := range sourceIndices {
			if index > 0 && index <= len(decisions) {
				ids = append(ids, decisions[index-1].ID)
				validIndices = append(validIndices, index)
			}
		}
		if len(uniqueNonEmptyStrings(ids)) >= 2 {
			content := extractDecisionMergeContent(text)
			return InteractionCommand{
				Intent:        "merge_decisions",
				TargetType:    "decision",
				SourceIndices: validIndices,
				SourceIDs:     ids,
				Content:       content,
				Confidence:    0.88,
				NeedConfirm:   false,
				Reply:         fmt.Sprintf("已识别为合并 %d 条会议决策。", len(ids)),
			}, true
		}
	}

	index := parseOrdinalIndex(text)
	if index <= 0 || index > len(decisions) {
		return InteractionCommand{}, false
	}
	decision := decisions[index-1]

	if containsNormalizedPhrase(key, "删除", "删掉", "去掉", "移除") {
		return InteractionCommand{
			Intent:      "delete_decision",
			TargetType:  "decision",
			TargetIndex: index,
			TargetID:    decision.ID,
			Confidence:  0.88,
			NeedConfirm: false,
			Reply:       fmt.Sprintf("已识别为删除第 %d 条会议决策。", index),
		}, true
	}

	if isDecisionToActionItemInstruction(text) {
		content := extractDecisionUpdateValue(text)
		return InteractionCommand{
			Intent:      "move_decision_to_action_item",
			TargetType:  "decision",
			TargetIndex: index,
			TargetID:    decision.ID,
			Task:        content,
			Content:     content,
			Confidence:  0.86,
			NeedConfirm: false,
			Reply:       fmt.Sprintf("已识别为将第 %d 条会议决策转为待办。", index),
		}, true
	}

	value := extractDecisionUpdateValue(text)
	if value == "" {
		return InteractionCommand{}, false
	}
	return InteractionCommand{
		Intent:      "update_decision",
		TargetType:  "decision",
		TargetIndex: index,
		TargetID:    decision.ID,
		Content:     value,
		Confidence:  0.9,
		NeedConfirm: false,
		Reply:       fmt.Sprintf("已识别为修改第 %d 条会议决策。", index),
	}, true
}

func isActionItemToDecisionInstruction(text string) bool {
	key := normalizeForCompare(text)
	if !containsNormalizedPhrase(key, "待办", "任务", "决策") || !containsNormalizedPhrase(key, "转为", "转成", "转换为", "移到", "改成") {
		return false
	}
	actionPos := firstNormalizedPhraseIndex(text, "待办", "任务")
	decisionPos := firstNormalizedPhraseIndex(text, "决策")
	return actionPos >= 0 && decisionPos >= 0 && actionPos < decisionPos
}

func isDecisionToActionItemInstruction(text string) bool {
	key := normalizeForCompare(text)
	if !containsNormalizedPhrase(key, "决策", "待办", "任务") || !containsNormalizedPhrase(key, "转为", "转成", "转换为", "移到", "改成") {
		return false
	}
	decisionPos := firstNormalizedPhraseIndex(text, "决策")
	actionPos := firstNormalizedPhraseIndex(text, "待办", "任务")
	return decisionPos >= 0 && actionPos >= 0 && decisionPos < actionPos
}

func firstNormalizedPhraseIndex(text string, phrases ...string) int {
	key := normalizeForCompare(text)
	best := -1
	for _, phrase := range phrases {
		index := strings.Index(key, normalizeForCompare(phrase))
		if index >= 0 && (best < 0 || index < best) {
			best = index
		}
	}
	return best
}

func extractDecisionUpdateValue(text string) string {
	markers := []string{
		"决策内容改成", "决策内容改为", "决策内容调整为", "决策内容更新为",
		"决策改成", "决策改为", "决策调整为", "决策更新为",
		"决定改成", "决定改为", "确认事项改成", "确认事项改为",
		"内容改成", "内容改为", "内容是", "内容为",
		"改成", "改为", "调整为", "更新为", "设置为", "设为",
		"新增一条决策", "新增一个决策", "新增决策",
		"添加一条决策", "添加一个决策", "添加决策",
		"补充一条决策", "补充一个决策", "补充决策",
		"创建一条决策", "创建一个决策", "创建决策",
		"记录一条决策", "记录一个决策", "记录决策",
		"决策是", "决策为", "决定是", "决定为",
	}
	return cleanupDynamicSectionValue(textAfterAnyMarker(text, markers))
}

func extractDecisionMergeContent(text string) string {
	content := textAfterAnyMarker(text, []string{
		"合并为", "合并成", "整合为", "整合成", "归并为", "归并成",
		"改成", "改为", "调整为", "更新为", "设置为", "设为",
	})
	return cleanupDynamicSectionValue(content)
}

func extractOpenQuestionUpdateValue(text string) string {
	markers := []string{
		"待确认问题改成", "待确认问题改为", "确认问题改成", "确认问题改为",
		"问题改成", "问题改为", "问题是", "问题为",
		"负责人改成", "负责人改为", "负责人是", "负责人为",
		"跟进人改成", "跟进人改为", "跟进人是", "跟进人为",
		"截止时间改成", "截止时间改为", "截止时间是", "截止时间为",
		"确认时间改成", "确认时间改为", "确认时间是", "确认时间为",
		"状态改成", "状态改为",
		"改成", "改为", "调整为", "更新为", "设置为", "设为",
	}
	return cleanupDynamicSectionValue(textAfterAnyMarker(text, markers))
}

func parseTargetOrdinals(text string) []int {
	pattern := regexp.MustCompile(`(?:第\s*)?([0-9]+|[一二两三四五六七八九十]{1,3})\s*(?:条|项|个)?`)
	matches := pattern.FindAllStringSubmatch(text, -1)
	values := make([]int, 0, len(matches))
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		raw := match[1]
		if number, err := strconv.Atoi(raw); err == nil {
			values = append(values, number)
			continue
		}
		if number := chineseNumber(raw); number > 0 {
			values = append(values, number)
		}
	}
	return positiveUniqueInts(values)
}

func parseRiskMutationLocally(instruction string, snapshot *IntelligenceSnapshot) (InteractionCommand, bool) {
	text := strings.TrimSpace(instruction)
	if text == "" || snapshot == nil || len(snapshot.Risks) == 0 {
		return InteractionCommand{}, false
	}
	key := normalizeForCompare(text)
	if containsNormalizedPhrase(key, "待办", "决策", "动态模块", "模块", "待确认") {
		return InteractionCommand{}, false
	}
	if !containsNormalizedPhrase(key, "风险", "方案", "处理方案", "预案", "影响", "等级", "级别", "负责人", "截止", "时间", "状态") {
		return InteractionCommand{}, false
	}
	index := parseOrdinalIndex(text)
	if index <= 0 || index > len(snapshot.Risks) {
		return InteractionCommand{}, false
	}
	risk := snapshot.Risks[index-1]
	if containsNormalizedPhrase(key, "关闭", "完成", "解决", "闭环", "处理完") {
		return InteractionCommand{
			Intent:      "close_risk",
			TargetType:  "risk",
			TargetIndex: index,
			TargetID:    risk.ID,
			Confidence:  0.86,
			NeedConfirm: false,
			Reply:       fmt.Sprintf("已识别为关闭第 %d 条风险。", index),
		}, true
	}
	value := extractRiskUpdateValue(text)
	if value == "" {
		return InteractionCommand{}, false
	}
	command := InteractionCommand{
		Intent:      "update_risk",
		TargetType:  "risk",
		TargetIndex: index,
		TargetID:    risk.ID,
		Confidence:  0.84,
		NeedConfirm: false,
		Reply:       fmt.Sprintf("已识别为修改第 %d 条风险。", index),
	}
	switch {
	case containsNormalizedPhrase(key, "方案", "处理方案", "预案", "措施"):
		command.Mitigation = value
	case containsNormalizedPhrase(key, "负责人"):
		command.Owner = value
	case containsNormalizedPhrase(key, "截止", "时间", "日期"):
		command.DueDate = value
	case containsNormalizedPhrase(key, "等级", "级别"):
		command.Level = value
	case containsNormalizedPhrase(key, "影响"):
		command.Impact = value
	case containsNormalizedPhrase(key, "状态"):
		command.Status = value
	default:
		command.Content = value
	}
	return command, true
}

func extractRiskUpdateValue(text string) string {
	markers := []string{
		"处理方案改成", "处理方案改为", "处理方案是", "处理方案为",
		"方案改成", "方案改为", "方案是", "方案为",
		"负责人改成", "负责人改为", "负责人是", "负责人为",
		"截止时间改成", "截止时间改为", "截止时间是", "截止时间为",
		"等级改成", "等级改为", "级别改成", "级别改为",
		"影响改成", "影响改为", "状态改成", "状态改为",
		"改成", "改为", "调整为", "更新为", "设置为", "设为", "是", "为",
	}
	value := textAfterAnyMarker(text, markers)
	return cleanupDynamicSectionValue(value)
}

func containsNormalizedPhrase(key string, phrases ...string) bool {
	for _, phrase := range phrases {
		normalized := normalizeForCompare(phrase)
		if normalized != "" && strings.Contains(key, normalized) {
			return true
		}
	}
	return false
}

func matchDynamicSectionFromInstruction(key, text string, sections []MeetingDynamicSection) (int, *MeetingDynamicSection) {
	if len(sections) == 0 {
		return 0, nil
	}
	for index := range sections {
		section := &sections[index]
		candidates := []string{section.Title, section.SectionKey}
		for _, candidate := range candidates {
			normalized := normalizeForCompare(candidate)
			if normalized == "" {
				continue
			}
			if strings.Contains(key, normalized) || strings.Contains(normalized, key) {
				return index + 1, section
			}
		}
	}
	if index := parseDynamicSectionOrdinal(text); index > 0 && index <= len(sections) {
		return index, &sections[index-1]
	}
	return 0, nil
}

func dynamicSectionIDsMentionedInInstruction(key string, sections []MeetingDynamicSection) []string {
	ids := []string{}
	for _, section := range sections {
		for _, candidate := range []string{section.Title, section.SectionKey} {
			normalized := normalizeForCompare(candidate)
			if normalized == "" {
				continue
			}
			if strings.Contains(key, normalized) {
				ids = append(ids, section.ID)
				break
			}
		}
	}
	return uniqueNonEmptyStrings(ids)
}

func parseAllDynamicSectionOrdinals(text string) []int {
	pattern := regexp.MustCompile(`第\s*([0-9]+|[一二两三四五六七八九十]{1,3})\s*(?:个|条|项)?`)
	matches := pattern.FindAllStringSubmatch(text, -1)
	values := make([]int, 0, len(matches))
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		if number, err := strconv.Atoi(match[1]); err == nil {
			values = append(values, number)
			continue
		}
		if number := chineseNumber(match[1]); number > 0 {
			values = append(values, number)
		}
	}
	return positiveUniqueInts(values)
}

func parseDynamicSectionOrdinal(text string) int {
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`第\s*([0-9]+|[一二两三四五六七八九十]{1,3})\s*(?:个)?(?:动态模块|自定义模块|发现模块|模块|栏目|板块|专题)`),
		regexp.MustCompile(`(?:动态模块|自定义模块|发现模块|模块|栏目|板块|专题)\s*第\s*([0-9]+|[一二两三四五六七八九十]{1,3})\s*(?:个)?`),
	}
	for _, pattern := range patterns {
		matches := pattern.FindStringSubmatch(text)
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
