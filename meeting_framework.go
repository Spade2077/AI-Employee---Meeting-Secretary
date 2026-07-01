// 本文件负责会议类型记录框架的模板定义、存储读取和去重合并。
package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

type MeetingFrameworkTemplateSection struct {
	MeetingType  string `json:"meetingType"`
	SectionKey   string `json:"sectionKey"`
	Title        string `json:"title"`
	DisplayOrder int    `json:"displayOrder"`
}

type FrameworkSectionMutation struct {
	Mode       string
	Content    string
	SourceText string
	Locked     bool
}

const meetingFrameworkEnabled = false

func meetingFrameworkFeatureEnabled() bool {
	return meetingFrameworkEnabled
}

func frameworkTemplatesForMeetingType(meetingType string) []MeetingFrameworkTemplateSection {
	if !meetingFrameworkFeatureEnabled() {
		return nil
	}
	meetingType = normalizeMeetingType(meetingType)
	switch meetingType {
	case "project_followup":
		return []MeetingFrameworkTemplateSection{
			{MeetingType: meetingType, SectionKey: "background", Title: "当前项目/事项背景", DisplayOrder: 1},
			{MeetingType: meetingType, SectionKey: "meeting_goal", Title: "本次会议目标", DisplayOrder: 2},
			{MeetingType: meetingType, SectionKey: "current_progress", Title: "当前进展", DisplayOrder: 3},
			{MeetingType: meetingType, SectionKey: "confirmed_items", Title: "已确认事项", DisplayOrder: 4},
			{MeetingType: meetingType, SectionKey: "next_followup", Title: "下次跟进节点", DisplayOrder: 5},
		}
	case "exploration":
		return []MeetingFrameworkTemplateSection{
			{MeetingType: meetingType, SectionKey: "discussion_background", Title: "讨论背景", DisplayOrder: 1},
			{MeetingType: meetingType, SectionKey: "core_question", Title: "核心问题", DisplayOrder: 2},
			{MeetingType: meetingType, SectionKey: "proposed_ideas", Title: "已提出想法", DisplayOrder: 3},
			{MeetingType: meetingType, SectionKey: "supporting_reasons", Title: "支持理由", DisplayOrder: 4},
			{MeetingType: meetingType, SectionKey: "opposing_risks", Title: "反对意见/风险", DisplayOrder: 5},
			{MeetingType: meetingType, SectionKey: "hypotheses_to_validate", Title: "待验证假设", DisplayOrder: 6},
			{MeetingType: meetingType, SectionKey: "next_exploration_actions", Title: "下一步探索动作", DisplayOrder: 7},
		}
	case "retrospective":
		return []MeetingFrameworkTemplateSection{
			{MeetingType: meetingType, SectionKey: "goal_vs_result", Title: "目标与实际结果", DisplayOrder: 1},
			{MeetingType: meetingType, SectionKey: "positives", Title: "做得好的地方", DisplayOrder: 2},
			{MeetingType: meetingType, SectionKey: "exposed_problems", Title: "暴露的问题", DisplayOrder: 3},
			{MeetingType: meetingType, SectionKey: "possible_causes", Title: "可能原因", DisplayOrder: 4},
			{MeetingType: meetingType, SectionKey: "improvement_plan", Title: "改进方案", DisplayOrder: 5},
			{MeetingType: meetingType, SectionKey: "follow_up_actions", Title: "后续行动", DisplayOrder: 6},
			{MeetingType: meetingType, SectionKey: "lessons_learned", Title: "可沉淀经验", DisplayOrder: 7},
		}
	case "general":
		return []MeetingFrameworkTemplateSection{
			{MeetingType: meetingType, SectionKey: "timeline_summary", Title: "时间线摘要", DisplayOrder: 1},
			{MeetingType: meetingType, SectionKey: "key_points", Title: "关键观点", DisplayOrder: 2},
			{MeetingType: meetingType, SectionKey: "unresolved_issues", Title: "未解决问题", DisplayOrder: 3},
		}
	default:
		return nil
	}
}

func frameworkSchemaForPrompt(meetingType string) []map[string]any {
	if !meetingFrameworkFeatureEnabled() {
		return []map[string]any{}
	}
	templates := frameworkTemplatesForMeetingType(meetingType)
	schema := make([]map[string]any, 0, len(templates))
	for _, template := range templates {
		schema = append(schema, map[string]any{
			"section_key":   template.SectionKey,
			"title":         template.Title,
			"display_order": template.DisplayOrder,
		})
	}
	return schema
}

func frameworkTemplateByKey(meetingType, sectionKey, title string) (MeetingFrameworkTemplateSection, bool) {
	meetingType = normalizeMeetingType(meetingType)
	key := normalizeFrameworkSectionKey(meetingType, sectionKey, title)
	for _, template := range frameworkTemplatesForMeetingType(meetingType) {
		templateKey := normalizeFrameworkSectionKey(meetingType, template.SectionKey, template.Title)
		if templateKey == key {
			template.SectionKey = templateKey
			template.MeetingType = meetingType
			return template, true
		}
	}
	return MeetingFrameworkTemplateSection{}, false
}

func (s *Store) ListFrameworkSections(ctx context.Context, meetingID, meetingType string) ([]MeetingFrameworkSection, error) {
	if !meetingFrameworkFeatureEnabled() {
		return []MeetingFrameworkSection{}, nil
	}
	meetingType = normalizeMeetingType(meetingType)
	stored, err := s.frameworkSectionsByType(ctx, meetingID, meetingType)
	if err != nil {
		return nil, err
	}
	templates := frameworkTemplatesForMeetingType(meetingType)
	if len(templates) == 0 {
		return stored, nil
	}

	byKey := map[string]MeetingFrameworkSection{}
	for _, section := range stored {
		byKey[normalizeFrameworkSectionKey(meetingType, section.SectionKey, section.Title)] = section
	}

	sections := make([]MeetingFrameworkSection, 0, len(templates)+len(stored))
	used := map[string]bool{}
	for _, template := range templates {
		key := normalizeFrameworkSectionKey(meetingType, template.SectionKey, template.Title)
		section, ok := byKey[key]
		if !ok {
			section = MeetingFrameworkSection{
				ID:           fmt.Sprintf("template:%s:%s", meetingType, key),
				MeetingID:    meetingID,
				MeetingType:  meetingType,
				SectionKey:   key,
				Title:        template.Title,
				Source:       "template",
				DisplayOrder: template.DisplayOrder,
			}
		}
		section.MeetingType = meetingType
		section.SectionKey = key
		section.Title = template.Title
		section.DisplayOrder = template.DisplayOrder
		sections = append(sections, section)
		used[key] = true
	}
	for _, section := range stored {
		key := normalizeFrameworkSectionKey(meetingType, section.SectionKey, section.Title)
		if used[key] {
			continue
		}
		sections = append(sections, section)
	}
	return sections, nil
}

func (s *Store) frameworkSectionsByType(ctx context.Context, meetingID, meetingType string) ([]MeetingFrameworkSection, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, meeting_id, meeting_type, section_key, title, content, source_text, source, locked, display_order, is_deleted, created_at, updated_at
		FROM meeting_framework_sections
		WHERE meeting_id = ? AND meeting_type = ? AND is_deleted = 0
		ORDER BY display_order ASC, created_at ASC`, meetingID, meetingType)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	sections := []MeetingFrameworkSection{}
	for rows.Next() {
		section, err := scanFrameworkSection(rows)
		if err != nil {
			return nil, err
		}
		sections = append(sections, *section)
	}
	return sections, rows.Err()
}

func frameworkSectionsTx(ctx context.Context, tx *sql.Tx, meetingID, meetingType string) ([]MeetingFrameworkSection, error) {
	if !meetingFrameworkFeatureEnabled() {
		return []MeetingFrameworkSection{}, nil
	}
	rows, err := tx.QueryContext(ctx, `SELECT id, meeting_id, meeting_type, section_key, title, content, source_text, source, locked, display_order, is_deleted, created_at, updated_at
		FROM meeting_framework_sections
		WHERE meeting_id = ? AND meeting_type = ? AND is_deleted = 0
		ORDER BY display_order ASC, created_at ASC`, meetingID, meetingType)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	sections := []MeetingFrameworkSection{}
	for rows.Next() {
		section, err := scanFrameworkSection(rows)
		if err != nil {
			return nil, err
		}
		sections = append(sections, *section)
	}
	return sections, rows.Err()
}

func saveFrameworkSectionExtractionsTx(ctx context.Context, tx *sql.Tx, meetingID, meetingType string, existing []MeetingFrameworkSection, sections []FrameworkSectionExtraction, now string) error {
	if !meetingFrameworkFeatureEnabled() {
		return nil
	}
	meetingType = normalizeMeetingType(meetingType)
	templates := frameworkTemplatesForMeetingType(meetingType)
	if len(templates) == 0 {
		return nil
	}
	templateByKey := map[string]MeetingFrameworkTemplateSection{}
	for _, template := range templates {
		templateByKey[normalizeFrameworkSectionKey(meetingType, template.SectionKey, template.Title)] = template
	}

	for _, section := range sections {
		key := normalizeFrameworkSectionKey(meetingType, section.SectionKey, section.Title)
		template, ok := templateByKey[key]
		if !ok {
			continue
		}
		content := strings.TrimSpace(section.Content)
		sourceText := strings.TrimSpace(section.SourceText)
		if content == "" && sourceText == "" {
			continue
		}
		if content == "" {
			content = sourceText
		}
		if index := matchingFrameworkSectionIndex(existing, key); index >= 0 {
			merged := existing[index]
			// 语音交互修改过的框架模块视为人工结论，后续自动结构化不能覆盖。
			if merged.Locked {
				continue
			}
			updatedContent := richerText(merged.Content, content)
			updatedSource := mergeSourceText(merged.SourceText, sourceText)
			if updatedContent == merged.Content && updatedSource == merged.SourceText {
				continue
			}
			_, err := tx.ExecContext(ctx, `UPDATE meeting_framework_sections SET
				title = ?, content = ?, source_text = ?, display_order = ?, updated_at = ?
				WHERE id = ?`,
				template.Title, updatedContent, updatedSource, template.DisplayOrder, now, merged.ID)
			if err != nil {
				return err
			}
			merged.Title = template.Title
			merged.Content = updatedContent
			merged.SourceText = updatedSource
			merged.DisplayOrder = template.DisplayOrder
			merged.UpdatedAt = now
			existing[index] = merged
			continue
		}
		inserted := MeetingFrameworkSection{
			ID:           newID("fw"),
			MeetingID:    meetingID,
			MeetingType:  meetingType,
			SectionKey:   key,
			Title:        template.Title,
			Content:      content,
			SourceText:   sourceText,
			Source:       "auto",
			DisplayOrder: template.DisplayOrder,
			CreatedAt:    now,
			UpdatedAt:    now,
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO meeting_framework_sections
			(id, meeting_id, meeting_type, section_key, title, content, source_text, source, locked, display_order, is_deleted, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, 'auto', 0, ?, 0, ?, ?)`,
			inserted.ID, inserted.MeetingID, inserted.MeetingType, inserted.SectionKey,
			inserted.Title, inserted.Content, inserted.SourceText, inserted.DisplayOrder, now, now)
		if err != nil {
			return err
		}
		existing = append(existing, inserted)
	}
	return nil
}

func (s *Store) ApplyFrameworkSectionMutation(ctx context.Context, meetingID, meetingType, sectionKey string, mutation FrameworkSectionMutation) (*MeetingFrameworkSection, error) {
	if !meetingFrameworkFeatureEnabled() {
		return nil, fmt.Errorf("meeting framework feature is disabled")
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	meetingType = normalizeMeetingType(meetingType)
	template, ok := frameworkTemplateByKey(meetingType, sectionKey, "")
	if !ok {
		return nil, fmt.Errorf("framework section %q is not supported by meeting type %s", sectionKey, meetingType)
	}

	mode := normalizeFrameworkMutationMode(mutation.Mode)
	content := strings.TrimSpace(mutation.Content)
	if mode != "clear" && content == "" {
		return nil, fmt.Errorf("framework section content is required")
	}

	section, err := s.frameworkSectionByKeyNoLock(ctx, meetingID, meetingType, template.SectionKey)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}

	now := nowString()
	if errors.Is(err, sql.ErrNoRows) {
		section = &MeetingFrameworkSection{
			ID:           newID("fw"),
			MeetingID:    meetingID,
			MeetingType:  meetingType,
			SectionKey:   template.SectionKey,
			Title:        template.Title,
			Source:       "interaction",
			DisplayOrder: template.DisplayOrder,
			CreatedAt:    now,
		}
	}

	switch mode {
	case "append":
		section.Content = appendFrameworkContent(section.Content, content)
	case "clear":
		section.Content = ""
	default:
		section.Content = content
	}
	section.MeetingID = meetingID
	section.MeetingType = meetingType
	section.SectionKey = template.SectionKey
	section.Title = template.Title
	section.DisplayOrder = template.DisplayOrder
	section.Source = "interaction"
	section.Locked = mutation.Locked
	if section.CreatedAt == "" {
		section.CreatedAt = now
	}
	section.UpdatedAt = now
	sourceText := strings.TrimSpace(mutation.SourceText)
	if mode == "append" {
		section.SourceText = mergeSourceText(section.SourceText, sourceText)
	} else {
		section.SourceText = sourceText
	}
	locked := 0
	if section.Locked {
		locked = 1
	}

	_, err = s.db.ExecContext(ctx, `INSERT INTO meeting_framework_sections
		(id, meeting_id, meeting_type, section_key, title, content, source_text, source, locked, display_order, is_deleted, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			meeting_type = excluded.meeting_type,
			section_key = excluded.section_key,
			title = excluded.title,
			content = excluded.content,
			source_text = excluded.source_text,
			source = excluded.source,
			locked = excluded.locked,
			display_order = excluded.display_order,
			is_deleted = 0,
			updated_at = excluded.updated_at`,
		section.ID, section.MeetingID, section.MeetingType, section.SectionKey, section.Title,
		section.Content, section.SourceText, section.Source, locked, section.DisplayOrder,
		section.CreatedAt, section.UpdatedAt)
	if err != nil {
		return nil, err
	}
	section.IsDeleted = false
	return section, nil
}

func (s *Store) RestoreFrameworkSection(ctx context.Context, section MeetingFrameworkSection) (*MeetingFrameworkSection, error) {
	if !meetingFrameworkFeatureEnabled() {
		return nil, fmt.Errorf("meeting framework feature is disabled")
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	meetingType := normalizeMeetingType(section.MeetingType)
	template, ok := frameworkTemplateByKey(meetingType, section.SectionKey, section.Title)
	if !ok {
		return nil, fmt.Errorf("framework section %q is not supported by meeting type %s", section.SectionKey, meetingType)
	}
	section.MeetingType = meetingType
	section.SectionKey = template.SectionKey
	section.Title = template.Title
	section.DisplayOrder = template.DisplayOrder
	if strings.TrimSpace(section.MeetingID) == "" {
		return nil, fmt.Errorf("framework section meeting id is required")
	}

	// 撤销到模板占位状态时，删除真实行，让读取逻辑重新返回模板占位模块。
	if strings.HasPrefix(section.ID, "template:") && strings.TrimSpace(section.Content) == "" &&
		strings.TrimSpace(section.SourceText) == "" && !section.Locked {
		_, err := s.db.ExecContext(ctx, `UPDATE meeting_framework_sections
			SET is_deleted = 1, updated_at = ?
			WHERE meeting_id = ? AND meeting_type = ? AND section_key = ? AND is_deleted = 0`,
			nowString(), section.MeetingID, section.MeetingType, section.SectionKey)
		if err != nil {
			return nil, err
		}
		section.Source = "template"
		section.IsDeleted = false
		return &section, nil
	}

	now := nowString()
	if strings.TrimSpace(section.ID) == "" || strings.HasPrefix(section.ID, "template:") {
		section.ID = newID("fw")
	}
	if strings.TrimSpace(section.Source) == "" {
		section.Source = "auto"
	}
	if section.CreatedAt == "" {
		section.CreatedAt = now
	}
	section.UpdatedAt = now
	locked := 0
	if section.Locked {
		locked = 1
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO meeting_framework_sections
		(id, meeting_id, meeting_type, section_key, title, content, source_text, source, locked, display_order, is_deleted, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			meeting_type = excluded.meeting_type,
			section_key = excluded.section_key,
			title = excluded.title,
			content = excluded.content,
			source_text = excluded.source_text,
			source = excluded.source,
			locked = excluded.locked,
			display_order = excluded.display_order,
			is_deleted = 0,
			updated_at = excluded.updated_at`,
		section.ID, section.MeetingID, section.MeetingType, section.SectionKey, section.Title,
		section.Content, section.SourceText, section.Source, locked, section.DisplayOrder,
		section.CreatedAt, section.UpdatedAt)
	if err != nil {
		return nil, err
	}
	section.IsDeleted = false
	return &section, nil
}

func (s *Store) frameworkSectionByKeyNoLock(ctx context.Context, meetingID, meetingType, sectionKey string) (*MeetingFrameworkSection, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, meeting_id, meeting_type, section_key, title, content, source_text, source, locked, display_order, is_deleted, created_at, updated_at
		FROM meeting_framework_sections
		WHERE meeting_id = ? AND meeting_type = ? AND section_key = ? AND is_deleted = 0
		ORDER BY updated_at DESC LIMIT 1`, meetingID, meetingType, sectionKey)
	return scanFrameworkSection(row)
}

func normalizeFrameworkMutationMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "append", "补充", "add":
		return "append"
	case "clear", "清空":
		return "clear"
	default:
		return "replace"
	}
}

func appendFrameworkContent(existing, incoming string) string {
	existing = strings.TrimSpace(existing)
	incoming = strings.TrimSpace(incoming)
	if incoming == "" {
		return existing
	}
	if existing == "" {
		return incoming
	}
	existingKey := normalizeForCompare(existing)
	incomingKey := normalizeForCompare(incoming)
	if incomingKey != "" && strings.Contains(existingKey, incomingKey) {
		return existing
	}
	return existing + "\n" + incoming
}

func scanFrameworkSection(scanner interface {
	Scan(dest ...any) error
}) (*MeetingFrameworkSection, error) {
	var section MeetingFrameworkSection
	var deleted int
	var locked int
	if err := scanner.Scan(&section.ID, &section.MeetingID, &section.MeetingType, &section.SectionKey,
		&section.Title, &section.Content, &section.SourceText, &section.Source, &locked, &section.DisplayOrder,
		&deleted, &section.CreatedAt, &section.UpdatedAt); err != nil {
		return nil, err
	}
	section.MeetingType = normalizeMeetingType(section.MeetingType)
	if strings.TrimSpace(section.Source) == "" {
		section.Source = "auto"
	}
	section.Locked = locked != 0
	section.IsDeleted = deleted != 0
	return &section, nil
}

func matchingFrameworkSectionIndex(existing []MeetingFrameworkSection, key string) int {
	for index, section := range existing {
		if normalizeFrameworkSectionKey(section.MeetingType, section.SectionKey, section.Title) == key {
			return index
		}
	}
	return -1
}

func normalizeFrameworkSectionKey(meetingType, key, title string) string {
	meetingType = normalizeMeetingType(meetingType)
	key = normalizeDynamicSectionKeyInput(key)
	if key != "" {
		switch key {
		case "goal", "objective", "meeting_objective":
			return "meeting_goal"
		case "progress":
			return "current_progress"
		case "confirmed", "confirmed_decisions", "confirmed_matters":
			return "confirmed_items"
		case "followup", "follow_up", "next_step", "next_steps":
			return "next_followup"
		case "background", "discussion_context":
			if meetingType == "exploration" {
				return "discussion_background"
			}
			return key
		case "problem", "question", "core_problem":
			if meetingType == "exploration" {
				return "core_question"
			}
			return key
		case "ideas", "proposals", "proposed_options":
			return "proposed_ideas"
		case "support", "supports", "pros", "supporting_points":
			return "supporting_reasons"
		case "opposition", "objections", "cons", "risks", "opposing_opinions":
			if meetingType == "exploration" {
				return "opposing_risks"
			}
			return key
		case "hypotheses", "assumptions", "validation_hypotheses":
			return "hypotheses_to_validate"
		case "exploration_actions", "next_actions":
			if meetingType == "exploration" {
				return "next_exploration_actions"
			}
			return key
		case "result", "results", "goal_result":
			return "goal_vs_result"
		case "good_points", "what_went_well", "wins":
			return "positives"
		case "problems", "issues":
			if meetingType == "retrospective" {
				return "exposed_problems"
			}
			return key
		case "causes", "root_causes":
			return "possible_causes"
		case "improvements", "improvement_actions":
			return "improvement_plan"
		case "actions":
			if meetingType == "retrospective" {
				return "follow_up_actions"
			}
			return key
		case "experience", "lessons", "takeaways":
			return "lessons_learned"
		case "timeline", "timeline_digest":
			return "timeline_summary"
		case "points", "key_views", "viewpoints":
			return "key_points"
		case "unresolved", "open_issues":
			return "unresolved_issues"
		default:
			return key
		}
	}
	titleKey := normalizeForCompare(title)
	switch meetingType {
	case "project_followup":
		switch {
		case strings.Contains(titleKey, "背景"):
			return "background"
		case strings.Contains(titleKey, "目标"):
			return "meeting_goal"
		case strings.Contains(titleKey, "进展"):
			return "current_progress"
		case strings.Contains(titleKey, "确认"):
			return "confirmed_items"
		case strings.Contains(titleKey, "下次") || strings.Contains(titleKey, "跟进"):
			return "next_followup"
		}
	case "exploration":
		switch {
		case strings.Contains(titleKey, "背景"):
			return "discussion_background"
		case strings.Contains(titleKey, "核心") || strings.Contains(titleKey, "问题"):
			return "core_question"
		case strings.Contains(titleKey, "想法") || strings.Contains(titleKey, "方案"):
			return "proposed_ideas"
		case strings.Contains(titleKey, "支持") || strings.Contains(titleKey, "理由"):
			return "supporting_reasons"
		case strings.Contains(titleKey, "反对") || strings.Contains(titleKey, "风险"):
			return "opposing_risks"
		case strings.Contains(titleKey, "假设") || strings.Contains(titleKey, "验证"):
			return "hypotheses_to_validate"
		case strings.Contains(titleKey, "下一步") || strings.Contains(titleKey, "探索动作"):
			return "next_exploration_actions"
		}
	case "retrospective":
		switch {
		case strings.Contains(titleKey, "目标") || strings.Contains(titleKey, "实际结果"):
			return "goal_vs_result"
		case strings.Contains(titleKey, "做得好") || strings.Contains(titleKey, "亮点"):
			return "positives"
		case strings.Contains(titleKey, "暴露") || strings.Contains(titleKey, "问题"):
			return "exposed_problems"
		case strings.Contains(titleKey, "原因"):
			return "possible_causes"
		case strings.Contains(titleKey, "改进"):
			return "improvement_plan"
		case strings.Contains(titleKey, "后续行动"):
			return "follow_up_actions"
		case strings.Contains(titleKey, "沉淀") || strings.Contains(titleKey, "经验"):
			return "lessons_learned"
		}
	case "general":
		switch {
		case strings.Contains(titleKey, "时间线") || strings.Contains(titleKey, "摘要"):
			return "timeline_summary"
		case strings.Contains(titleKey, "关键观点") || strings.Contains(titleKey, "观点"):
			return "key_points"
		case strings.Contains(titleKey, "未解决") || strings.Contains(titleKey, "遗留问题"):
			return "unresolved_issues"
		}
	}
	return titleKey
}
