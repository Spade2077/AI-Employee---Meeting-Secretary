// 本文件负责验证本地存储、去重合并和转写保存行为。
package main

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"meeting-core-service/internal/stt"

	_ "modernc.org/sqlite"
)

func TestSaveIntelligenceExtractionMergesNearDuplicateActionItems(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	store := &Store{db: db}
	if err := store.Init(ctx); err != nil {
		t.Fatal(err)
	}
	meeting, _, err := store.CreateMeeting(ctx, "dedupe", "test", "test")
	if err != nil {
		t.Fatal(err)
	}

	_, err = store.SaveIntelligenceExtraction(ctx, meeting.ID, IntelligenceExtraction{
		Summary: SummaryExtraction{Brief: "讨论火山 ASR 连接保持。"},
		ActionItems: []ActionItemExtraction{{
			Task:   "继续排查Volcano ASR连接保持逻辑，重点关注是否应该在暂停期间保持连接以及是否需要等待",
			Owner:  "王磊",
			Status: "open",
		}},
	}, 1)
	if err != nil {
		t.Fatal(err)
	}

	snapshot, err := store.SaveIntelligenceExtraction(ctx, meeting.ID, IntelligenceExtraction{
		Summary: SummaryExtraction{Brief: "继续讨论火山 ASR 连接保持。"},
		ActionItems: []ActionItemExtraction{{
			Task:    "排查Volcano ASR连接保持逻辑，重点关注是否应该在暂停期间保持连接以及是否需要等待会议结束时的服务器正常关闭，并在本周五前给出结论。",
			Owner:   "王磊",
			DueDate: "本周五前",
			Status:  "open",
		}},
	}, 2)
	if err != nil {
		t.Fatal(err)
	}

	if len(snapshot.ActionItems) != 1 {
		t.Fatalf("expected 1 merged action item, got %d: %#v", len(snapshot.ActionItems), snapshot.ActionItems)
	}
	item := snapshot.ActionItems[0]
	if !strings.Contains(item.Task, "服务器正常关闭") {
		t.Fatalf("expected richer task to be retained, got %q", item.Task)
	}
	if item.Owner != "王磊" {
		t.Fatalf("expected owner to stay filled, got %q", item.Owner)
	}
	if item.DueDate != "本周五前" {
		t.Fatalf("expected due date to be filled, got %q", item.DueDate)
	}
}

func TestSaveIntelligenceExtractionAppliesLaterActionItemRevision(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)

	store := &Store{db: db}
	if err := store.Init(ctx); err != nil {
		t.Fatal(err)
	}
	meeting, _, err := store.CreateMeeting(ctx, "revision", "test", "test")
	if err != nil {
		t.Fatal(err)
	}

	_, err = store.SaveIntelligenceExtraction(ctx, meeting.ID, IntelligenceExtraction{
		ActionItems: []ActionItemExtraction{{
			Task:       "完成展厅演示系统联调",
			Owner:      "周明",
			DueDate:    "这周",
			Status:     "open",
			SourceText: "周明说展厅演示系统这周能做完。",
		}},
	}, 1)
	if err != nil {
		t.Fatal(err)
	}

	snapshot, err := store.SaveIntelligenceExtraction(ctx, meeting.ID, IntelligenceExtraction{
		ActionItems: []ActionItemExtraction{{
			Task:       "完成展厅演示系统联调",
			Owner:      "周明",
			DueDate:    "下周",
			Status:     "open",
			SourceText: "李娜说不行，要下周。周明说那好，就下周。",
		}},
	}, 2)
	if err != nil {
		t.Fatal(err)
	}

	if len(snapshot.ActionItems) != 1 {
		t.Fatalf("expected revised action item to merge into one row, got %d: %#v", len(snapshot.ActionItems), snapshot.ActionItems)
	}
	item := snapshot.ActionItems[0]
	if item.DueDate != "下周" {
		t.Fatalf("expected due date to be revised to 下周, got %q", item.DueDate)
	}
	if !strings.Contains(item.SourceText, "那好，就下周") {
		t.Fatalf("expected revision evidence to be retained in source_text, got %q", item.SourceText)
	}
}

func TestMergeActionItemsOnlyKeepsCompletedWhenAllSourcesCompleted(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	store := &Store{db: db}
	if err := store.Init(ctx); err != nil {
		t.Fatal(err)
	}
	meeting, _, err := store.CreateMeeting(ctx, "merge-status", "test", "test")
	if err != nil {
		t.Fatal(err)
	}
	done, err := store.CreateActionItem(ctx, meeting.ID, ActionItemMutation{Task: "Finish camera wiring", Status: "completed"})
	if err != nil {
		t.Fatal(err)
	}
	open, err := store.CreateActionItem(ctx, meeting.ID, ActionItemMutation{Task: "Confirm onsite network", Status: "open"})
	if err != nil {
		t.Fatal(err)
	}
	merged, _, err := store.MergeActionItems(ctx, meeting.ID, []string{done.ID, open.ID}, ActionItemMutation{})
	if err != nil {
		t.Fatal(err)
	}
	if merged.Status != "open" {
		t.Fatalf("expected mixed completed/open merge to stay open, got %#v", merged)
	}

	doneA, err := store.CreateActionItem(ctx, meeting.ID, ActionItemMutation{Task: "Close checklist", Status: "completed"})
	if err != nil {
		t.Fatal(err)
	}
	doneB, err := store.CreateActionItem(ctx, meeting.ID, ActionItemMutation{Task: "Archive test report", Status: "completed"})
	if err != nil {
		t.Fatal(err)
	}
	merged, _, err = store.MergeActionItems(ctx, meeting.ID, []string{doneA.ID, doneB.ID}, ActionItemMutation{})
	if err != nil {
		t.Fatal(err)
	}
	if merged.Status != "completed" {
		t.Fatalf("expected all-completed merge to stay completed, got %#v", merged)
	}
}

func TestSaveIntelligenceExtractionAppliesEndOfMeetingActionItemRevision(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	store := &Store{db: db}
	if err := store.Init(ctx); err != nil {
		t.Fatal(err)
	}
	meeting, _, err := store.CreateMeeting(ctx, "end-revision", "test", "test")
	if err != nil {
		t.Fatal(err)
	}

	_, err = store.SaveIntelligenceExtraction(ctx, meeting.ID, IntelligenceExtraction{
		Summary: SummaryExtraction{Brief: "会议前半段确认压测脚本交付节奏。"},
		ActionItems: []ActionItemExtraction{{
			Task:       "完成支付链路压测脚本",
			Owner:      "陈晨",
			DueDate:    "周三",
			Status:     "open",
			SourceText: "会议前半段陈晨说支付链路压测脚本周三完成。",
		}},
	}, 1)
	if err != nil {
		t.Fatal(err)
	}

	_, err = store.SaveIntelligenceExtraction(ctx, meeting.ID, IntelligenceExtraction{
		Summary: SummaryExtraction{Brief: "中途补充了接口清单整理任务。"},
		ActionItems: []ActionItemExtraction{{
			Task:       "整理回调接口字段清单",
			Owner:      "赵强",
			DueDate:    "周四",
			Status:     "open",
			SourceText: "赵强周四整理回调接口字段清单。",
		}},
	}, 2)
	if err != nil {
		t.Fatal(err)
	}

	snapshot, err := store.SaveIntelligenceExtraction(ctx, meeting.ID, IntelligenceExtraction{
		Summary: SummaryExtraction{Brief: "会议结束前确认压测脚本最终交付时间改为周五。"},
		ActionItems: []ActionItemExtraction{{
			Task:       "完成支付链路压测脚本",
			Owner:      "陈晨",
			DueDate:    "周五",
			Status:     "open",
			SourceText: "会议结束前陈晨说周三来不及，改到周五；大家确认最终以周五为准。",
		}},
	}, 3)
	if err != nil {
		t.Fatal(err)
	}

	if len(snapshot.ActionItems) != 2 {
		t.Fatalf("expected revised item plus unrelated item, got %d: %#v", len(snapshot.ActionItems), snapshot.ActionItems)
	}
	revised := findActionItemByTaskPart(snapshot.ActionItems, "支付链路压测脚本")
	if revised == nil {
		t.Fatalf("expected revised action item to exist, got %#v", snapshot.ActionItems)
	}
	if revised.DueDate != "周五" {
		t.Fatalf("expected end-of-meeting revision to update due date to 周五, got %q", revised.DueDate)
	}
	if !strings.Contains(revised.SourceText, "最终以周五为准") {
		t.Fatalf("expected end-of-meeting revision evidence to be retained, got %q", revised.SourceText)
	}
	unchanged := findActionItemByTaskPart(snapshot.ActionItems, "回调接口字段清单")
	if unchanged == nil || unchanged.DueDate != "周四" {
		t.Fatalf("expected unrelated action item to remain unchanged, got %#v", snapshot.ActionItems)
	}
}

func TestSaveIntelligenceExtractionAppliesRiskRevision(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	store := &Store{db: db}
	if err := store.Init(ctx); err != nil {
		t.Fatal(err)
	}
	meeting, _, err := store.CreateMeeting(ctx, "risk-revision", "test", "test")
	if err != nil {
		t.Fatal(err)
	}

	_, err = store.SaveIntelligenceExtraction(ctx, meeting.ID, IntelligenceExtraction{
		Risks: []RiskExtraction{{
			Content:    "现场网络波动可能影响演示",
			Impact:     "演示过程可能中断",
			Level:      "medium",
			Owner:      "王磊",
			Mitigation: "准备备用热点",
			DueDate:    "周四",
			Status:     "open",
			SourceText: "王磊周四前准备备用热点，避免现场网络波动影响演示。",
		}},
	}, 1)
	if err != nil {
		t.Fatal(err)
	}

	snapshot, err := store.SaveIntelligenceExtraction(ctx, meeting.ID, IntelligenceExtraction{
		Risks: []RiskExtraction{{
			Content:    "现场网络波动可能影响演示",
			Impact:     "演示过程可能中断",
			Level:      "high",
			Owner:      "王磊",
			Mitigation: "改用有线网络并保留备用热点",
			DueDate:    "周五",
			Status:     "open",
			SourceText: "会议结束前确认备用热点不够稳，处理方案改为有线网络并保留备用热点，时间改到周五。",
		}},
	}, 2)
	if err != nil {
		t.Fatal(err)
	}

	if len(snapshot.Risks) != 1 {
		t.Fatalf("expected revised risk to merge into one row, got %d: %#v", len(snapshot.Risks), snapshot.Risks)
	}
	risk := snapshot.Risks[0]
	if risk.Mitigation != "改用有线网络并保留备用热点" || risk.DueDate != "周五" {
		t.Fatalf("expected risk mitigation and due date to be revised, got %#v", risk)
	}
	if !strings.Contains(risk.SourceText, "处理方案改为有线网络") {
		t.Fatalf("expected risk revision evidence to be retained, got %q", risk.SourceText)
	}
}

func TestSaveIntelligenceExtractionMergesNearDuplicateRisksAndOpenQuestions(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	store := &Store{db: db}
	if err := store.Init(ctx); err != nil {
		t.Fatal(err)
	}
	meeting, _, err := store.CreateMeeting(ctx, "risk-question-dedupe", "test", "test")
	if err != nil {
		t.Fatal(err)
	}

	_, err = store.SaveIntelligenceExtraction(ctx, meeting.ID, IntelligenceExtraction{
		Risks: []RiskExtraction{{
			Content:    "麦克风稳定性风险影响现场演示",
			Impact:     "现场演示可能中断",
			Level:      "medium",
			Status:     "open",
			SourceText: "麦克风稳定性这里还有风险，可能会影响现场演示。",
		}},
		OpenQuestions: []OpenQuestionExtraction{{
			Question:   "是否需要为展会演示准备离线模式",
			Status:     "open",
			SourceText: "离线模式要不要准备还没定。",
		}},
	}, 1)
	if err != nil {
		t.Fatal(err)
	}

	snapshot, err := store.SaveIntelligenceExtraction(ctx, meeting.ID, IntelligenceExtraction{
		Risks: []RiskExtraction{{
			Content:    "麦克风稳定性风险影响现场演示，需要准备备用采集方案",
			Impact:     "现场演示可能中断",
			Level:      "high",
			Owner:      "李娜",
			Mitigation: "准备备用采集方案",
			DueDate:    "周四前",
			Status:     "open",
			SourceText: "李娜周四前准备备用采集方案，避免麦克风稳定性影响现场演示。",
		}},
		OpenQuestions: []OpenQuestionExtraction{{
			Question:   "是否需要为展会演示准备离线模式，周四前确认",
			Owner:      "赵强",
			DueDate:    "周四前",
			Status:     "open",
			SourceText: "赵强周四前确认展会演示是否需要离线模式。",
		}},
	}, 2)
	if err != nil {
		t.Fatal(err)
	}

	if len(snapshot.Risks) != 1 {
		t.Fatalf("expected 1 merged risk, got %d: %#v", len(snapshot.Risks), snapshot.Risks)
	}
	risk := snapshot.Risks[0]
	if !strings.Contains(risk.Content, "备用采集方案") {
		t.Fatalf("expected richer risk content to be retained, got %q", risk.Content)
	}
	if risk.Level != "high" || risk.Owner != "李娜" || risk.Mitigation != "准备备用采集方案" || risk.DueDate != "周四前" {
		t.Fatalf("expected risk fields to be merged, got %#v", risk)
	}
	if !strings.Contains(risk.SourceText, "李娜周四前") {
		t.Fatalf("expected richer risk source_text to be retained, got %q", risk.SourceText)
	}

	if len(snapshot.OpenQuestions) != 1 {
		t.Fatalf("expected 1 merged open question, got %d: %#v", len(snapshot.OpenQuestions), snapshot.OpenQuestions)
	}
	question := snapshot.OpenQuestions[0]
	if !strings.Contains(question.Question, "周四前确认") {
		t.Fatalf("expected richer open question to be retained, got %q", question.Question)
	}
	if question.Owner != "赵强" || question.DueDate != "周四前" {
		t.Fatalf("expected open question fields to be merged, got %#v", question)
	}
	if !strings.Contains(question.SourceText, "赵强周四前") {
		t.Fatalf("expected richer open question source_text to be retained, got %q", question.SourceText)
	}
}

func TestSaveIntelligenceExtractionStoresFormalIntelligenceShape(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	store := &Store{db: db}
	if err := store.Init(ctx); err != nil {
		t.Fatal(err)
	}
	meeting, _, err := store.CreateMeeting(ctx, "", "test", "test")
	if err != nil {
		t.Fatal(err)
	}

	snapshot, err := store.SaveIntelligenceExtraction(ctx, meeting.ID, IntelligenceExtraction{
		Title:       "供应链系统评审会",
		MeetingType: "project_followup",
		Summary:     SummaryExtraction{Brief: "本次会议确认了供应链系统的联调节奏、验收口径和接口延期风险。"},
		Decisions: []DecisionExtraction{{
			Content:    "联调环境继续沿用现有网关方案。",
			SourceText: "大家确认联调环境继续沿用现有网关方案。",
		}},
		ActionItems: []ActionItemExtraction{{
			Task:       "整理供应商接口字段差异清单",
			Owner:      "李娜",
			Status:     "open",
			SourceText: "李娜整理供应商接口字段差异清单。",
		}},
		Risks: []RiskExtraction{{
			Content:    "供应商接口延期可能影响月底验收",
			Impact:     "月底验收可能延期",
			Level:      "high",
			Status:     "open",
			SourceText: "供应商接口如果继续延期，月底验收会有风险。",
		}},
		OpenQuestions: []OpenQuestionExtraction{{
			Question:   "是否需要为供应商接口预留灰度开关",
			Owner:      "赵强",
			Status:     "open",
			SourceText: "灰度开关这件事还需要赵强确认。",
		}},
		FrameworkSections: []FrameworkSectionExtraction{{
			SectionKey: "background",
			Content:    "供应链系统进入联调阶段，需要统一供应商接口对接节奏。",
			SourceText: "供应链系统现在进入联调阶段。",
		}, {
			SectionKey: "meeting_goal",
			Content:    "确认联调节奏、验收口径和接口延期风险处理方式。",
			SourceText: "今天主要确认联调节奏和验收口径。",
		}},
	}, 3)
	if err != nil {
		t.Fatal(err)
	}

	if snapshot.Title != "供应链系统评审会" {
		t.Fatalf("expected title to be stored, got %q", snapshot.Title)
	}
	if snapshot.MeetingType != "project_followup" {
		t.Fatalf("expected meeting type project_followup, got %q", snapshot.MeetingType)
	}
	if snapshot.Summary == nil || !strings.Contains(snapshot.Summary.Content, "验收口径") {
		t.Fatalf("expected summary to be stored, got %#v", snapshot.Summary)
	}
	if len(snapshot.Decisions) != 1 || len(snapshot.ActionItems) != 1 || len(snapshot.Risks) != 1 || len(snapshot.OpenQuestions) != 1 {
		t.Fatalf("unexpected formal intelligence counts: decisions=%d actionItems=%d risks=%d openQuestions=%d",
			len(snapshot.Decisions), len(snapshot.ActionItems), len(snapshot.Risks), len(snapshot.OpenQuestions))
	}
	if meetingFrameworkFeatureEnabled() {
		if len(snapshot.FrameworkSections) != 5 {
			t.Fatalf("expected project follow-up framework sections to include 5 template modules, got %#v", snapshot.FrameworkSections)
		}
		if snapshot.FrameworkSections[0].SectionKey != "background" || !strings.Contains(snapshot.FrameworkSections[0].Content, "联调阶段") {
			t.Fatalf("expected project background framework content, got %#v", snapshot.FrameworkSections[0])
		}
	} else if len(snapshot.FrameworkSections) != 0 {
		t.Fatalf("expected framework sections to be hidden while feature is disabled, got %#v", snapshot.FrameworkSections)
	}
	if !hasString(snapshot.ActionItems[0].MissingFields, "due_date") {
		t.Fatalf("expected action item missing fields to include due_date, got %#v", snapshot.ActionItems[0].MissingFields)
	}
	if !hasString(snapshot.Risks[0].MissingFields, "owner") || !hasString(snapshot.Risks[0].MissingFields, "mitigation") || !hasString(snapshot.Risks[0].MissingFields, "due_date") {
		t.Fatalf("expected risk missing fields to include owner, mitigation and due_date, got %#v", snapshot.Risks[0].MissingFields)
	}
	if !hasString(snapshot.OpenQuestions[0].MissingFields, "due_date") {
		t.Fatalf("expected open question missing fields to include due_date, got %#v", snapshot.OpenQuestions[0].MissingFields)
	}
	if !hasMissingAlert(snapshot.MissingFieldAlerts, "risk", "mitigation") || !hasMissingAlert(snapshot.MissingFieldAlerts, "open_question", "due_date") {
		t.Fatalf("expected risk and open question missing alerts, got %#v", snapshot.MissingFieldAlerts)
	}
	if !hasMissingAlertMessage(snapshot.MissingFieldAlerts, "risk", "mitigation", "未闭环") {
		t.Fatalf("expected open risk without mitigation to be marked as unclosed, got %#v", snapshot.MissingFieldAlerts)
	}
}

func TestManualMeetingTitleLocksAgainstAutomaticTitle(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	store := &Store{db: db}
	if err := store.Init(ctx); err != nil {
		t.Fatal(err)
	}
	meeting, _, err := store.CreateMeeting(ctx, "调试会议", "general", "test")
	if err != nil {
		t.Fatal(err)
	}

	snapshot, err := store.SaveIntelligenceExtraction(ctx, meeting.ID, IntelligenceExtraction{
		Title:       "展会演示准备会",
		MeetingType: "project_followup",
		Summary:     SummaryExtraction{Brief: "会议讨论展会演示准备工作。"},
	}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Title != "展会演示准备会" {
		t.Fatalf("expected automatic title to be stored, got %q", snapshot.Title)
	}

	manual, err := store.UpdateMeetingTitle(ctx, meeting.ID, "客户试点复盘会", true)
	if err != nil {
		t.Fatal(err)
	}
	if manual.Title != "客户试点复盘会" || !manual.TitleLocked {
		t.Fatalf("expected manual title to be locked, got %#v", manual)
	}

	snapshot, err = store.SaveIntelligenceExtraction(ctx, meeting.ID, IntelligenceExtraction{
		Title:       "模型能力讨论会",
		MeetingType: "exploration",
		Summary:     SummaryExtraction{Brief: "会议继续讨论模型能力。"},
	}, 2)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Title != "客户试点复盘会" {
		t.Fatalf("expected locked title to resist automatic overwrite, got %q", snapshot.Title)
	}
}

func TestManualMeetingTypeLocksAgainstAutomaticType(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	store := &Store{db: db}
	if err := store.Init(ctx); err != nil {
		t.Fatal(err)
	}
	meeting, _, err := store.CreateMeeting(ctx, "类型选择测试", "general", "test")
	if err != nil {
		t.Fatal(err)
	}

	manual, err := store.UpdateMeetingType(ctx, meeting.ID, "project_followup", true)
	if err != nil {
		t.Fatal(err)
	}
	if manual.MeetingType != "project_followup" || !manual.TypeLocked {
		t.Fatalf("expected manual meeting type to be locked, got %#v", manual)
	}
	snapshot, err := store.GetIntelligenceSnapshot(ctx, meeting.ID)
	if err != nil {
		t.Fatal(err)
	}
	if meetingFrameworkFeatureEnabled() {
		if len(snapshot.FrameworkSections) != 5 || snapshot.FrameworkSections[0].Title != "当前项目/事项背景" {
			t.Fatalf("expected project framework templates after manual type change, got %#v", snapshot.FrameworkSections)
		}
	} else if len(snapshot.FrameworkSections) != 0 {
		t.Fatalf("expected framework templates to be hidden while feature is disabled, got %#v", snapshot.FrameworkSections)
	}

	snapshot, err = store.SaveIntelligenceExtraction(ctx, meeting.ID, IntelligenceExtraction{
		MeetingType: "exploration",
		Summary:     SummaryExtraction{Brief: "模型判断为探索讨论会。"},
	}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.MeetingType != "project_followup" {
		t.Fatalf("expected locked meeting type to resist automatic overwrite, got %q", snapshot.MeetingType)
	}
}

func TestMeetingTypeFrameworkTemplates(t *testing.T) {
	if !meetingFrameworkFeatureEnabled() {
		t.Skip("meeting framework feature is temporarily disabled")
	}
	ctx := context.Background()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)

	store := &Store{db: db}
	if err := store.Init(ctx); err != nil {
		t.Fatal(err)
	}
	meeting, _, err := store.CreateMeeting(ctx, "框架模板测试", "general", "test")
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		meetingType string
		titles      []string
	}{
		{"exploration", []string{"讨论背景", "核心问题", "已提出想法", "支持理由", "反对意见/风险", "待验证假设", "下一步探索动作"}},
		{"retrospective", []string{"目标与实际结果", "做得好的地方", "暴露的问题", "可能原因", "改进方案", "后续行动", "可沉淀经验"}},
		{"general", []string{"时间线摘要", "关键观点", "未解决问题"}},
	}
	for _, tc := range cases {
		if _, err := store.UpdateMeetingType(ctx, meeting.ID, tc.meetingType, true); err != nil {
			t.Fatal(err)
		}
		snapshot, err := store.GetIntelligenceSnapshot(ctx, meeting.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got := frameworkSectionTitles(snapshot.FrameworkSections); strings.Join(got, "|") != strings.Join(tc.titles, "|") {
			t.Fatalf("unexpected %s framework titles: got %#v want %#v", tc.meetingType, got, tc.titles)
		}
	}
}

func TestSaveIntelligenceExtractionStoresAllMeetingTypeFrameworkSections(t *testing.T) {
	if !meetingFrameworkFeatureEnabled() {
		t.Skip("meeting framework feature is temporarily disabled")
	}
	ctx := context.Background()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)

	store := &Store{db: db}
	if err := store.Init(ctx); err != nil {
		t.Fatal(err)
	}
	meeting, _, err := store.CreateMeeting(ctx, "多类型框架测试", "general", "test")
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		meetingType string
		sectionKey  string
		title       string
		content     string
	}{
		{"exploration", "core_question", "核心问题", "是否应该把会中交互作为正式版主卖点。"},
		{"retrospective", "lessons_learned", "可沉淀经验", "演示前需要提前锁定网络、麦克风和模型配置。"},
		{"general", "unresolved_issues", "未解决问题", "分享二维码的公网访问策略还没有最终确定。"},
	}
	for _, tc := range cases {
		if _, err := store.UpdateMeetingType(ctx, meeting.ID, tc.meetingType, true); err != nil {
			t.Fatal(err)
		}
		snapshot, err := store.SaveIntelligenceExtraction(ctx, meeting.ID, IntelligenceExtraction{
			MeetingType: tc.meetingType,
			FrameworkSections: []FrameworkSectionExtraction{{
				SectionKey: tc.sectionKey,
				Title:      tc.title,
				Content:    tc.content,
				SourceText: tc.content,
			}},
		}, 1)
		if err != nil {
			t.Fatal(err)
		}
		section := findFrameworkSection(snapshot.FrameworkSections, tc.sectionKey)
		if section == nil || section.Title != tc.title || !strings.Contains(section.Content, tc.content) {
			t.Fatalf("expected %s framework section %s to be stored, got %#v", tc.meetingType, tc.sectionKey, section)
		}
	}
}

func TestManualFrameworkSectionLocksAgainstAutomaticExtraction(t *testing.T) {
	if !meetingFrameworkFeatureEnabled() {
		t.Skip("meeting framework feature is temporarily disabled")
	}
	ctx := context.Background()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)

	store := &Store{db: db}
	if err := store.Init(ctx); err != nil {
		t.Fatal(err)
	}
	meeting, _, err := store.CreateMeeting(ctx, "框架锁定测试", "project_followup", "test")
	if err != nil {
		t.Fatal(err)
	}

	manual, err := store.ApplyFrameworkSectionMutation(ctx, meeting.ID, "project_followup", "current_progress", FrameworkSectionMutation{
		Mode:       "replace",
		Content:    "人工确认：实时转写已稳定，结构化提取还在优化。",
		SourceText: "会议助手，把当前进展改成实时转写已稳定，结构化提取还在优化。",
		Locked:     true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !manual.Locked || manual.Source != "interaction" {
		t.Fatalf("expected manual framework section to be locked, got %#v", manual)
	}

	snapshot, err := store.SaveIntelligenceExtraction(ctx, meeting.ID, IntelligenceExtraction{
		MeetingType: "project_followup",
		FrameworkSections: []FrameworkSectionExtraction{{
			SectionKey: "current_progress",
			Content:    "自动提取：当前仍在排查转写链路。",
			SourceText: "当前仍在排查转写链路。",
		}},
	}, 2)
	if err != nil {
		t.Fatal(err)
	}

	progress := findFrameworkSection(snapshot.FrameworkSections, "current_progress")
	if progress == nil || !strings.Contains(progress.Content, "人工确认") {
		t.Fatalf("expected manual framework section to resist automatic overwrite, got %#v", progress)
	}
}

func TestSaveIntelligenceExtractionStoresVisibleDynamicSections(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)

	store := &Store{db: db}
	if err := store.Init(ctx); err != nil {
		t.Fatal(err)
	}
	meeting, _, err := store.CreateMeeting(ctx, "dynamic-section", "test", "test")
	if err != nil {
		t.Fatal(err)
	}

	snapshot, err := store.SaveIntelligenceExtraction(ctx, meeting.ID, IntelligenceExtraction{
		Summary: SummaryExtraction{Brief: "Discussed launch budget and customer acceptance concerns."},
		DynamicSections: []DynamicSectionExtraction{{
			SectionKey:  "budget_constraints",
			Title:       "Budget Constraints",
			Description: "Budget limits mentioned during the meeting.",
			Items: []DynamicSectionItemExtraction{{
				Title:      "Demo budget ceiling",
				Content:    "The demo budget should stay below 80,000 yuan unless management approves an exception.",
				SourceText: "Keep the demo budget under 80,000; anything above that needs management approval.",
			}},
		}},
	}, 1)
	if err != nil {
		t.Fatal(err)
	}

	if len(snapshot.DynamicSections) != 1 {
		t.Fatalf("expected one dynamic section, got %#v", snapshot.DynamicSections)
	}
	section := snapshot.DynamicSections[0]
	if section.SectionKey != "budget_constraints" || section.Visibility != "visible" {
		t.Fatalf("expected visible budget section, got %#v", section)
	}
	if len(section.Items) != 1 || !strings.Contains(section.Items[0].Content, "80,000") {
		t.Fatalf("expected dynamic section item to be stored, got %#v", section.Items)
	}

	snapshot, err = store.SaveIntelligenceExtraction(ctx, meeting.ID, IntelligenceExtraction{
		DynamicSections: []DynamicSectionExtraction{{
			SectionKey:  "budget_constraints",
			Title:       "Budget Constraints",
			Description: "Budget limits and approval requirements for the onsite demo rollout.",
			Items: []DynamicSectionItemExtraction{{
				Title:      "Demo budget ceiling",
				Content:    "The demo budget should stay below 80,000 yuan unless management approves an exception.",
				SourceText: "The same budget ceiling was repeated.",
			}},
		}},
	}, 2)
	if err != nil {
		t.Fatal(err)
	}

	if len(snapshot.DynamicSections) != 1 || len(snapshot.DynamicSections[0].Items) != 1 {
		t.Fatalf("expected duplicate dynamic section to merge, got %#v", snapshot.DynamicSections)
	}
	if !strings.Contains(snapshot.DynamicSections[0].Description, "approval") {
		t.Fatalf("expected richer section description, got %#v", snapshot.DynamicSections[0])
	}

	visible, err := store.SetDynamicSectionVisibility(ctx, meeting.ID, section.ID, "visible")
	if err != nil {
		t.Fatal(err)
	}
	if visible.Visibility != "visible" {
		t.Fatalf("expected dynamic section to become visible, got %#v", visible)
	}
	snapshot, err = store.GetIntelligenceSnapshot(ctx, meeting.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.DynamicSections) != 1 || snapshot.DynamicSections[0].Visibility != "visible" {
		t.Fatalf("expected visible dynamic section in snapshot, got %#v", snapshot.DynamicSections)
	}
	affected, err := store.SetAllDynamicSectionVisibility(ctx, meeting.ID, "hidden")
	if err != nil {
		t.Fatal(err)
	}
	if affected != 1 {
		t.Fatalf("expected one dynamic section to be updated, got %d", affected)
	}
	snapshot, err = store.GetIntelligenceSnapshot(ctx, meeting.ID)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.DynamicSections[0].Visibility != "hidden" {
		t.Fatalf("expected dynamic section to become hidden again, got %#v", snapshot.DynamicSections[0])
	}
}

func TestAddTranscriptMergesNearbySimilarFinalResults(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	store := &Store{db: db}
	if err := store.Init(ctx); err != nil {
		t.Fatal(err)
	}
	meeting, _, err := store.CreateMeeting(ctx, "transcript-dedupe", "test", "test")
	if err != nil {
		t.Fatal(err)
	}

	startedAt := time.Date(2026, 7, 6, 9, 30, 1, 0, time.UTC)
	firstEndedAt := startedAt.Add(3 * time.Second)
	secondEndedAt := startedAt.Add(7 * time.Second)
	first, err := store.AddTranscript(ctx, stt.TranscriptResult{
		MeetingID:    meeting.ID,
		SpeakerLabel: "unknown",
		Text:         "继续排查Volcano ASR连接保持逻辑，重点关注是否应该在暂停期间保持连接以及是否需要等待",
		IsFinal:      true,
		Provider:     "volcano",
		StartedAt:    startedAt,
		EndedAt:      firstEndedAt,
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.AddTranscript(ctx, stt.TranscriptResult{
		MeetingID:    meeting.ID,
		SpeakerLabel: "unknown",
		Text:         "排查Volcano ASR连接保持逻辑，重点关注是否应该在暂停期间保持连接以及是否需要等待会议结束时的服务器正常关闭，并在本周五前给出结论。",
		IsFinal:      true,
		Provider:     "volcano",
		StartedAt:    startedAt.Add(500 * time.Millisecond),
		EndedAt:      secondEndedAt,
	})
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID {
		t.Fatalf("expected similar ASR revisions to update the same utterance, got %s and %s", first.ID, second.ID)
	}

	utterances, err := store.ListUtterances(ctx, meeting.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(utterances) != 1 {
		t.Fatalf("expected 1 merged utterance, got %d: %#v", len(utterances), utterances)
	}
	if !strings.Contains(utterances[0].Text, "服务器正常关闭") {
		t.Fatalf("expected richer transcript to be retained, got %q", utterances[0].Text)
	}
	if utterances[0].StartedAt != startedAt.Format(time.RFC3339Nano) {
		t.Fatalf("StartedAt = %q", utterances[0].StartedAt)
	}
	if utterances[0].EndedAt != secondEndedAt.Format(time.RFC3339Nano) {
		t.Fatalf("EndedAt = %q", utterances[0].EndedAt)
	}
}

func hasString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func findActionItemByTaskPart(items []MeetingActionItem, taskPart string) *MeetingActionItem {
	for index := range items {
		if strings.Contains(items[index].Task, taskPart) {
			return &items[index]
		}
	}
	return nil
}

func findFrameworkSection(sections []MeetingFrameworkSection, sectionKey string) *MeetingFrameworkSection {
	for index := range sections {
		if sections[index].SectionKey == sectionKey {
			return &sections[index]
		}
	}
	return nil
}

func frameworkSectionTitles(sections []MeetingFrameworkSection) []string {
	titles := make([]string, 0, len(sections))
	for _, section := range sections {
		titles = append(titles, section.Title)
	}
	return titles
}

func hasMissingAlert(alerts []MissingFieldAlert, targetType, field string) bool {
	for _, alert := range alerts {
		if alert.TargetType == targetType && alert.Field == field {
			return true
		}
	}
	return false
}

func hasMissingAlertMessage(alerts []MissingFieldAlert, targetType, field, want string) bool {
	for _, alert := range alerts {
		if alert.TargetType == targetType && alert.Field == field && strings.Contains(alert.Message, want) {
			return true
		}
	}
	return false
}
