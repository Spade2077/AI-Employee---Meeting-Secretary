// 本文件负责会中交互对会议摘要的局部改写，避免把“改写某一段”的指令误当成整篇摘要覆盖。
package main

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"
)

type summarySection struct {
	Key   string
	Label string
}

func (s *Server) rewriteSummaryByCommand(ctx context.Context, meetingID string, command InteractionCommand) (*MeetingSummary, error) {
	content := strings.TrimSpace(command.Content)
	if content == "" {
		return nil, errSummaryContentRequired()
	}
	snapshot, err := s.store.GetIntelligenceSnapshot(ctx, meetingID)
	if err != nil {
		return nil, err
	}
	existing := ""
	if snapshot != nil && snapshot.Summary != nil {
		existing = snapshot.Summary.Content
	}
	section := detectSummarySection(command)
	next := buildSummaryRewrite(existing, section, content)
	return s.store.RewriteSummary(ctx, meetingID, next)
}

func errSummaryContentRequired() error {
	return fmt.Errorf("summary content is required")
}

func detectSummarySection(command InteractionCommand) summarySection {
	text := strings.ToLower(command.Instruction + " " + command.Reason + " " + command.Content + " " + command.Task)
	switch {
	case containsAny(text, "风险", "risk"):
		return summarySection{Key: "risk", Label: "风险提示"}
	case containsAny(text, "缺失", "遗漏", "待确认", "待补充", "missing"):
		return summarySection{Key: "missing", Label: "缺失项提醒"}
	case containsAny(text, "待办", "任务", "action item", "todo"):
		return summarySection{Key: "action", Label: "待办事项"}
	case containsAny(text, "决策", "决定", "decision"):
		return summarySection{Key: "decision", Label: "会议决策"}
	default:
		return summarySection{}
	}
}

func buildSummaryRewrite(existing string, section summarySection, content string) string {
	existing = strings.TrimSpace(existing)
	replacement := normalizeSummaryReplacement(section, content)
	if existing == "" || section.Key == "" {
		return replacement
	}
	if replaced, ok := replaceSummarySection(existing, section, replacement); ok {
		return replaced
	}
	return existing + "\n" + replacement
}

func normalizeSummaryReplacement(section summarySection, content string) string {
	content = strings.TrimSpace(content)
	content = strings.Trim(content, " ，。；;,.")
	if section.Key != "" {
		content = stripSummaryInstructionPhrases(content)
		content = strings.Trim(content, " ，。；;,.")
		if !strings.HasPrefix(content, section.Label) {
			content = section.Label + "：" + content
		}
	}
	if content != "" && !endsWithSentencePunctuation(content) {
		content += "。"
	}
	return content
}

func stripSummaryInstructionPhrases(content string) string {
	cues := []string{"重点突出", "主要突出", "突出", "强调", "改成", "改为", "写成", "更新为", "调整为", "重写为", "重新写成"}
	for _, cue := range cues {
		if index := strings.LastIndex(content, cue); index >= 0 {
			content = content[index+len(cue):]
			break
		}
	}
	noise := []string{
		"会议摘要里", "会议摘要中", "摘要里", "摘要中", "关于风险那段", "风险那段",
		"关于缺失项那段", "缺失项那段", "重新写一下", "重写一下", "改写一下",
	}
	for _, value := range noise {
		content = strings.ReplaceAll(content, value, "")
	}
	return strings.TrimSpace(content)
}

func replaceSummarySection(existing string, section summarySection, replacement string) (string, bool) {
	lines := splitNonEmptyLines(existing)
	if len(lines) > 1 {
		for index, line := range lines {
			if summarySectionMatches(line, section.Key) {
				lines[index] = replacement
				return strings.Join(lines, "\n"), true
			}
		}
	}

	sentences := splitSummarySentences(existing)
	for index, sentence := range sentences {
		if summarySectionMatches(sentence, section.Key) {
			sentences[index] = replacement
			return strings.Join(sentences, ""), true
		}
	}
	return "", false
}

func splitNonEmptyLines(text string) []string {
	raw := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	lines := make([]string, 0, len(raw))
	for _, line := range raw {
		line = strings.TrimSpace(line)
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

func splitSummarySentences(text string) []string {
	sentences := []string{}
	var builder strings.Builder
	for _, r := range text {
		builder.WriteRune(r)
		if strings.ContainsRune("。！？!?；;", r) {
			value := strings.TrimSpace(builder.String())
			if value != "" {
				sentences = append(sentences, value)
			}
			builder.Reset()
		}
	}
	if strings.TrimSpace(builder.String()) != "" {
		sentences = append(sentences, strings.TrimSpace(builder.String()))
	}
	if len(sentences) == 0 && strings.TrimSpace(text) != "" {
		return []string{strings.TrimSpace(text)}
	}
	return sentences
}

func summarySectionMatches(text, key string) bool {
	text = strings.ToLower(text)
	switch key {
	case "risk":
		return containsAny(text, "风险", "risk")
	case "missing":
		return containsAny(text, "缺失", "遗漏", "待确认", "待补充", "missing")
	case "action":
		return containsAny(text, "待办", "任务", "action item", "todo")
	case "decision":
		return containsAny(text, "决策", "决定", "decision")
	default:
		return false
	}
}

func containsAny(text string, values ...string) bool {
	for _, value := range values {
		if strings.Contains(text, value) {
			return true
		}
	}
	return false
}

func endsWithSentencePunctuation(text string) bool {
	if text == "" {
		return false
	}
	last, _ := utf8.DecodeLastRuneInString(text)
	return strings.ContainsRune("。！？!?；;.", last)
}
