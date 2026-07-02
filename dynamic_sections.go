// 本文件负责会议动态模块的存储、查询和去重合并。
package main

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

func (s *Store) ListDynamicSections(ctx context.Context, meetingID string) ([]MeetingDynamicSection, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, meeting_id, section_key, title, description, source, visibility, confidence, display_order, is_deleted, created_at, updated_at
		FROM meeting_dynamic_sections WHERE meeting_id = ? AND is_deleted = 0 ORDER BY display_order ASC, created_at ASC`, meetingID)
	if err != nil {
		return nil, err
	}

	sections := []MeetingDynamicSection{}
	for rows.Next() {
		section, err := scanDynamicSection(rows)
		if err != nil {
			return nil, err
		}
		sections = append(sections, *section)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	for index := range sections {
		items, err := s.listDynamicSectionItems(ctx, meetingID, sections[index].ID)
		if err != nil {
			return nil, err
		}
		sections[index].Items = items
	}
	return sections, nil
}

func (s *Store) listDynamicSectionItems(ctx context.Context, meetingID, sectionID string) ([]MeetingDynamicSectionItem, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, meeting_id, section_id, title, content, fields_json, source_text, confidence, status, display_order, is_deleted, created_at, updated_at
		FROM meeting_dynamic_section_items
		WHERE meeting_id = ? AND section_id = ? AND is_deleted = 0
		ORDER BY display_order ASC, created_at ASC`, meetingID, sectionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := []MeetingDynamicSectionItem{}
	for rows.Next() {
		item, err := scanDynamicSectionItem(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

func (s *Store) SetDynamicSectionVisibility(ctx context.Context, meetingID, sectionID, visibility string) (*MeetingDynamicSection, error) {
	visibility = normalizeDynamicSectionVisibility(visibility)
	now := time.Now().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx, `UPDATE meeting_dynamic_sections
		SET visibility = ?, updated_at = ?
		WHERE id = ? AND meeting_id = ? AND is_deleted = 0`,
		visibility, now, sectionID, meetingID)
	if err != nil {
		return nil, err
	}
	row := s.db.QueryRowContext(ctx, `SELECT id, meeting_id, section_key, title, description, source, visibility, confidence, display_order, is_deleted, created_at, updated_at
		FROM meeting_dynamic_sections
		WHERE id = ? AND meeting_id = ? AND is_deleted = 0`, sectionID, meetingID)
	section, err := scanDynamicSection(row)
	if err != nil {
		return nil, err
	}
	items, err := s.listDynamicSectionItems(ctx, meetingID, section.ID)
	if err != nil {
		return nil, err
	}
	section.Items = items
	return section, nil
}

func (s *Store) SetAllDynamicSectionVisibility(ctx context.Context, meetingID, visibility string) (int, error) {
	visibility = normalizeDynamicSectionVisibility(visibility)
	now := time.Now().Format(time.RFC3339)
	result, err := s.db.ExecContext(ctx, `UPDATE meeting_dynamic_sections
		SET visibility = ?, updated_at = ?
		WHERE meeting_id = ? AND is_deleted = 0`,
		visibility, now, meetingID)
	if err != nil {
		return 0, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}
	return int(affected), nil
}

func (s *Store) CreateDynamicSection(ctx context.Context, meetingID, title, sectionKey, description, content, sourceText string) (*MeetingDynamicSection, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	title = strings.TrimSpace(title)
	if title == "" {
		return nil, fmt.Errorf("dynamic section title is required")
	}
	key := normalizeDynamicSectionKey(sectionKey, title)
	existing, err := s.listDynamicSectionsNoLock(ctx, meetingID)
	if err != nil {
		return nil, err
	}
	if matchingDynamicSectionIndex(existing, key, title) >= 0 {
		return nil, fmt.Errorf("dynamic section already exists: %s", title)
	}

	now := time.Now().Format(time.RFC3339)
	displayOrder := len(existing) + 1
	section := &MeetingDynamicSection{
		ID:           newID("sec"),
		MeetingID:    meetingID,
		SectionKey:   key,
		Title:        title,
		Description:  strings.TrimSpace(description),
		Source:       "user",
		Visibility:   "visible",
		Confidence:   1,
		DisplayOrder: displayOrder,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO meeting_dynamic_sections
		(id, meeting_id, section_key, title, description, source, visibility, confidence, display_order, is_deleted, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 0, ?, ?)`,
		section.ID, section.MeetingID, section.SectionKey, section.Title, section.Description, section.Source,
		section.Visibility, section.Confidence, section.DisplayOrder, section.CreatedAt, section.UpdatedAt)
	if err != nil {
		return nil, err
	}
	item, err := s.addDynamicSectionItemNoLock(ctx, meetingID, section.ID, "", content, sourceText, now)
	if err != nil {
		return nil, err
	}
	if item != nil {
		section.Items = []MeetingDynamicSectionItem{*item}
	}
	return section, nil
}

func (s *Store) AddDynamicSectionItem(ctx context.Context, meetingID, sectionID, title, content, sourceText string) (*MeetingDynamicSectionItem, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if strings.TrimSpace(sectionID) == "" {
		return nil, false, fmt.Errorf("dynamic section id is required")
	}
	if _, err := s.getDynamicSectionNoLock(ctx, meetingID, sectionID); err != nil {
		return nil, false, err
	}
	now := time.Now().Format(time.RFC3339)
	item, err := s.addDynamicSectionItemNoLock(ctx, meetingID, sectionID, title, content, sourceText, now)
	if err != nil {
		return nil, false, err
	}
	if item != nil {
		if _, err := s.db.ExecContext(ctx, `UPDATE meeting_dynamic_sections
			SET visibility = CASE WHEN visibility = 'hidden' THEN 'visible' ELSE visibility END, updated_at = ?
			WHERE meeting_id = ? AND id = ? AND is_deleted = 0`,
			now, meetingID, sectionID); err != nil {
			return nil, false, err
		}
	}
	return item, item != nil, nil
}

func (s *Store) UpdateDynamicSection(ctx context.Context, meetingID, sectionID, title, description string) (*MeetingDynamicSection, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	section, err := s.getDynamicSectionNoLock(ctx, meetingID, sectionID)
	if err != nil {
		return nil, err
	}
	title = strings.TrimSpace(title)
	description = strings.TrimSpace(description)
	if title == "" {
		title = section.Title
	}
	key := section.SectionKey
	if title != section.Title {
		key = normalizeDynamicSectionKey(section.SectionKey, title)
	}
	now := time.Now().Format(time.RFC3339)
	if _, err := s.db.ExecContext(ctx, `UPDATE meeting_dynamic_sections
		SET section_key = ?, title = ?, description = ?, visibility = CASE WHEN visibility = 'hidden' THEN 'visible' ELSE visibility END, updated_at = ?
		WHERE meeting_id = ? AND id = ? AND is_deleted = 0`,
		key, title, description, now, meetingID, sectionID); err != nil {
		return nil, err
	}
	return s.getDynamicSectionNoLock(ctx, meetingID, sectionID)
}

func (s *Store) UpdateDynamicSectionItem(ctx context.Context, meetingID, itemID, title, content string) (*MeetingDynamicSectionItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	item, err := s.getDynamicSectionItemNoLock(ctx, meetingID, itemID)
	if err != nil {
		return nil, err
	}
	title = strings.TrimSpace(title)
	content = strings.TrimSpace(content)
	if title == "" {
		title = item.Title
	}
	if content == "" {
		content = item.Content
	}
	now := time.Now().Format(time.RFC3339)
	if _, err := s.db.ExecContext(ctx, `UPDATE meeting_dynamic_section_items
		SET title = ?, content = ?, updated_at = ?
		WHERE meeting_id = ? AND id = ? AND is_deleted = 0`,
		title, content, now, meetingID, itemID); err != nil {
		return nil, err
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE meeting_dynamic_sections
		SET visibility = CASE WHEN visibility = 'hidden' THEN 'visible' ELSE visibility END, updated_at = ?
		WHERE meeting_id = ? AND id = ? AND is_deleted = 0`,
		now, meetingID, item.SectionID); err != nil {
		return nil, err
	}
	return s.getDynamicSectionItemNoLock(ctx, meetingID, itemID)
}

func (s *Store) DeleteDynamicSection(ctx context.Context, meetingID, sectionID string) (*MeetingDynamicSection, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	section, err := s.getDynamicSectionNoLock(ctx, meetingID, sectionID)
	if err != nil {
		return nil, err
	}
	now := time.Now().Format(time.RFC3339)
	if _, err := s.db.ExecContext(ctx, `UPDATE meeting_dynamic_sections
		SET is_deleted = 1, updated_at = ?
		WHERE meeting_id = ? AND id = ?`, now, meetingID, sectionID); err != nil {
		return nil, err
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE meeting_dynamic_section_items
		SET is_deleted = 1, updated_at = ?
		WHERE meeting_id = ? AND section_id = ?`, now, meetingID, sectionID); err != nil {
		return nil, err
	}
	section.IsDeleted = true
	section.UpdatedAt = now
	for index := range section.Items {
		section.Items[index].IsDeleted = true
		section.Items[index].UpdatedAt = now
	}
	return section, nil
}

func (s *Store) DeleteDynamicSectionItem(ctx context.Context, meetingID, itemID string) (*MeetingDynamicSectionItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	item, err := s.getDynamicSectionItemNoLock(ctx, meetingID, itemID)
	if err != nil {
		return nil, err
	}
	now := time.Now().Format(time.RFC3339)
	if _, err := s.db.ExecContext(ctx, `UPDATE meeting_dynamic_section_items
		SET is_deleted = 1, updated_at = ?
		WHERE meeting_id = ? AND id = ?`, now, meetingID, itemID); err != nil {
		return nil, err
	}
	item.IsDeleted = true
	item.UpdatedAt = now
	return item, nil
}

func (s *Store) MergeDynamicSections(ctx context.Context, meetingID string, sectionIDs []string, title, description string) (*MeetingDynamicSection, []MeetingDynamicSection, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	sectionIDs = uniqueNonEmptyStrings(sectionIDs)
	if len(sectionIDs) < 2 {
		return nil, nil, fmt.Errorf("at least two dynamic sections are required")
	}
	existing, err := s.listDynamicSectionsNoLock(ctx, meetingID)
	if err != nil {
		return nil, nil, err
	}
	byID := map[string]MeetingDynamicSection{}
	for _, section := range existing {
		byID[section.ID] = section
	}
	sections := make([]MeetingDynamicSection, 0, len(sectionIDs))
	for _, id := range sectionIDs {
		section, ok := byID[id]
		if !ok {
			return nil, nil, fmt.Errorf("dynamic section id %s not found", id)
		}
		sections = append(sections, section)
	}

	target := sections[0]
	title = strings.TrimSpace(title)
	if title == "" {
		title = target.Title
	}
	description = strings.TrimSpace(description)
	if description == "" {
		values := make([]string, 0, len(sections))
		for _, section := range sections {
			values = append(values, section.Description)
		}
		description = mergeDistinctText(values...)
	}
	key := normalizeDynamicSectionKey(target.SectionKey, title)
	now := time.Now().Format(time.RFC3339)

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `UPDATE meeting_dynamic_sections SET
		section_key = ?, title = ?, description = ?, visibility = CASE WHEN visibility = 'hidden' THEN 'visible' ELSE visibility END, updated_at = ?
		WHERE meeting_id = ? AND id = ? AND is_deleted = 0`,
		key, title, description, now, meetingID, target.ID); err != nil {
		return nil, nil, err
	}

	currentItems := append([]MeetingDynamicSectionItem{}, target.Items...)
	nextOrder := len(currentItems) + 1
	for _, section := range sections[1:] {
		for _, item := range section.Items {
			if matchingDynamicSectionItemIndex(currentItems, item.Title, item.Content) >= 0 {
				if _, err := tx.ExecContext(ctx, `UPDATE meeting_dynamic_section_items SET is_deleted = 1, updated_at = ? WHERE meeting_id = ? AND id = ?`,
					now, meetingID, item.ID); err != nil {
					return nil, nil, err
				}
				continue
			}
			if _, err := tx.ExecContext(ctx, `UPDATE meeting_dynamic_section_items SET section_id = ?, display_order = ?, is_deleted = 0, updated_at = ?
				WHERE meeting_id = ? AND id = ?`,
				target.ID, nextOrder, now, meetingID, item.ID); err != nil {
				return nil, nil, err
			}
			item.SectionID = target.ID
			item.DisplayOrder = nextOrder
			item.UpdatedAt = now
			currentItems = append(currentItems, item)
			nextOrder++
		}
		if _, err := tx.ExecContext(ctx, `UPDATE meeting_dynamic_sections SET is_deleted = 1, updated_at = ? WHERE meeting_id = ? AND id = ?`,
			now, meetingID, section.ID); err != nil {
			return nil, nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, nil, err
	}

	merged, err := s.getDynamicSectionNoLock(ctx, meetingID, target.ID)
	if err != nil {
		return nil, nil, err
	}
	return merged, sections[1:], nil
}

func (s *Store) RestoreDynamicSection(ctx context.Context, section MeetingDynamicSection) (*MeetingDynamicSection, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if strings.TrimSpace(section.ID) == "" || strings.TrimSpace(section.MeetingID) == "" {
		return nil, fmt.Errorf("dynamic section id and meeting id are required")
	}
	if strings.TrimSpace(section.Title) == "" {
		return nil, fmt.Errorf("dynamic section title is required")
	}
	now := time.Now().Format(time.RFC3339)
	if section.CreatedAt == "" {
		section.CreatedAt = now
	}
	section.UpdatedAt = now
	section.SectionKey = normalizeDynamicSectionKey(section.SectionKey, section.Title)
	section.Visibility = normalizeDynamicSectionVisibility(section.Visibility)
	if section.Source == "" {
		section.Source = "user"
	}
	if section.Confidence == 0 {
		section.Confidence = 1
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO meeting_dynamic_sections
		(id, meeting_id, section_key, title, description, source, visibility, confidence, display_order, is_deleted, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 0, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			meeting_id = excluded.meeting_id,
			section_key = excluded.section_key,
			title = excluded.title,
			description = excluded.description,
			source = excluded.source,
			visibility = excluded.visibility,
			confidence = excluded.confidence,
			display_order = excluded.display_order,
			is_deleted = 0,
			updated_at = excluded.updated_at`,
		section.ID, section.MeetingID, section.SectionKey, section.Title, section.Description, section.Source,
		section.Visibility, section.Confidence, section.DisplayOrder, section.CreatedAt, section.UpdatedAt)
	if err != nil {
		return nil, err
	}
	for _, item := range section.Items {
		item.MeetingID = section.MeetingID
		item.SectionID = section.ID
		if err := s.restoreDynamicSectionItemNoLock(ctx, item, now); err != nil {
			return nil, err
		}
	}
	return s.getDynamicSectionNoLock(ctx, section.MeetingID, section.ID)
}

func (s *Store) listDynamicSectionsNoLock(ctx context.Context, meetingID string) ([]MeetingDynamicSection, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, meeting_id, section_key, title, description, source, visibility, confidence, display_order, is_deleted, created_at, updated_at
		FROM meeting_dynamic_sections WHERE meeting_id = ? AND is_deleted = 0 ORDER BY display_order ASC, created_at ASC`, meetingID)
	if err != nil {
		return nil, err
	}
	sections := []MeetingDynamicSection{}
	for rows.Next() {
		section, err := scanDynamicSection(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		sections = append(sections, *section)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	for index := range sections {
		items, err := s.listDynamicSectionItems(ctx, meetingID, sections[index].ID)
		if err != nil {
			return nil, err
		}
		sections[index].Items = items
	}
	return sections, nil
}

func (s *Store) getDynamicSectionNoLock(ctx context.Context, meetingID, sectionID string) (*MeetingDynamicSection, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, meeting_id, section_key, title, description, source, visibility, confidence, display_order, is_deleted, created_at, updated_at
		FROM meeting_dynamic_sections
		WHERE id = ? AND meeting_id = ? AND is_deleted = 0`, sectionID, meetingID)
	section, err := scanDynamicSection(row)
	if err != nil {
		return nil, err
	}
	items, err := s.listDynamicSectionItems(ctx, meetingID, section.ID)
	if err != nil {
		return nil, err
	}
	section.Items = items
	return section, nil
}

func (s *Store) getDynamicSectionItemNoLock(ctx context.Context, meetingID, itemID string) (*MeetingDynamicSectionItem, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, meeting_id, section_id, title, content, fields_json, source_text, confidence, status, display_order, is_deleted, created_at, updated_at
		FROM meeting_dynamic_section_items
		WHERE id = ? AND meeting_id = ? AND is_deleted = 0`, itemID, meetingID)
	return scanDynamicSectionItem(row)
}

func (s *Store) addDynamicSectionItemNoLock(ctx context.Context, meetingID, sectionID, title, content, sourceText, now string) (*MeetingDynamicSectionItem, error) {
	title = strings.TrimSpace(title)
	content = strings.TrimSpace(content)
	if title == "" && content == "" {
		return nil, nil
	}
	if title == "" {
		title = dynamicItemTitleFromContent(content)
	}
	if content == "" {
		content = title
	}
	sourceText = strings.TrimSpace(sourceText)
	existing, err := s.listDynamicSectionItems(ctx, meetingID, sectionID)
	if err != nil {
		return nil, err
	}
	if index := matchingDynamicSectionItemIndex(existing, title, content); index >= 0 {
		merged := existing[index]
		updatedTitle := richerText(merged.Title, title)
		updatedContent := richerText(merged.Content, content)
		updatedSource := mergeSourceText(merged.SourceText, sourceText)
		if updatedTitle == merged.Title && updatedContent == merged.Content && updatedSource == merged.SourceText {
			return &merged, nil
		}
		if _, err := s.db.ExecContext(ctx, `UPDATE meeting_dynamic_section_items SET
			title = ?, content = ?, source_text = ?, updated_at = ?
			WHERE id = ?`,
			updatedTitle, updatedContent, updatedSource, now, merged.ID); err != nil {
			return nil, err
		}
		return s.getDynamicSectionItemNoLock(ctx, meetingID, merged.ID)
	}
	item := &MeetingDynamicSectionItem{
		ID:           newID("secitem"),
		MeetingID:    meetingID,
		SectionID:    sectionID,
		Title:        title,
		Content:      content,
		SourceText:   sourceText,
		Confidence:   1,
		Status:       "open",
		DisplayOrder: len(existing) + 1,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO meeting_dynamic_section_items
		(id, meeting_id, section_id, title, content, fields_json, source_text, confidence, status, display_order, is_deleted, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, '', ?, ?, ?, ?, 0, ?, ?)`,
		item.ID, item.MeetingID, item.SectionID, item.Title, item.Content, item.SourceText, item.Confidence,
		item.Status, item.DisplayOrder, item.CreatedAt, item.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return item, nil
}

func (s *Store) restoreDynamicSectionItemNoLock(ctx context.Context, item MeetingDynamicSectionItem, now string) error {
	if strings.TrimSpace(item.ID) == "" || strings.TrimSpace(item.MeetingID) == "" || strings.TrimSpace(item.SectionID) == "" {
		return fmt.Errorf("dynamic section item id, meeting id and section id are required")
	}
	item.Title = strings.TrimSpace(item.Title)
	item.Content = strings.TrimSpace(item.Content)
	if item.Title == "" && item.Content == "" {
		return nil
	}
	if item.Title == "" {
		item.Title = dynamicItemTitleFromContent(item.Content)
	}
	if item.Content == "" {
		item.Content = item.Title
	}
	if item.CreatedAt == "" {
		item.CreatedAt = now
	}
	item.UpdatedAt = now
	if item.Status == "" {
		item.Status = "open"
	}
	if item.Confidence == 0 {
		item.Confidence = 1
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO meeting_dynamic_section_items
		(id, meeting_id, section_id, title, content, fields_json, source_text, confidence, status, display_order, is_deleted, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			meeting_id = excluded.meeting_id,
			section_id = excluded.section_id,
			title = excluded.title,
			content = excluded.content,
			fields_json = excluded.fields_json,
			source_text = excluded.source_text,
			confidence = excluded.confidence,
			status = excluded.status,
			display_order = excluded.display_order,
			is_deleted = 0,
			updated_at = excluded.updated_at`,
		item.ID, item.MeetingID, item.SectionID, item.Title, item.Content, item.FieldsJSON, item.SourceText, item.Confidence,
		item.Status, item.DisplayOrder, item.CreatedAt, item.UpdatedAt)
	return err
}

func saveDynamicSectionExtractionsTx(ctx context.Context, tx *sql.Tx, meetingID string, existing []MeetingDynamicSection, sections []DynamicSectionExtraction, now string) error {
	for _, section := range sections {
		title := strings.TrimSpace(section.Title)
		if title == "" || normalizeForCompare(title) == "" {
			continue
		}
		if looksLikeInteractionCommandText(title) || looksLikeInteractionCommandText(section.Description) {
			continue
		}
		key := normalizeDynamicSectionKey(section.SectionKey, title)
		description := strings.TrimSpace(section.Description)
		index := matchingDynamicSectionIndex(existing, key, title)
		if index >= 0 {
			merged := existing[index]
			updatedTitle := richerText(merged.Title, title)
			updatedDescription := richerText(merged.Description, description)
			updatedVisibility := merged.Visibility
			if len(section.Items) > 0 && updatedVisibility == "hidden" {
				updatedVisibility = "visible"
			}
			if updatedTitle != merged.Title || updatedDescription != merged.Description || updatedVisibility != merged.Visibility || merged.SectionKey == "" {
				_, err := tx.ExecContext(ctx, `UPDATE meeting_dynamic_sections SET
					section_key = ?, title = ?, description = ?, visibility = ?, updated_at = ?
					WHERE id = ?`,
					key, updatedTitle, updatedDescription, updatedVisibility, now, merged.ID)
				if err != nil {
					return err
				}
				merged.SectionKey = key
				merged.Title = updatedTitle
				merged.Description = updatedDescription
				merged.Visibility = updatedVisibility
				merged.UpdatedAt = now
			}
			if err := saveDynamicSectionItemsTx(ctx, tx, meetingID, merged.ID, merged.Items, section.Items, now); err != nil {
				return err
			}
			items, err := dynamicSectionItemsTx(ctx, tx, meetingID, merged.ID)
			if err != nil {
				return err
			}
			merged.Items = items
			existing[index] = merged
			continue
		}

		sectionID := newID("sec")
		displayOrder := len(existing) + 1
		visibility := "hidden"
		if len(section.Items) > 0 {
			visibility = "visible"
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO meeting_dynamic_sections
			(id, meeting_id, section_key, title, description, source, visibility, confidence, display_order, is_deleted, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, 'auto', ?, 0.65, ?, 0, ?, ?)`,
			sectionID, meetingID, key, title, description, visibility, displayOrder, now, now)
		if err != nil {
			return err
		}
		inserted := MeetingDynamicSection{
			ID:           sectionID,
			MeetingID:    meetingID,
			SectionKey:   key,
			Title:        title,
			Description:  description,
			Source:       "auto",
			Visibility:   visibility,
			Confidence:   0.65,
			DisplayOrder: displayOrder,
			CreatedAt:    now,
			UpdatedAt:    now,
		}
		if err := saveDynamicSectionItemsTx(ctx, tx, meetingID, sectionID, nil, section.Items, now); err != nil {
			return err
		}
		items, err := dynamicSectionItemsTx(ctx, tx, meetingID, sectionID)
		if err != nil {
			return err
		}
		inserted.Items = items
		existing = append(existing, inserted)
	}
	return nil
}

func saveDynamicSectionItemsTx(ctx context.Context, tx *sql.Tx, meetingID, sectionID string, existing []MeetingDynamicSectionItem, items []DynamicSectionItemExtraction, now string) error {
	for _, item := range items {
		title := strings.TrimSpace(item.Title)
		content := strings.TrimSpace(item.Content)
		if title == "" && content == "" {
			continue
		}
		if title == "" {
			title = dynamicItemTitleFromContent(content)
		}
		if content == "" {
			content = title
		}
		sourceText := strings.TrimSpace(item.SourceText)
		if looksLikeInteractionCommandText(title) || looksLikeInteractionCommandText(content) || looksLikeInteractionCommandText(sourceText) {
			continue
		}
		if index := matchingDynamicSectionItemIndex(existing, title, content); index >= 0 {
			merged := existing[index]
			updatedTitle := richerText(merged.Title, title)
			updatedContent := richerText(merged.Content, content)
			updatedSource := mergeSourceText(merged.SourceText, sourceText)
			if updatedTitle == merged.Title && updatedContent == merged.Content && updatedSource == merged.SourceText {
				continue
			}
			_, err := tx.ExecContext(ctx, `UPDATE meeting_dynamic_section_items SET
				title = ?, content = ?, source_text = ?, updated_at = ?
				WHERE id = ?`,
				updatedTitle, updatedContent, updatedSource, now, merged.ID)
			if err != nil {
				return err
			}
			merged.Title = updatedTitle
			merged.Content = updatedContent
			merged.SourceText = updatedSource
			merged.UpdatedAt = now
			existing[index] = merged
			continue
		}

		itemID := newID("secitem")
		displayOrder := len(existing) + 1
		_, err := tx.ExecContext(ctx, `INSERT INTO meeting_dynamic_section_items
			(id, meeting_id, section_id, title, content, fields_json, source_text, confidence, status, display_order, is_deleted, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, '', ?, 0.65, 'open', ?, 0, ?, ?)`,
			itemID, meetingID, sectionID, title, content, sourceText, displayOrder, now, now)
		if err != nil {
			return err
		}
		existing = append(existing, MeetingDynamicSectionItem{
			ID:           itemID,
			MeetingID:    meetingID,
			SectionID:    sectionID,
			Title:        title,
			Content:      content,
			SourceText:   sourceText,
			Confidence:   0.65,
			Status:       "open",
			DisplayOrder: displayOrder,
			CreatedAt:    now,
			UpdatedAt:    now,
		})
	}
	return nil
}

func mergeExistingDynamicSectionItemsForSectionsTx(ctx context.Context, tx *sql.Tx, sections []MeetingDynamicSection, now string) ([]MeetingDynamicSection, error) {
	for index := range sections {
		items, err := mergeExistingDynamicSectionItemsTx(ctx, tx, sections[index].Items, now)
		if err != nil {
			return nil, err
		}
		sections[index].Items = items
	}
	return sections, nil
}

func mergeExistingDynamicSectionItemsTx(ctx context.Context, tx *sql.Tx, items []MeetingDynamicSectionItem, now string) ([]MeetingDynamicSectionItem, error) {
	merged := make([]MeetingDynamicSectionItem, 0, len(items))
	for _, item := range items {
		if index := matchingDynamicSectionItemIndex(merged, item.Title, item.Content); index >= 0 {
			target := merged[index]
			updatedTitle := richerText(target.Title, item.Title)
			updatedContent := richerText(target.Content, item.Content)
			updatedSource := mergeSourceText(target.SourceText, item.SourceText)
			if updatedTitle != target.Title || updatedContent != target.Content || updatedSource != target.SourceText {
				if _, err := tx.ExecContext(ctx, `UPDATE meeting_dynamic_section_items SET
					title = ?, content = ?, source_text = ?, updated_at = ?
					WHERE id = ?`,
					updatedTitle, updatedContent, updatedSource, now, target.ID); err != nil {
					return nil, err
				}
				target.Title = updatedTitle
				target.Content = updatedContent
				target.SourceText = updatedSource
				target.UpdatedAt = now
				merged[index] = target
			}
			if _, err := tx.ExecContext(ctx, `UPDATE meeting_dynamic_section_items SET is_deleted = 1, updated_at = ? WHERE id = ?`, now, item.ID); err != nil {
				return nil, err
			}
			continue
		}
		merged = append(merged, item)
	}
	return merged, nil
}

func dynamicSectionsTx(ctx context.Context, tx *sql.Tx, meetingID string) ([]MeetingDynamicSection, error) {
	rows, err := tx.QueryContext(ctx, `SELECT id, meeting_id, section_key, title, description, source, visibility, confidence, display_order, is_deleted, created_at, updated_at
		FROM meeting_dynamic_sections WHERE meeting_id = ? AND is_deleted = 0 ORDER BY display_order ASC, created_at ASC`, meetingID)
	if err != nil {
		return nil, err
	}

	sections := []MeetingDynamicSection{}
	for rows.Next() {
		section, err := scanDynamicSection(rows)
		if err != nil {
			return nil, err
		}
		sections = append(sections, *section)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	for index := range sections {
		items, err := dynamicSectionItemsTx(ctx, tx, meetingID, sections[index].ID)
		if err != nil {
			return nil, err
		}
		sections[index].Items = items
	}
	return sections, nil
}

func dynamicSectionItemsTx(ctx context.Context, tx *sql.Tx, meetingID, sectionID string) ([]MeetingDynamicSectionItem, error) {
	rows, err := tx.QueryContext(ctx, `SELECT id, meeting_id, section_id, title, content, fields_json, source_text, confidence, status, display_order, is_deleted, created_at, updated_at
		FROM meeting_dynamic_section_items
		WHERE meeting_id = ? AND section_id = ? AND is_deleted = 0
		ORDER BY display_order ASC, created_at ASC`, meetingID, sectionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := []MeetingDynamicSectionItem{}
	for rows.Next() {
		item, err := scanDynamicSectionItem(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

func scanDynamicSection(scanner interface {
	Scan(dest ...any) error
}) (*MeetingDynamicSection, error) {
	var section MeetingDynamicSection
	var deleted int
	if err := scanner.Scan(&section.ID, &section.MeetingID, &section.SectionKey, &section.Title,
		&section.Description, &section.Source, &section.Visibility, &section.Confidence,
		&section.DisplayOrder, &deleted, &section.CreatedAt, &section.UpdatedAt); err != nil {
		return nil, err
	}
	section.IsDeleted = deleted != 0
	if strings.TrimSpace(section.Visibility) == "" {
		section.Visibility = "hidden"
	}
	if strings.TrimSpace(section.Source) == "" {
		section.Source = "auto"
	}
	return &section, nil
}

func scanDynamicSectionItem(scanner interface {
	Scan(dest ...any) error
}) (*MeetingDynamicSectionItem, error) {
	var item MeetingDynamicSectionItem
	var deleted int
	if err := scanner.Scan(&item.ID, &item.MeetingID, &item.SectionID, &item.Title,
		&item.Content, &item.FieldsJSON, &item.SourceText, &item.Confidence, &item.Status,
		&item.DisplayOrder, &deleted, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return nil, err
	}
	item.IsDeleted = deleted != 0
	if strings.TrimSpace(item.Status) == "" {
		item.Status = "open"
	}
	return &item, nil
}

func matchingDynamicSectionIndex(existing []MeetingDynamicSection, key, title string) int {
	for index, section := range existing {
		if key != "" && normalizeForCompare(section.SectionKey) == normalizeForCompare(key) {
			return index
		}
		if similarStructuredText(section.Title, title) {
			return index
		}
	}
	return -1
}

func matchingDynamicSectionItemIndex(existing []MeetingDynamicSectionItem, title, content string) int {
	for index, item := range existing {
		if structuredTextShouldMerge(item.Title, title) || structuredTextShouldMerge(item.Content, content) {
			return index
		}
	}
	return -1
}

func normalizeDynamicSectionKey(key, title string) string {
	key = normalizeDynamicSectionKeyInput(key)
	if key != "" {
		return key
	}
	titleKey := normalizeForCompare(title)
	switch {
	case strings.Contains(titleKey, "预算"):
		return "budget_constraints"
	case strings.Contains(titleKey, "客户") || strings.Contains(titleKey, "用户"):
		return "customer_focus"
	case strings.Contains(titleKey, "硬件"):
		return "hardware_dependencies"
	case strings.Contains(titleKey, "合规"):
		return "compliance"
	case strings.Contains(titleKey, "竞品"):
		return "competitor_info"
	case strings.Contains(titleKey, "支持理由") || strings.Contains(titleKey, "支持"):
		return "supporting_reasons"
	case strings.Contains(titleKey, "反对") || strings.Contains(titleKey, "异议"):
		return "opposing_reasons"
	case strings.Contains(titleKey, "经验") || strings.Contains(titleKey, "沉淀"):
		return "lessons_learned"
	default:
		return titleKey
	}
}

func normalizeDynamicSectionKeyInput(key string) string {
	key = strings.ToLower(strings.TrimSpace(key))
	if key == "" {
		return ""
	}
	var builder strings.Builder
	lastUnderscore := false
	for _, r := range key {
		switch {
		case r >= 'a' && r <= 'z':
			builder.WriteRune(r)
			lastUnderscore = false
		case r >= '0' && r <= '9':
			builder.WriteRune(r)
			lastUnderscore = false
		case r == '_' || r == '-' || r == ' ':
			if !lastUnderscore && builder.Len() > 0 {
				builder.WriteByte('_')
				lastUnderscore = true
			}
		}
	}
	return strings.Trim(builder.String(), "_")
}

func normalizeDynamicSectionVisibility(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "visible", "show", "shown":
		return "visible"
	case "pinned", "pin":
		return "pinned"
	case "hidden", "hide", "closed":
		return "hidden"
	default:
		return "hidden"
	}
}

func dynamicItemTitleFromContent(content string) string {
	content = strings.TrimSpace(content)
	runes := []rune(content)
	if len(runes) <= 18 {
		return content
	}
	return string(runes[:18])
}
