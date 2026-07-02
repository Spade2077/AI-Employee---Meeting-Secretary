// 本文件负责把会中交互命令里的序号、ID 和合并来源解析为真实业务对象。
package main

import (
	"context"
	"fmt"
	"strings"
)

func (s *Server) resolveActionItemTarget(ctx context.Context, meetingID string, command InteractionCommand) (int, *MeetingActionItem, error) {
	actionItems, err := s.store.ListActionItems(ctx, meetingID)
	if err != nil {
		return 0, nil, err
	}
	targetID := strings.TrimSpace(command.TargetID)
	if targetID != "" {
		for index, item := range actionItems {
			if item.ID == targetID {
				return index + 1, &actionItems[index], nil
			}
		}
		return 0, nil, fmt.Errorf("action item id %s not found", targetID)
	}
	if command.TargetIndex <= 0 || command.TargetIndex > len(actionItems) {
		return 0, nil, fmt.Errorf("action item index %d out of range", command.TargetIndex)
	}
	return command.TargetIndex, &actionItems[command.TargetIndex-1], nil
}

func (s *Server) resolveMergeActionItemIDs(ctx context.Context, meetingID string, command InteractionCommand) ([]string, error) {
	actionItems, err := s.store.ListActionItems(ctx, meetingID)
	if err != nil {
		return nil, err
	}
	ids := append([]string{}, command.SourceIDs...)
	if command.TargetID != "" {
		ids = append([]string{command.TargetID}, ids...)
	}
	for _, index := range command.SourceIndices {
		if index <= 0 || index > len(actionItems) {
			return nil, fmt.Errorf("action item index %d out of range", index)
		}
		ids = append(ids, actionItems[index-1].ID)
	}
	if command.TargetID == "" && command.TargetIndex > 0 {
		if command.TargetIndex > len(actionItems) {
			return nil, fmt.Errorf("action item index %d out of range", command.TargetIndex)
		}
		ids = append([]string{actionItems[command.TargetIndex-1].ID}, ids...)
	}
	ids = uniqueNonEmptyStrings(ids)
	if len(ids) < 2 {
		return nil, fmt.Errorf("merge requires at least two action item targets")
	}
	return ids, nil
}

func (s *Server) actionItemSnapshots(ctx context.Context, meetingID string, ids []string) ([]MeetingActionItem, error) {
	actionItems, err := s.store.ListActionItems(ctx, meetingID)
	if err != nil {
		return nil, err
	}
	byID := map[string]MeetingActionItem{}
	for _, item := range actionItems {
		byID[item.ID] = item
	}
	result := make([]MeetingActionItem, 0, len(ids))
	for _, id := range uniqueNonEmptyStrings(ids) {
		item, ok := byID[id]
		if !ok {
			return nil, fmt.Errorf("action item id %s not found", id)
		}
		result = append(result, item)
	}
	return result, nil
}

func (s *Server) resolveDecisionTarget(ctx context.Context, meetingID string, command InteractionCommand) (int, *MeetingDecision, error) {
	decisions, err := s.store.ListDecisions(ctx, meetingID)
	if err != nil {
		return 0, nil, err
	}
	targetID := strings.TrimSpace(command.TargetID)
	if targetID != "" {
		for index, decision := range decisions {
			if decision.ID == targetID {
				return index + 1, &decisions[index], nil
			}
		}
		return 0, nil, fmt.Errorf("decision id %s not found", targetID)
	}
	if command.TargetIndex <= 0 || command.TargetIndex > len(decisions) {
		return 0, nil, fmt.Errorf("decision index %d out of range", command.TargetIndex)
	}
	return command.TargetIndex, &decisions[command.TargetIndex-1], nil
}

func (s *Server) resolveMergeDecisionIDs(ctx context.Context, meetingID string, command InteractionCommand) ([]string, error) {
	decisions, err := s.store.ListDecisions(ctx, meetingID)
	if err != nil {
		return nil, err
	}
	ids := append([]string{}, command.SourceIDs...)
	if command.TargetID != "" {
		ids = append([]string{command.TargetID}, ids...)
	}
	for _, index := range command.SourceIndices {
		if index <= 0 || index > len(decisions) {
			return nil, fmt.Errorf("decision index %d out of range", index)
		}
		ids = append(ids, decisions[index-1].ID)
	}
	if command.TargetID == "" && command.TargetIndex > 0 {
		if command.TargetIndex > len(decisions) {
			return nil, fmt.Errorf("decision index %d out of range", command.TargetIndex)
		}
		ids = append([]string{decisions[command.TargetIndex-1].ID}, ids...)
	}
	ids = uniqueNonEmptyStrings(ids)
	if len(ids) < 2 {
		return nil, fmt.Errorf("merge requires at least two decision targets")
	}
	return ids, nil
}

func (s *Server) decisionSnapshots(ctx context.Context, meetingID string, ids []string) ([]MeetingDecision, error) {
	decisions, err := s.store.ListDecisions(ctx, meetingID)
	if err != nil {
		return nil, err
	}
	byID := map[string]MeetingDecision{}
	for _, decision := range decisions {
		byID[decision.ID] = decision
	}
	result := make([]MeetingDecision, 0, len(ids))
	for _, id := range uniqueNonEmptyStrings(ids) {
		decision, ok := byID[id]
		if !ok {
			return nil, fmt.Errorf("decision id %s not found", id)
		}
		result = append(result, decision)
	}
	return result, nil
}

func (s *Server) resolveRiskTarget(ctx context.Context, meetingID string, command InteractionCommand) (int, *MeetingRisk, error) {
	risks, err := s.store.ListRisks(ctx, meetingID)
	if err != nil {
		return 0, nil, err
	}
	targetID := strings.TrimSpace(command.TargetID)
	if targetID != "" {
		for index, risk := range risks {
			if risk.ID == targetID {
				return index + 1, &risks[index], nil
			}
		}
		return 0, nil, fmt.Errorf("risk id %s not found", targetID)
	}
	if command.TargetIndex <= 0 || command.TargetIndex > len(risks) {
		return 0, nil, fmt.Errorf("risk index %d out of range", command.TargetIndex)
	}
	return command.TargetIndex, &risks[command.TargetIndex-1], nil
}

func (s *Server) resolveMergeRiskIDs(ctx context.Context, meetingID string, command InteractionCommand) ([]string, error) {
	risks, err := s.store.ListRisks(ctx, meetingID)
	if err != nil {
		return nil, err
	}
	ids := append([]string{}, command.SourceIDs...)
	if command.TargetID != "" {
		ids = append([]string{command.TargetID}, ids...)
	}
	for _, index := range command.SourceIndices {
		if index <= 0 || index > len(risks) {
			return nil, fmt.Errorf("risk index %d out of range", index)
		}
		ids = append(ids, risks[index-1].ID)
	}
	if command.TargetID == "" && command.TargetIndex > 0 {
		if command.TargetIndex > len(risks) {
			return nil, fmt.Errorf("risk index %d out of range", command.TargetIndex)
		}
		ids = append([]string{risks[command.TargetIndex-1].ID}, ids...)
	}
	ids = uniqueNonEmptyStrings(ids)
	if len(ids) < 2 {
		return nil, fmt.Errorf("merge requires at least two risk targets")
	}
	return ids, nil
}

func (s *Server) resolveOpenQuestionTarget(ctx context.Context, meetingID string, command InteractionCommand) (int, *MeetingOpenQuestion, error) {
	questions, err := s.store.ListOpenQuestions(ctx, meetingID)
	if err != nil {
		return 0, nil, err
	}
	targetID := strings.TrimSpace(command.TargetID)
	if targetID != "" {
		for index, question := range questions {
			if question.ID == targetID {
				return index + 1, &questions[index], nil
			}
		}
		return 0, nil, fmt.Errorf("open question id %s not found", targetID)
	}
	if command.TargetIndex <= 0 || command.TargetIndex > len(questions) {
		return 0, nil, fmt.Errorf("open question index %d out of range", command.TargetIndex)
	}
	return command.TargetIndex, &questions[command.TargetIndex-1], nil
}

func (s *Server) resolveMergeOpenQuestionIDs(ctx context.Context, meetingID string, command InteractionCommand) ([]string, error) {
	questions, err := s.store.ListOpenQuestions(ctx, meetingID)
	if err != nil {
		return nil, err
	}
	ids := append([]string{}, command.SourceIDs...)
	if command.TargetID != "" {
		ids = append([]string{command.TargetID}, ids...)
	}
	for _, index := range command.SourceIndices {
		if index <= 0 || index > len(questions) {
			return nil, fmt.Errorf("open question index %d out of range", index)
		}
		ids = append(ids, questions[index-1].ID)
	}
	if command.TargetID == "" && command.TargetIndex > 0 {
		if command.TargetIndex > len(questions) {
			return nil, fmt.Errorf("open question index %d out of range", command.TargetIndex)
		}
		ids = append([]string{questions[command.TargetIndex-1].ID}, ids...)
	}
	ids = uniqueNonEmptyStrings(ids)
	if len(ids) < 2 {
		return nil, fmt.Errorf("merge requires at least two open question targets")
	}
	return ids, nil
}

func (s *Server) resolveDynamicSectionTarget(ctx context.Context, meetingID string, command InteractionCommand) (int, *MeetingDynamicSection, error) {
	sections, err := s.store.ListDynamicSections(ctx, meetingID)
	if err != nil {
		return 0, nil, err
	}
	targetID := strings.TrimSpace(command.TargetID)
	if targetID != "" {
		for index, section := range sections {
			if section.ID == targetID {
				return index + 1, &sections[index], nil
			}
		}
	}
	if command.TargetIndex > 0 {
		if command.TargetIndex > len(sections) {
			return 0, nil, fmt.Errorf("dynamic section index %d out of range", command.TargetIndex)
		}
		return command.TargetIndex, &sections[command.TargetIndex-1], nil
	}

	candidates := []string{
		command.SectionKey,
		command.SectionTitle,
		command.Title,
		command.Reason,
	}
	for _, candidate := range candidates {
		key := normalizeForCompare(candidate)
		if key == "" {
			continue
		}
		for index, section := range sections {
			if normalizeForCompare(section.SectionKey) == key ||
				normalizeForCompare(section.Title) == key ||
				similarStructuredText(section.Title, candidate) ||
				strings.Contains(normalizeForCompare(section.Title), key) ||
				strings.Contains(key, normalizeForCompare(section.Title)) {
				return index + 1, &sections[index], nil
			}
		}
	}
	return 0, nil, fmt.Errorf("dynamic section target not found")
}

func (s *Server) resolveMergeDynamicSectionIDs(ctx context.Context, meetingID string, command InteractionCommand) ([]string, error) {
	sections, err := s.store.ListDynamicSections(ctx, meetingID)
	if err != nil {
		return nil, err
	}
	explicitIDs := append([]string{}, command.SourceIDs...)
	if command.TargetID != "" {
		explicitIDs = append([]string{command.TargetID}, explicitIDs...)
	}
	for _, index := range command.SourceIndices {
		if index <= 0 || index > len(sections) {
			return nil, fmt.Errorf("dynamic section index %d out of range", index)
		}
		explicitIDs = append(explicitIDs, sections[index-1].ID)
	}
	if command.TargetID == "" && command.TargetIndex > 0 {
		if command.TargetIndex > len(sections) {
			return nil, fmt.Errorf("dynamic section index %d out of range", command.TargetIndex)
		}
		explicitIDs = append([]string{sections[command.TargetIndex-1].ID}, explicitIDs...)
	}

	// 已经有明确编号或 ID 时，以显式来源为准，不再用新标题、描述反向猜测目标。
	explicitIDs = uniqueNonEmptyStrings(explicitIDs)
	if len(explicitIDs) >= 2 {
		return explicitIDs, nil
	}
	ids := append([]string{}, explicitIDs...)
	for _, candidate := range []string{command.Instruction, command.Reason} {
		ids = append(ids, dynamicSectionIDsFromText(candidate, sections)...)
	}
	if len(uniqueNonEmptyStrings(ids)) < 2 && len(explicitIDs) == 0 {
		for _, candidate := range []string{command.SectionTitle, command.Title, command.Content} {
			ids = append(ids, dynamicSectionIDsFromText(candidate, sections)...)
		}
	}
	ids = uniqueNonEmptyStrings(ids)
	if len(ids) < 2 {
		return nil, fmt.Errorf("merge requires at least two dynamic section targets")
	}
	return ids, nil
}

func dynamicSectionIDsFromText(candidate string, sections []MeetingDynamicSection) []string {
	ids := []string{}
	key := normalizeForCompare(candidate)
	if key == "" {
		return ids
	}
	for _, section := range sections {
		sectionTitle := normalizeForCompare(section.Title)
		sectionKey := normalizeForCompare(section.SectionKey)
		if (sectionTitle != "" && strings.Contains(key, sectionTitle)) ||
			(sectionKey != "" && strings.Contains(key, sectionKey)) {
			ids = append(ids, section.ID)
		}
	}
	return ids
}

func (s *Server) dynamicSectionSnapshots(ctx context.Context, meetingID string, ids []string) ([]MeetingDynamicSection, error) {
	sections, err := s.store.ListDynamicSections(ctx, meetingID)
	if err != nil {
		return nil, err
	}
	byID := map[string]MeetingDynamicSection{}
	for _, section := range sections {
		byID[section.ID] = section
	}
	result := make([]MeetingDynamicSection, 0, len(ids))
	for _, id := range uniqueNonEmptyStrings(ids) {
		section, ok := byID[id]
		if !ok {
			return nil, fmt.Errorf("dynamic section id %s not found", id)
		}
		result = append(result, section)
	}
	return result, nil
}

func (s *Server) resolveDynamicSectionItemTarget(ctx context.Context, meetingID string, command InteractionCommand) (int, *MeetingDynamicSection, int, *MeetingDynamicSectionItem, error) {
	sections, err := s.store.ListDynamicSections(ctx, meetingID)
	if err != nil {
		return 0, nil, 0, nil, err
	}
	itemIDs := append([]string{}, command.SourceIDs...)
	if strings.TrimSpace(command.TargetID) != "" {
		itemIDs = append(itemIDs, command.TargetID)
	}
	itemIDs = uniqueNonEmptyStrings(itemIDs)
	for _, itemID := range itemIDs {
		for sectionIndex := range sections {
			for itemIndex := range sections[sectionIndex].Items {
				if sections[sectionIndex].Items[itemIndex].ID == itemID {
					return sectionIndex + 1, &sections[sectionIndex], itemIndex + 1, &sections[sectionIndex].Items[itemIndex], nil
				}
			}
		}
	}

	sectionIndex, section, err := s.resolveDynamicSectionTarget(ctx, meetingID, command)
	if err != nil {
		return 0, nil, 0, nil, err
	}
	itemIndex := 0
	if len(command.SourceIndices) > 0 {
		itemIndex = command.SourceIndices[0]
	}
	if itemIndex <= 0 || itemIndex > len(section.Items) {
		return 0, nil, 0, nil, fmt.Errorf("dynamic section item index %d out of range", itemIndex)
	}
	return sectionIndex, section, itemIndex, &section.Items[itemIndex-1], nil
}

func (s *Server) resolveFrameworkSectionTarget(ctx context.Context, meetingID string, command InteractionCommand) (int, *MeetingFrameworkSection, error) {
	snapshot, err := s.store.GetIntelligenceSnapshot(ctx, meetingID)
	if err != nil {
		return 0, nil, err
	}
	sections := snapshot.FrameworkSections
	targetID := strings.TrimSpace(command.TargetID)
	if targetID != "" {
		for index, section := range sections {
			if section.ID == targetID {
				return index + 1, &sections[index], nil
			}
		}
	}
	if command.TargetIndex > 0 {
		if command.TargetIndex > len(sections) {
			return 0, nil, fmt.Errorf("framework section index %d out of range", command.TargetIndex)
		}
		return command.TargetIndex, &sections[command.TargetIndex-1], nil
	}

	candidates := []string{
		command.SectionKey,
		command.SectionTitle,
		command.Title,
		command.Reason,
	}
	for _, candidate := range candidates {
		key := normalizeForCompare(candidate)
		frameworkKey := normalizeFrameworkSectionKey(snapshot.MeetingType, candidate, candidate)
		if frameworkKey == "" && key == "" {
			continue
		}
		for index, section := range sections {
			sectionKey := normalizeFrameworkSectionKey(snapshot.MeetingType, section.SectionKey, section.Title)
			titleKey := normalizeForCompare(section.Title)
			if frameworkKey != "" && sectionKey == frameworkKey {
				return index + 1, &sections[index], nil
			}
			if key != "" && (titleKey == key ||
				strings.Contains(titleKey, key) ||
				strings.Contains(key, titleKey) ||
				similarStructuredText(section.Title, candidate)) {
				return index + 1, &sections[index], nil
			}
		}
	}
	return 0, nil, fmt.Errorf("framework section target not found")
}

func actionItemMutationFromCommand(command InteractionCommand) ActionItemMutation {
	task := strings.TrimSpace(command.Task)
	if task == "" && command.Intent == "merge_action_items" {
		task = strings.TrimSpace(command.Content)
	}
	if task == "" && command.Intent == "update_action_item" {
		task = strings.TrimSpace(command.Content)
	}
	if task == "" && command.Intent == "move_decision_to_action_item" {
		task = strings.TrimSpace(command.Content)
	}
	return ActionItemMutation{
		Task:       task,
		Owner:      strings.TrimSpace(command.Owner),
		DueDate:    strings.TrimSpace(command.DueDate),
		Status:     strings.TrimSpace(command.Status),
		SourceText: strings.TrimSpace(command.Reason),
	}
}

func riskMutationFromCommand(command InteractionCommand) RiskMutation {
	content := strings.TrimSpace(command.Content)
	if content == "" && command.Intent == "merge_risks" {
		content = strings.TrimSpace(command.Task)
	}
	return RiskMutation{
		Content:    content,
		Impact:     strings.TrimSpace(command.Impact),
		Level:      strings.TrimSpace(command.Level),
		Owner:      strings.TrimSpace(command.Owner),
		Mitigation: strings.TrimSpace(command.Mitigation),
		DueDate:    strings.TrimSpace(command.DueDate),
		Status:     strings.TrimSpace(command.Status),
		SourceText: strings.TrimSpace(command.Reason),
	}
}

func openQuestionMutationFromCommand(command InteractionCommand) OpenQuestionMutation {
	question := strings.TrimSpace(command.Question)
	if question == "" {
		question = strings.TrimSpace(command.Content)
	}
	return OpenQuestionMutation{
		Question:   question,
		Owner:      strings.TrimSpace(command.Owner),
		DueDate:    strings.TrimSpace(command.DueDate),
		Status:     strings.TrimSpace(command.Status),
		SourceText: strings.TrimSpace(command.Reason),
	}
}
