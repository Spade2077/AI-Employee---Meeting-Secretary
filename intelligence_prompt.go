// 本文件负责会议结构化 LLM 的提示词和上下文构造。
package main

import (
	"context"
	"encoding/json"
)

const intelligenceSystemPrompt = `你是会议结构化助手。请根据会议转写维护结构化会议结果。
只输出合法 JSON，不要输出解释、Markdown、代码块。
字段必须固定为：
{
  "summary": "100字以内的当前会议摘要",
  "decisions": [{"content": "已经明确达成的决定", "source_text": "对应原文"}],
  "action_items": [{"task": "任务内容", "owner": "负责人，没有则为空字符串", "due_date": "截止时间，没有则为空字符串", "status": "open", "source_text": "对应原文"}]
}
要求：
1. summary 可以基于全文更新。
2. decisions 和 action_items 只返回尚未出现在 existing_decisions / existing_action_items 中的新内容。
3. 不要编造负责人、截止时间或决定。
4. 没有新内容时，decisions 或 action_items 返回空数组。`

func (s *Server) callLLMForIntelligence(ctx context.Context, cfg *LLMConfig, transcript string, current *IntelligenceSnapshot) (*IntelligenceExtraction, error) {
	messages := buildIntelligenceMessages(transcript, current)
	content, err := s.llmClient.Complete(ctx, cfg, messages, 0.2)
	if err != nil {
		return nil, err
	}
	return parseIntelligenceExtraction(content)
}

func buildIntelligenceMessages(transcript string, current *IntelligenceSnapshot) []LLMMessage {
	payload := buildIntelligencePromptPayload(transcript, current)
	payloadBytes, _ := json.MarshalIndent(payload, "", "  ")
	return []LLMMessage{
		{Role: "system", Content: intelligenceSystemPrompt},
		{Role: "user", Content: string(payloadBytes)},
	}
}

func buildIntelligencePromptPayload(transcript string, current *IntelligenceSnapshot) map[string]any {
	payload := map[string]any{
		"full_transcript":       limitTail(transcript, 12000),
		"existing_summary":      "",
		"existing_decisions":    []MeetingDecision{},
		"existing_action_items": []MeetingActionItem{},
	}
	if current == nil {
		return payload
	}
	payload["existing_decisions"] = current.Decisions
	payload["existing_action_items"] = current.ActionItems
	if current.Summary != nil {
		payload["existing_summary"] = current.Summary.Content
	}
	return payload
}
