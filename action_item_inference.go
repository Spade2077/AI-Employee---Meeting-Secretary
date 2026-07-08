package main

import (
	"regexp"
	"strings"
	"unicode"
)

type actionItemFieldInference struct {
	Owner   string
	DueDate string
}

var (
	actionItemDueDatePattern    = regexp.MustCompile(`(?:\d{4}年)?\d{1,2}月\d{1,2}[日号]?(?:早上|上午|中午|下午|晚上)?(?:前|之前|以前)?|\d{1,2}[/-]\d{1,2}(?:前|之前|以前)?|(?:(?:本|这|下|下个)?(?:周|星期|礼拜)[一二三四五六日天])(?:早上|上午|中午|下午|晚上)?(?:前|之前|以前)?|(?:(?:本|这|下|下个)(?:周|星期|礼拜))(?:内|前|之前|以前)?|(?:今天|明天|后天|大后天)(?:早上|上午|中午|下午|晚上)?(?:前|之前|以前)?|(?:(?:本|这|下|下个)?月底)(?:前|之前|以前)?`)
	actionItemActionVerbPattern = regexp.MustCompile(`(?:看一下|跟进一下|确认一下|完成|处理|准备|整理|确认|跟进|联调|测试|排查|更新|修复|交付|推进|对接|验证|负责|输出|提交|同步|检查|解决|优化|补充|评估|梳理|发|给)`)
	leadingSpeakerLabelPattern  = regexp.MustCompile(`^[^:：，,。；;]{1,24}[:：]\s*`)
)

func completeActionItemExtractionFieldsFromText(item *ActionItemExtraction) {
	if item == nil {
		return
	}
	item.Task = strings.TrimSpace(item.Task)
	item.Owner = strings.TrimSpace(item.Owner)
	item.DueDate = strings.TrimSpace(item.DueDate)
	item.SourceText = strings.TrimSpace(item.SourceText)

	inferred := inferActionItemFields(item.Task, item.SourceText)
	if item.Owner == "" {
		item.Owner = inferred.Owner
	}
	if item.DueDate == "" {
		item.DueDate = inferred.DueDate
	}
}

func completeActionItemFieldsFromText(item *MeetingActionItem) {
	if item == nil {
		return
	}
	item.Task = strings.TrimSpace(item.Task)
	item.Owner = strings.TrimSpace(item.Owner)
	item.DueDate = strings.TrimSpace(item.DueDate)
	item.SourceText = strings.TrimSpace(item.SourceText)

	inferred := inferActionItemFields(item.Task, item.SourceText)
	if item.Owner == "" {
		item.Owner = inferred.Owner
	}
	if item.DueDate == "" {
		item.DueDate = inferred.DueDate
	}
}

func inferActionItemFields(task, sourceText string) actionItemFieldInference {
	var inferred actionItemFieldInference
	for _, text := range uniqueNonEmptyStrings([]string{task, sourceText}) {
		owner, dueDate := inferActionItemFieldsFromSingleText(text)
		if inferred.Owner == "" {
			inferred.Owner = owner
		}
		if inferred.DueDate == "" {
			inferred.DueDate = dueDate
		}
		if inferred.Owner != "" && inferred.DueDate != "" {
			return inferred
		}
	}
	return inferred
}

func inferActionItemFieldsFromSingleText(text string) (string, string) {
	text = normalizeActionItemInferenceText(text)
	if text == "" {
		return "", ""
	}
	dueDate, dateStart, dateEnd := inferActionItemDueDate(text)
	owner := inferActionItemOwner(text, dateStart, dateEnd)
	return owner, dueDate
}

func normalizeActionItemInferenceText(text string) string {
	text = strings.TrimSpace(text)
	text = leadingSpeakerLabelPattern.ReplaceAllString(text, "")
	return strings.TrimSpace(text)
}

func inferActionItemDueDate(text string) (string, int, int) {
	match := actionItemDueDatePattern.FindStringIndex(text)
	if match == nil {
		return "", -1, -1
	}
	return strings.TrimSpace(text[match[0]:match[1]]), match[0], match[1]
}

func inferActionItemOwner(text string, dateStart, dateEnd int) string {
	candidates := []string{}
	if dateStart > 0 {
		candidates = append(candidates, text[:dateStart])
	}
	if dateEnd > 0 && dateEnd < len(text) {
		tail := strings.TrimSpace(text[dateEnd:])
		if match := actionItemActionVerbPattern.FindStringIndex(tail); match != nil && match[0] > 0 {
			candidates = append(candidates, tail[:match[0]])
		}
	}
	if match := actionItemActionVerbPattern.FindStringIndex(text); match != nil && match[0] > 0 {
		candidates = append(candidates, text[:match[0]])
	}
	for _, candidate := range candidates {
		owner := cleanupActionItemOwnerCandidate(candidate)
		if isPlausibleActionItemOwner(owner) {
			return owner
		}
	}
	return ""
}

func cleanupActionItemOwnerCandidate(value string) string {
	value = strings.TrimSpace(value)
	value = strings.Trim(value, " \t\r\n，,。；;：:、")
	for {
		trimmed := value
		for _, prefix := range []string{
			"负责人是", "负责人为", "负责人", "负责同学", "责任人是", "责任人为", "责任人",
			"由", "请", "让", "麻烦你", "麻烦", "安排", "交给", "给", "需要",
		} {
			trimmed = strings.TrimSpace(strings.TrimPrefix(trimmed, prefix))
		}
		trimmed = strings.Trim(trimmed, " \t\r\n，,。；;：:、")
		for _, suffix := range []string{"来负责", "来牵头", "负责", "牵头", "同学", "来", "去", "这边"} {
			trimmed = strings.TrimSpace(strings.TrimSuffix(trimmed, suffix))
		}
		trimmed = strings.Trim(trimmed, " \t\r\n，,。；;：:、")
		if trimmed == value {
			return trimmed
		}
		value = trimmed
	}
}

func isPlausibleActionItemOwner(value string) bool {
	value = strings.TrimSpace(value)
	runes := []rune(value)
	if len(runes) < 2 || len(runes) > 8 {
		return false
	}
	key := normalizeForCompare(value)
	if key == "" {
		return false
	}
	for _, generic := range []string{
		"我们", "咱们", "大家", "团队", "客户", "现场", "系统", "模型", "页面", "项目", "会议",
		"这个", "那个", "这里", "那里", "这块", "那块", "我来", "你来", "他们", "它们",
	} {
		if key == normalizeForCompare(generic) {
			return false
		}
	}
	if containsNormalizedPhrase(key,
		"今天", "明天", "后天", "本周", "这周", "下周", "周一", "周二", "周三", "周四", "周五", "周六", "周日", "周天",
		"星期", "礼拜", "月底", "上午", "下午", "晚上",
		"完成", "处理", "准备", "整理", "确认", "跟进", "联调", "测试", "排查", "更新", "修复",
		"交付", "推进", "对接", "验证", "负责", "输出", "提交", "同步", "检查", "解决", "优化", "补充", "评估", "梳理") {
		return false
	}
	for _, r := range value {
		if unicode.IsLetter(r) {
			return true
		}
	}
	return false
}
