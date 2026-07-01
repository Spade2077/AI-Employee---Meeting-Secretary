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
	if command, ok := parseCorrectionLocally(instruction); ok {
		timing.addStage("local_parse", "本地解析指令", time.Since(localStarted), "未调用 LLM；本地口误纠错指令")
		command = normalizeInteractionCommand(command)
		bytes, _ := json.Marshal(command)
		return command, string(bytes), nil
	}
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

	if command, ok := parseSummaryRewriteLocally(instruction); ok {
		timing.addStage("local_parse", "本地解析指令", time.Since(localStarted), "未调用 LLM；本地摘要改写指令")
		command = normalizeInteractionCommand(command)
		bytes, _ := json.Marshal(command)
		return command, string(bytes), nil
	}

	if command, ok := parseFrameworkSectionMutationLocally(instruction, snapshot); ok {
		timing.addStage("local_parse", "本地解析指令", time.Since(localStarted), "未调用 LLM；本地固定框架模块修改指令")
		command = normalizeInteractionCommand(command)
		bytes, _ := json.Marshal(command)
		return command, string(bytes), nil
	}

	if command, ok := parseDynamicSectionDeleteMergeLocally(instruction, snapshot); ok {
		timing.addStage("local_parse", "本地解析指令", time.Since(localStarted), "未调用 LLM；本地动态模块删除/合并指令")
		command = normalizeInteractionCommand(command)
		bytes, _ := json.Marshal(command)
		return command, string(bytes), nil
	}

	if command, ok := parseDynamicSectionMutationLocally(instruction, snapshot); ok {
		timing.addStage("local_parse", "本地解析指令", time.Since(localStarted), "未调用 LLM；本地动态模块写入指令")
		command = normalizeInteractionCommand(command)
		bytes, _ := json.Marshal(command)
		return command, string(bytes), nil
	}

	if command, ok := parseDynamicSectionVisibilityLocally(instruction, snapshot); ok {
		timing.addStage("local_parse", "本地解析指令", time.Since(localStarted), "未调用 LLM；本地动态模块展示指令")
		command = normalizeInteractionCommand(command)
		bytes, _ := json.Marshal(command)
		return command, string(bytes), nil
	}

	if command, ok := parseOpenQuestionMutationLocally(instruction, snapshot); ok {
		timing.addStage("local_parse", "本地解析指令", time.Since(localStarted), "未调用 LLM；本地待确认问题修改指令")
		command = normalizeInteractionCommand(command)
		bytes, _ := json.Marshal(command)
		return command, string(bytes), nil
	}

	if command, ok := parseRiskMutationLocally(instruction, snapshot); ok {
		timing.addStage("local_parse", "本地解析指令", time.Since(localStarted), "未调用 LLM；本地风险修改指令")
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
	command.SectionKey = strings.TrimSpace(command.SectionKey)
	command.SectionTitle = strings.TrimSpace(command.SectionTitle)
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
	case "create_open_question", "update_open_question", "close_open_question", "delete_open_question", "merge_open_questions":
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
		ids := dynamicSectionIDsMentionedInInstruction(key, snapshot.DynamicSections)
		indices := parseAllDynamicSectionOrdinals(text)
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
		return InteractionCommand{
			Intent:        "merge_dynamic_sections",
			TargetType:    "dynamic_section",
			SourceIDs:     ids,
			SourceIndices: sourceIndices,
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

	if containsNormalizedPhrase(key, "修改", "更新", "调整", "改一下", "改成", "改为", "重写") {
		targetIndex, targetSection := matchDynamicSectionFromInstruction(key, text, sections)
		if targetSection == nil {
			return InteractionCommand{}, false
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

	if !containsNormalizedPhrase(key, "新增", "创建", "新建", "添加") {
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
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`(?:新增|创建|新建|添加)(?:一个|1个|个)?(.+?)(?:动态模块|自定义模块|发现模块|模块|栏目|板块|专题)`),
		regexp.MustCompile(`(?:把|将)(.+?)(?:作为|设为|做成)(?:动态模块|自定义模块|发现模块|模块|栏目|板块|专题)`),
	}
	for _, pattern := range patterns {
		matches := pattern.FindStringSubmatch(text)
		if len(matches) >= 2 {
			if title := cleanupDynamicSectionValue(matches[1]); title != "" {
				return title
			}
		}
	}
	return ""
}

func extractDynamicSectionCreateContent(text string) string {
	content := textAfterAnyMarker(text, []string{"内容是", "内容为", "里面写", "先写", "先记", "记录一下", "记录", "包括"})
	if content != "" {
		return cleanupDynamicSectionValue(content)
	}
	for _, marker := range []string{"动态模块", "自定义模块", "发现模块", "模块", "栏目", "板块", "专题"} {
		if index := strings.LastIndex(text, marker); index >= 0 {
			return cleanupDynamicSectionValue(text[index+len(marker):])
		}
	}
	return ""
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

func cleanupDynamicSectionValue(value string) string {
	value = strings.TrimSpace(value)
	value = strings.Trim(value, "，。！？、；;,.!?：: ")
	for _, prefix := range []string{"为", "是", "成", "到", "在里面", "里面"} {
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

func extractOpenQuestionUpdateValue(text string) string {
	markers := []string{
		"待确认问题改成", "待确认问题改为", "确认问题改成", "确认问题改为",
		"问题改成", "问题改为", "问题是", "问题为",
		"负责人改成", "负责人改为", "负责人是", "负责人为",
		"跟进人改成", "跟进人改为", "跟进人是", "跟进人为",
		"截止时间改成", "截止时间改为", "截止时间是", "截止时间为",
		"确认时间改成", "确认时间改为", "确认时间是", "确认时间为",
		"状态改成", "状态改为",
		"改成", "改为", "调整为", "更新为", "设置为", "设为", "是", "为",
	}
	return cleanupDynamicSectionValue(textAfterAnyMarker(text, markers))
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
