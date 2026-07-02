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
	type riskContext struct {
		Index      int    `json:"index"`
		ID         string `json:"id"`
		Content    string `json:"content"`
		Impact     string `json:"impact,omitempty"`
		Level      string `json:"level"`
		Owner      string `json:"owner,omitempty"`
		Mitigation string `json:"mitigation,omitempty"`
		DueDate    string `json:"dueDate,omitempty"`
		Status     string `json:"status"`
	}
	type openQuestionContext struct {
		Index    int    `json:"index"`
		ID       string `json:"id"`
		Question string `json:"question"`
		Owner    string `json:"owner,omitempty"`
		DueDate  string `json:"dueDate,omitempty"`
		Status   string `json:"status"`
	}
	type dynamicSectionItemContext struct {
		Index   int    `json:"index"`
		ID      string `json:"id"`
		Title   string `json:"title,omitempty"`
		Content string `json:"content,omitempty"`
	}
	type dynamicSectionContext struct {
		Index       int                         `json:"index"`
		ID          string                      `json:"id"`
		SectionKey  string                      `json:"sectionKey"`
		Title       string                      `json:"title"`
		Description string                      `json:"description,omitempty"`
		Visibility  string                      `json:"visibility"`
		Items       []dynamicSectionItemContext `json:"items,omitempty"`
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
	risks := make([]riskContext, 0, len(snapshot.Risks))
	for index, risk := range snapshot.Risks {
		risks = append(risks, riskContext{
			Index:      index + 1,
			ID:         risk.ID,
			Content:    risk.Content,
			Impact:     risk.Impact,
			Level:      risk.Level,
			Owner:      risk.Owner,
			Mitigation: risk.Mitigation,
			DueDate:    risk.DueDate,
			Status:     risk.Status,
		})
	}
	openQuestions := make([]openQuestionContext, 0, len(snapshot.OpenQuestions))
	for index, question := range snapshot.OpenQuestions {
		openQuestions = append(openQuestions, openQuestionContext{
			Index:    index + 1,
			ID:       question.ID,
			Question: question.Question,
			Owner:    question.Owner,
			DueDate:  question.DueDate,
			Status:   question.Status,
		})
	}
	dynamicSections := make([]dynamicSectionContext, 0, len(snapshot.DynamicSections))
	for index, section := range snapshot.DynamicSections {
		items := make([]dynamicSectionItemContext, 0, len(section.Items))
		for itemIndex, item := range section.Items {
			items = append(items, dynamicSectionItemContext{
				Index:   itemIndex + 1,
				ID:      item.ID,
				Title:   item.Title,
				Content: item.Content,
			})
		}
		dynamicSections = append(dynamicSections, dynamicSectionContext{
			Index:       index + 1,
			ID:          section.ID,
			SectionKey:  section.SectionKey,
			Title:       section.Title,
			Description: section.Description,
			Visibility:  section.Visibility,
			Items:       items,
		})
	}
	summary := ""
	if snapshot.Summary != nil {
		summary = snapshot.Summary.Content
	}

	supportedIntents := []string{
		"update_meeting_title",
		"switch_meeting_view",
		"show_share_qr",
		"request_end_meeting",
		"create_dynamic_section",
		"add_dynamic_section_item",
		"update_dynamic_section",
		"update_dynamic_section_item",
		"delete_dynamic_section",
		"merge_dynamic_sections",
		"show_dynamic_section",
		"hide_dynamic_section",
		"show_all_dynamic_sections",
		"hide_all_dynamic_sections",
		"pin_dynamic_section",
		"unpin_dynamic_section",
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
		"create_risk",
		"update_risk",
		"close_risk",
		"delete_risk",
		"merge_risks",
		"create_open_question",
		"update_open_question",
		"close_open_question",
		"delete_open_question",
		"merge_open_questions",
		"rewrite_summary_section",
		"undo_last_interaction",
		"correct_last_interaction",
		"unsupported",
	}
	if meetingFrameworkFeatureEnabled() {
		supportedIntents = append(supportedIntents,
			"update_framework_section",
			"append_framework_section",
			"rewrite_framework_section",
			"clear_framework_section",
		)
	}

	payload := map[string]any{
		"meeting_title":     snapshot.Title,
		"meeting_type":      snapshot.MeetingType,
		"instruction":       instruction,
		"summary":           summary,
		"decisions":         decisions,
		"action_items":      items,
		"risks":             risks,
		"open_questions":    openQuestions,
		"dynamic_sections":  dynamicSections,
		"supported_intents": supportedIntents,
	}
	if meetingFrameworkFeatureEnabled() {
		payload["framework_sections"] = snapshot.FrameworkSections
	}
	payloadBytes, _ := json.MarshalIndent(payload, "", "  ")
	systemPrompt := `You are the meeting assistant interaction parser. Convert the user's spoken instruction into one executable JSON command only. Do not execute anything. Do not output Markdown, explanations, or code fences.

Return exactly this JSON shape:
{
  "intent": "update_meeting_title | switch_meeting_view | show_share_qr | request_end_meeting | create_dynamic_section | add_dynamic_section_item | update_dynamic_section | update_dynamic_section_item | delete_dynamic_section | merge_dynamic_sections | show_dynamic_section | hide_dynamic_section | show_all_dynamic_sections | hide_all_dynamic_sections | pin_dynamic_section | unpin_dynamic_section | delete_action_item | update_action_item | create_action_item | complete_action_item | move_action_item_to_decision | merge_action_items | delete_decision | update_decision | create_decision | merge_decisions | move_decision_to_action_item | create_risk | update_risk | close_risk | delete_risk | merge_risks | create_open_question | update_open_question | close_open_question | delete_open_question | merge_open_questions | rewrite_summary_section | undo_last_interaction | correct_last_interaction | unsupported",
  "target_type": "meeting | meeting_view | share | dynamic_section | action_item | decision | risk | open_question | summary | interaction",
  "target_index": 0,
  "target_id": "",
  "source_indices": [],
  "source_ids": [],
  "task": "",
  "owner": "",
  "due_date": "",
  "status": "",
  "impact": "",
  "level": "",
  "mitigation": "",
  "question": "",
  "title": "",
  "section_key": "",
  "section_title": "",
  "content": "",
  "confidence": 0.0,
  "need_confirm": false,
  "reply": "",
  "reason": ""
}

Rules:
1. Use the supplied meeting_title, action_items, decisions, risks, open_questions, dynamic_sections, and summary as the only editable context.
2. For update_meeting_title, target_type must be meeting and title is required. Use it for instructions like "把会议标题改成X", "设置标题为X", "这场会就叫X", or "会议名称改为X". Put the same value in content only for compatibility.
3. For switch_meeting_view, target_type must be meeting_view. Use target_id/content as one of live or summary. Map "实时记录/转写/记录" to live. Map "会议总结/待办/决策/风险/待确认" to summary. Do not use switch_meeting_view for QR code, meeting minutes download, or ending the meeting.
4. For show_share_qr, target_type must be share and target_id should be minutes_qr. Use this when the user asks to display/open/show the QR code, scan code, or meeting minutes download QR.
5. For request_end_meeting, target_type must be meeting. Use this when the user asks to end/stop/close this meeting. The backend will only request confirmation; do not end directly.
5a. For create_dynamic_section, target_type must be dynamic_section. Put the new module name in section_title and title. If the user also gives initial module content, put it in content. Use this for instructions like "add/create a budget constraints module" or "create a module for customer objections".
5b. For add_dynamic_section_item, target_type must be dynamic_section. Use target_index/target_id or section_key/section_title from supplied dynamic_sections to identify one existing dynamic module, and put the new item content in content.
5c. For update_dynamic_section, target_type must be dynamic_section. Use target_index/target_id or section_key/section_title from supplied dynamic_sections to identify one existing dynamic module. Put the new module description in content. Only use title when the user explicitly asks to rename the module.
5d. For update_dynamic_section_item, target_type must be dynamic_section. First identify the existing dynamic module with target_index/target_id or section_key/section_title, then identify the item inside that module with source_indices or source_ids. Put the rewritten item content in content. Use title only if the user explicitly changes the item title.
5e. For delete_dynamic_section, target_type must be dynamic_section. Use target_index/target_id or section_key/section_title from supplied dynamic_sections to identify one existing dynamic module.
5f. For merge_dynamic_sections, target_type must be dynamic_section. source_indices or source_ids must contain every dynamic module explicitly named or numbered by the user as merge sources. Do not treat the new combined title as a source module, even if it looks similar to another existing module. Do not use delete_dynamic_section for merge instructions. If the user gives a new combined module title, put it in title/section_title. If the user gives a new description, put it in content.
5g. For show_dynamic_section, hide_dynamic_section, pin_dynamic_section, and unpin_dynamic_section, target_type must be dynamic_section. Use target_index/target_id or section_key/section_title from supplied dynamic_sections to identify one existing dynamic module.
5h. For show_all_dynamic_sections and hide_all_dynamic_sections, target_type must be dynamic_section and no single target is required. Use these only when the user asks to show/hide all dynamic modules, all discovered modules, all custom sections, or all extra topics.
5i. Do not use switch_meeting_view for showing or hiding a specific dynamic module. switch_meeting_view only changes the main page between live and summary.
5j. Meeting-type framework module editing is currently disabled. Do not output framework_section target_type or any update_framework_section/append_framework_section/rewrite_framework_section/clear_framework_section intent.
6. For delete_action_item, complete_action_item, update_action_item, and move_action_item_to_decision, target_type must be action_item and target_index or target_id must identify one existing action item.
7. For create_action_item, target_type must be action_item and task is required. Fill owner, due_date, and status only if the instruction says them.
5. For update_action_item, fill only the fields the user asked to change: task, owner, due_date, status, or content when useful.
6. For complete_action_item, set status to completed.
7. For move_action_item_to_decision, content is optional. If the user asks to rewrite the decision wording, put the rewritten decision text in content.
8. For merge_action_items, source_indices or source_ids must contain every action item mentioned by the user. Do not use delete_action_item for merge instructions. If the user does not provide exact merged wording, leave task empty so the backend can combine the original tasks.
9. For delete_decision and update_decision, target_type must be decision and target_index or target_id must identify one existing decision. update_decision requires content.
10. For create_decision, target_type must be decision and content is required.
11. For merge_decisions, source_indices or source_ids must contain every decision mentioned by the user. Do not use delete_decision for merge instructions. If the user does not provide exact merged wording, leave content empty so the backend can combine the original decision texts.
12. For move_decision_to_action_item, target_type must be decision and target_index or target_id must identify one existing decision. task, owner, due_date, and status are optional action item values.
13. For create_risk, target_type must be risk and content is required. Fill impact, level, owner, mitigation, due_date, and status only if the instruction says them.
14. For update_risk, close_risk, and delete_risk, target_type must be risk and target_index or target_id must identify one existing risk. update_risk may fill content, impact, level, owner, mitigation, due_date, or status. If the user says the risk is resolved, use close_risk.
15. For merge_risks, source_indices or source_ids must contain every risk mentioned by the user. Do not use delete_risk for merge instructions. If the user does not provide exact merged wording, leave content empty so the backend can combine the original risks.
16. For create_open_question, target_type must be open_question and question is required. You may also put the question text in content for compatibility.
17. For update_open_question, close_open_question, and delete_open_question, target_type must be open_question and target_index or target_id must identify one existing open question. update_open_question may fill question, owner, due_date, or status. If the user says it is confirmed/resolved/closed, use close_open_question.
18. For merge_open_questions, source_indices or source_ids must contain every open question mentioned by the user. Do not use delete_open_question for merge instructions.
19. Missing-field completion commands should map to update_action_item, update_risk, or update_open_question. Examples: "麦克风风险负责人是李娜" => update_risk owner; "处理方案是备用麦克风" => update_risk mitigation; "展会网络问题周三前确认" => update_open_question due_date.
20. For rewrite_summary_section, target_type must be summary. If the user asks to rewrite one section (for example risk, missing items, decisions, action items), put the rewritten section text in content and put the target section name in reason. Do not put the user's instruction itself in content. If the user only gives rewrite requirements such as "focus on A and B", write a polished replacement section such as "风险提示：A；B。".
21. For undo_last_interaction, target_type must be interaction. Use it when the user says undo, revert, cancel the previous operation, or 撤销刚刚的操作.
21a. For correct_last_interaction, target_type must be interaction. Use it when the user corrects a mistaken previous spoken command, such as "not A, it should be B", "I said it wrong, change it to B", or "刚才说错了，应该 B". Put the corrected executable instruction B in content, and put the mistaken phrase A in reason when available. The backend will undo the previous operation first, then parse and execute content.
22. If the target is ambiguous or does not exist, set need_confirm=true and explain in reason. Do not guess.
23. If the instruction is outside supported_intents, return unsupported.`

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
