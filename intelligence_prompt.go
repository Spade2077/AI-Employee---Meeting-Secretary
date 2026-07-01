// 本文件负责会议结构化 LLM 的提示词和上下文构造。
package main

import (
	"context"
	"encoding/json"
)

const intelligenceSystemPrompt = `你是会议结构化助手。请根据会议转写维护结构化会议结果。
只输出合法 JSON，不要输出解释、Markdown、代码块。
输出必须严格使用下面的固定 JSON 结构，所有顶层字段必须出现，不要新增字段，不要输出 null：
{
  "title": "",
  "meeting_type": "project_followup | exploration | retrospective | general",
  "summary": {
    "brief": "",
    "background": "",
    "key_points": [],
    "progress": "",
    "changes": [],
    "conclusion": "",
    "next_follow_up": ""
  },
  "decisions": [],
  "action_items": [
    {
      "task": "",
      "owner": "",
      "due_date": "",
      "status": "open",
      "source_text": ""
    }
  ],
  "risks": [
    {
      "content": "",
      "impact": "",
      "level": "low | medium | high",
      "owner": "",
      "mitigation": "",
      "due_date": "",
      "status": "open",
      "source_text": ""
    }
  ],
  "open_questions": [
    {
      "question": "",
      "owner": "",
      "due_date": "",
      "status": "open",
      "source_text": ""
    }
  ],
  "framework_sections": [
    {
      "section_key": "",
      "title": "",
      "content": "",
      "source_text": ""
    }
  ],
  "dynamic_sections": [
    {
      "section_key": "",
      "title": "",
      "description": "",
      "items": [
        {
          "title": "",
          "content": "",
          "source_text": ""
        }
      ]
    }
  ]
}
要求：
1. title 控制在 12 字以内；内容不足时可以为空字符串。
2. meeting_type 必须从 project_followup、exploration、retrospective、general 中选择一个；无法判断时用 general。
3. summary 是当前会议摘要，控制在 100 字以内，可以基于全文更新。
4. decisions 数组项字段固定为 content、source_text，只放已经明确达成的决定。content 必须是提炼后的决策结论，控制在 30 字以内，不要照抄原话；source_text 才放能支撑该决策的原始发言。
5. action_items 数组项字段固定为 task、owner、due_date、status、source_text。
6. risks 数组项字段固定为 content、impact、level、owner、mitigation、due_date、status、source_text。
7. open_questions 数组项字段固定为 question、owner、due_date、status、source_text。
8. decisions、action_items、risks、open_questions 默认只返回尚未出现在 existing_* 中的新内容；没有新内容时返回空数组。
9. 如果后续发言明确否定、改期、改负责人、改处理方案或形成最终共识，要把被修正的原条目作为更新返回，字段填最新结论，并在 source_text 保留能说明修正的原话。
10. 多人讨论中以前后文最终确认结果为准，例如“这周能做完 / 不行要下周 / 那好就下周”应把截止时间提取为“下周”，不要保留被否定的“这周”。
11. 不要编造负责人、截止时间、处理方案或决定；没有明确说出就填空字符串。
12. status 默认填 open；level 只能填 low、medium、high。
13. 缺失项由后端计算，不要输出 missing_fields、missingFields、missing_alerts 或其他提醒字段。
14. 风险是可能影响推进的阻塞、依赖、不确定性或异常；待确认问题是会议里尚未定论的问题，不要把待办缺字段伪造成问题。
15. framework_sections 功能当前暂时关闭，必须返回空数组 []，不要提取任何会议类型记录框架模块。`

const dynamicSectionPromptExtension = `
16. dynamic_sections 用于记录不适合放进决策、待办、风险、待确认问题，但会议中多次出现或被明确强调的主题，例如预算约束、客户关注点、硬件依赖、数据合规、竞品信息、供应商限制、跨部门依赖。
17. 能放入 decisions、action_items、risks、open_questions 的内容必须优先放入对应固定列表，不要重复放进 dynamic_sections。
18. dynamic_sections 默认只返回新的或需要补充的动态模块；没有动态模块时返回空数组。
19. section_key 使用稳定英文小写蛇形命名，例如 budget_constraints、customer_focus、hardware_dependencies；title 使用中文展示名；items 中保留可解释的 source_text。`

const summaryPromptExtension = `
Summary rules override the older short-summary rule above:
20. summary must be an object, never a string.
21. summary.brief is the live screen overview, 80-150 Chinese characters when possible.
22. summary.background records why this meeting/topic exists. Leave empty if not stated.
23. summary.key_points contains 3-6 concise discussion points. Do not repeat decisions/action items verbatim.
24. summary.progress records current state or stage.
25. summary.changes records later corrections to earlier statements, such as changed owner, deadline, scope, or conclusion.
26. summary.conclusion records the current overall conclusion if one exists.
27. summary.next_follow_up records next review point, next meeting, or follow-up focus if stated.
28. Base summary on full_transcript plus existing structured data. Prefer latest confirmed context over earlier denied statements.
`

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
		{Role: "system", Content: intelligenceSystemPrompt + summaryPromptExtension + dynamicSectionPromptExtension},
		{Role: "user", Content: string(payloadBytes)},
	}
}

func buildIntelligencePromptPayload(transcript string, current *IntelligenceSnapshot) map[string]any {
	payload := map[string]any{
		"full_transcript":           limitTail(transcript, 12000),
		"existing_title":            "",
		"existing_meeting_type":     "unknown",
		"existing_summary":          map[string]any{},
		"existing_decisions":        []MeetingDecision{},
		"existing_action_items":     []MeetingActionItem{},
		"existing_risks":            []MeetingRisk{},
		"existing_open_questions":   []MeetingOpenQuestion{},
		"existing_dynamic_sections": []MeetingDynamicSection{},
	}
	if current == nil {
		return payload
	}
	payload["existing_decisions"] = current.Decisions
	payload["existing_action_items"] = current.ActionItems
	payload["existing_title"] = current.Title
	payload["existing_meeting_type"] = current.MeetingType
	payload["existing_risks"] = current.Risks
	payload["existing_open_questions"] = current.OpenQuestions
	if meetingFrameworkFeatureEnabled() {
		payload["framework_schema"] = frameworkSchemaForPrompt(current.MeetingType)
		payload["existing_framework_sections"] = current.FrameworkSections
	}
	payload["existing_dynamic_sections"] = current.DynamicSections
	if current.Summary != nil {
		payload["existing_summary"] = current.Summary
	}
	return payload
}
