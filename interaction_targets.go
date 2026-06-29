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
