// 本文件负责会中交互 LLM 的提示词、上下文构造和模型调用。
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

func (s *Server) callLLMForInteraction(ctx context.Context, cfg *LLMConfig, instruction string, snapshot *IntelligenceSnapshot) (InteractionCommand, string, error) {
	type actionItemContext struct {
		Index   int    `json:"index"`
		ID      string `json:"id"`
		Task    string `json:"task"`
		Owner   string `json:"owner,omitempty"`
		DueDate string `json:"dueDate,omitempty"`
		Status  string `json:"status"`
	}
	type decisionContext struct {
		Index   int    `json:"index"`
		ID      string `json:"id"`
		Content string `json:"content"`
	}

	items := make([]actionItemContext, 0, len(snapshot.ActionItems))
	for index, item := range snapshot.ActionItems {
		items = append(items, actionItemContext{
			Index:   index + 1,
			ID:      item.ID,
			Task:    item.Task,
			Owner:   item.Owner,
			DueDate: item.DueDate,
			Status:  item.Status,
		})
	}
	decisions := make([]decisionContext, 0, len(snapshot.Decisions))
	for index, decision := range snapshot.Decisions {
		decisions = append(decisions, decisionContext{
			Index:   index + 1,
			ID:      decision.ID,
			Content: decision.Content,
		})
	}
	summary := ""
	if snapshot.Summary != nil {
		summary = snapshot.Summary.Content
	}

	payload := map[string]any{
		"instruction":  instruction,
		"summary":      summary,
		"decisions":    decisions,
		"action_items": items,
		"supported_intents": []string{
			"delete_action_item",
			"update_action_item",
			"create_action_item",
			"complete_action_item",
			"move_action_item_to_decision",
			"merge_action_items",
			"delete_decision",
			"update_decision",
			"create_decision",
			"merge_decisions",
			"move_decision_to_action_item",
			"rewrite_summary_section",
			"undo_last_interaction",
			"unsupported",
		},
	}
	payloadBytes, _ := json.MarshalIndent(payload, "", "  ")
	systemPrompt := `You are the meeting assistant interaction parser. Convert the user's spoken instruction into one executable JSON command only. Do not execute anything. Do not output Markdown, explanations, or code fences.

Return exactly this JSON shape:
{
  "intent": "delete_action_item | update_action_item | create_action_item | complete_action_item | move_action_item_to_decision | merge_action_items | delete_decision | update_decision | create_decision | merge_decisions | move_decision_to_action_item | rewrite_summary_section | undo_last_interaction | unsupported",
  "target_type": "action_item | decision | summary | interaction",
  "target_index": 0,
  "target_id": "",
  "source_indices": [],
  "source_ids": [],
  "task": "",
  "owner": "",
  "due_date": "",
  "status": "",
  "content": "",
  "confidence": 0.0,
  "need_confirm": false,
  "reply": "",
  "reason": ""
}

Rules:
1. Use the supplied action_items, decisions, and summary as the only editable context.
2. For delete_action_item, complete_action_item, update_action_item, and move_action_item_to_decision, target_type must be action_item and target_index or target_id must identify one existing action item.
3. For create_action_item, target_type must be action_item and task is required. Fill owner, due_date, and status only if the instruction says them.
4. For update_action_item, fill only the fields the user asked to change: task, owner, due_date, status, or content when useful.
5. For complete_action_item, set status to completed.
6. For move_action_item_to_decision, content is optional. If the user asks to rewrite the decision wording, put the rewritten decision text in content.
7. For merge_action_items, source_indices or source_ids must contain every action item mentioned by the user. Do not use delete_action_item for merge instructions. If the user does not provide exact merged wording, leave task empty so the backend can combine the original tasks.
8. For delete_decision and update_decision, target_type must be decision and target_index or target_id must identify one existing decision. update_decision requires content.
9. For create_decision, target_type must be decision and content is required.
10. For merge_decisions, source_indices or source_ids must contain every decision mentioned by the user. Do not use delete_decision for merge instructions. If the user does not provide exact merged wording, leave content empty so the backend can combine the original decision texts.
11. For move_decision_to_action_item, target_type must be decision and target_index or target_id must identify one existing decision. task, owner, due_date, and status are optional action item values.
12. For rewrite_summary_section, target_type must be summary. If the user asks to rewrite one section (for example risk, missing items, decisions, action items), put the rewritten section text in content and put the target section name in reason. Do not put the user's instruction itself in content. If the user only gives rewrite requirements such as "focus on A and B", write a polished replacement section such as "风险提示：A；B。".
13. For undo_last_interaction, target_type must be interaction. Use it when the user says undo, revert, cancel the previous operation, or 撤销刚刚的操作.
14. If the target is ambiguous or does not exist, set need_confirm=true and explain in reason. Do not guess.
15. If the instruction is outside supported_intents, return unsupported.`

	content, err := s.llmClient.Complete(ctx, cfg, []LLMMessage{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: string(payloadBytes)},
	}, 0)
	if err != nil {
		return InteractionCommand{}, "", err
	}
	var command InteractionCommand
	if err := json.Unmarshal([]byte(extractJSONObject(content)), &command); err != nil {
		return InteractionCommand{}, content, err
	}
	return command, content, nil
}

func interactionLLMDetail(cfg *LLMConfig) string {
	if cfg == nil {
		return "交互 LLM 配置读取失败"
	}
	model := strings.TrimSpace(cfg.Model)
	if model == "" {
		model = "未配置"
	}
	return fmt.Sprintf("模型：%s；思考模式：%s", model, thinkingModeLabel(cfg.ThinkingMode))
}

func thinkingModeLabel(mode string) string {
	switch normalizeLLMThinkingMode(mode) {
	case LLMThinkingEnabled:
		return "enabled（已开启）"
	case LLMThinkingDisabled:
		return "disabled（已关闭）"
	default:
		return "default（未显式发送）"
	}
}
