// 本文件负责会议、转写、LLM 配置和交互事件的核心数据结构。
package main

type MeetingStatus string

const (
	StatusStandby    MeetingStatus = "standby"
	StatusRecording  MeetingStatus = "recording"
	StatusSoftPaused MeetingStatus = "soft_paused"
	StatusEnding     MeetingStatus = "ending"
	StatusEnded      MeetingStatus = "ended"
)

func CanUploadAudio(status MeetingStatus) bool {
	return status == StatusRecording
}

type Config struct {
	Addr          string
	DB            string
	STTProvider   string
	PublicBaseURL string
}

type LLMUseCase string

const (
	LLMUseCaseDefault      LLMUseCase = "default"
	LLMUseCaseIntelligence LLMUseCase = "intelligence"
	LLMUseCaseInteraction  LLMUseCase = "interaction"
)

const (
	LLMThinkingDefault  = "default"
	LLMThinkingEnabled  = "enabled"
	LLMThinkingDisabled = "disabled"
)

type Meeting struct {
	ID              string `json:"id"`
	Title           string `json:"title"`
	Type            string `json:"type"`
	MeetingType     string `json:"meetingType"`
	TitleLocked     bool   `json:"titleLocked"`
	TypeLocked      bool   `json:"typeLocked"`
	Status          string `json:"status"`
	PreviousStatus  string `json:"previousStatus,omitempty"`
	StartedAt       string `json:"startedAt,omitempty"`
	EndedAt         string `json:"endedAt,omitempty"`
	DurationSeconds int64  `json:"durationSeconds"`
	CreatedAt       string `json:"createdAt"`
	UpdatedAt       string `json:"updatedAt"`
}

type StateEvent struct {
	ID         string `json:"id"`
	MeetingID  string `json:"meetingId"`
	FromStatus string `json:"fromStatus"`
	ToStatus   string `json:"toStatus"`
	EventType  string `json:"eventType"`
	Operator   string `json:"operator"`
	Note       string `json:"note,omitempty"`
	CreatedAt  string `json:"createdAt"`
}

type Utterance struct {
	ID           string  `json:"id"`
	MeetingID    string  `json:"meetingId"`
	SpeakerLabel string  `json:"speakerLabel"`
	Text         string  `json:"text"`
	IsFinal      bool    `json:"isFinal"`
	Provider     string  `json:"provider"`
	Confidence   float64 `json:"confidence"`
	IsDeleted    bool    `json:"isDeleted"`
	CreatedAt    string  `json:"createdAt"`
	DeletedAt    string  `json:"deletedAt,omitempty"`
}

type LLMConfig struct {
	UseCase      LLMUseCase `json:"useCase"`
	BaseURL      string     `json:"baseUrl"`
	Model        string     `json:"model"`
	ThinkingMode string     `json:"thinkingMode"`
	HasAPIKey    bool       `json:"hasApiKey"`
	UpdatedAt    string     `json:"updatedAt,omitempty"`

	apiKey string
}

type MeetingSummary struct {
	ID                   string   `json:"id"`
	MeetingID            string   `json:"meetingId"`
	Content              string   `json:"content"`
	Brief                string   `json:"brief"`
	Background           string   `json:"background"`
	KeyPoints            []string `json:"keyPoints"`
	Progress             string   `json:"progress"`
	Changes              []string `json:"changes"`
	Conclusion           string   `json:"conclusion"`
	NextFollowUp         string   `json:"nextFollowUp"`
	SourceUtteranceCount int      `json:"sourceUtteranceCount"`
	CreatedAt            string   `json:"createdAt"`
	UpdatedAt            string   `json:"updatedAt"`
}

type MeetingDecision struct {
	ID         string `json:"id"`
	MeetingID  string `json:"meetingId"`
	Content    string `json:"content"`
	SourceText string `json:"sourceText,omitempty"`
	IsDeleted  bool   `json:"isDeleted"`
	CreatedAt  string `json:"createdAt"`
	UpdatedAt  string `json:"updatedAt"`
}

type MeetingActionItem struct {
	ID            string   `json:"id"`
	MeetingID     string   `json:"meetingId"`
	Task          string   `json:"task"`
	Owner         string   `json:"owner,omitempty"`
	DueDate       string   `json:"dueDate,omitempty"`
	Status        string   `json:"status"`
	MissingFields []string `json:"missingFields,omitempty"`
	SourceText    string   `json:"sourceText,omitempty"`
	IsDeleted     bool     `json:"isDeleted"`
	CreatedAt     string   `json:"createdAt"`
	UpdatedAt     string   `json:"updatedAt"`
}

type ActionItemMutation struct {
	Task       string
	Owner      string
	DueDate    string
	Status     string
	SourceText string
}

type MeetingRisk struct {
	ID            string   `json:"id"`
	MeetingID     string   `json:"meetingId"`
	Content       string   `json:"content"`
	Impact        string   `json:"impact,omitempty"`
	Level         string   `json:"level"`
	Owner         string   `json:"owner,omitempty"`
	Mitigation    string   `json:"mitigation,omitempty"`
	DueDate       string   `json:"dueDate,omitempty"`
	Status        string   `json:"status"`
	MissingFields []string `json:"missingFields,omitempty"`
	SourceText    string   `json:"sourceText,omitempty"`
	IsDeleted     bool     `json:"isDeleted"`
	CreatedAt     string   `json:"createdAt"`
	UpdatedAt     string   `json:"updatedAt"`
}

type RiskMutation struct {
	Content    string
	Impact     string
	Level      string
	Owner      string
	Mitigation string
	DueDate    string
	Status     string
	SourceText string
}

type MeetingOpenQuestion struct {
	ID            string   `json:"id"`
	MeetingID     string   `json:"meetingId"`
	Question      string   `json:"question"`
	Owner         string   `json:"owner,omitempty"`
	DueDate       string   `json:"dueDate,omitempty"`
	Status        string   `json:"status"`
	MissingFields []string `json:"missingFields,omitempty"`
	SourceText    string   `json:"sourceText,omitempty"`
	IsDeleted     bool     `json:"isDeleted"`
	CreatedAt     string   `json:"createdAt"`
	UpdatedAt     string   `json:"updatedAt"`
}

type OpenQuestionMutation struct {
	Question   string
	Owner      string
	DueDate    string
	Status     string
	SourceText string
}

type MissingFieldAlert struct {
	TargetType  string `json:"targetType"`
	TargetID    string `json:"targetId"`
	TargetTitle string `json:"targetTitle"`
	Field       string `json:"field"`
	Message     string `json:"message"`
	Level       string `json:"level"`
}

type InteractionCommand struct {
	Intent        string   `json:"intent"`
	TargetType    string   `json:"target_type"`
	TargetIndex   int      `json:"target_index"`
	TargetID      string   `json:"target_id"`
	SourceIndices []int    `json:"source_indices,omitempty"`
	SourceIDs     []string `json:"source_ids,omitempty"`
	Task          string   `json:"task,omitempty"`
	Owner         string   `json:"owner,omitempty"`
	DueDate       string   `json:"due_date,omitempty"`
	Status        string   `json:"status,omitempty"`
	Impact        string   `json:"impact,omitempty"`
	Level         string   `json:"level,omitempty"`
	Mitigation    string   `json:"mitigation,omitempty"`
	Question      string   `json:"question,omitempty"`
	Title         string   `json:"title,omitempty"`
	SectionKey    string   `json:"section_key,omitempty"`
	SectionTitle  string   `json:"section_title,omitempty"`
	Content       string   `json:"content,omitempty"`
	Confidence    float64  `json:"confidence"`
	NeedConfirm   bool     `json:"need_confirm"`
	Reply         string   `json:"reply"`
	Reason        string   `json:"reason"`
	Instruction   string   `json:"-"`
}

type InteractionEvent struct {
	ID              string             `json:"id"`
	MeetingID       string             `json:"meetingId"`
	RawTranscript   string             `json:"rawTranscript"`
	WakeWord        string             `json:"wakeWord"`
	Instruction     string             `json:"instruction"`
	Intent          string             `json:"intent"`
	TargetType      string             `json:"targetType"`
	TargetIndex     int                `json:"targetIndex"`
	TargetID        string             `json:"targetId,omitempty"`
	CommandJSON     string             `json:"commandJson"`
	ExecutionStatus string             `json:"executionStatus"`
	Message         string             `json:"message"`
	ErrorMessage    string             `json:"errorMessage,omitempty"`
	CreatedAt       string             `json:"createdAt"`
	CompletedAt     string             `json:"completedAt,omitempty"`
	DurationMs      int64              `json:"durationMs"`
	Timing          *InteractionTiming `json:"timing,omitempty"`
}

type InteractionTiming struct {
	StartedAt   string                   `json:"startedAt,omitempty"`
	CompletedAt string                   `json:"completedAt,omitempty"`
	TotalMs     int64                    `json:"totalMs"`
	Stages      []InteractionTimingStage `json:"stages"`
}

type InteractionTimingStage struct {
	Key        string `json:"key"`
	Label      string `json:"label"`
	DurationMs int64  `json:"durationMs"`
	Detail     string `json:"detail,omitempty"`
}

type InteractionUndoRecord struct {
	ID                 string `json:"id"`
	MeetingID          string `json:"meetingId"`
	InteractionEventID string `json:"interactionEventId"`
	UndoType           string `json:"undoType"`
	UndoJSON           string `json:"undoJson"`
	IsUsed             bool   `json:"isUsed"`
	UsedByEventID      string `json:"usedByEventId,omitempty"`
	CreatedAt          string `json:"createdAt"`
	UsedAt             string `json:"usedAt,omitempty"`
}

type OperationJournalRecord struct {
	ID                 string `json:"id"`
	MeetingID          string `json:"meetingId"`
	InteractionEventID string `json:"interactionEventId"`
	StepIndex          int    `json:"stepIndex"`
	ToolName           string `json:"toolName"`
	ArgsJSON           string `json:"argsJson"`
	BeforeJSON         string `json:"beforeJson"`
	AfterJSON          string `json:"afterJson"`
	UndoJSON           string `json:"undoJson"`
	Status             string `json:"status"`
	ErrorMessage       string `json:"errorMessage,omitempty"`
	CreatedAt          string `json:"createdAt"`
}

type InteractionUpdate struct {
	Type      string                `json:"type"`
	MeetingID string                `json:"meetingId"`
	Event     *InteractionEvent     `json:"event"`
	Snapshot  *IntelligenceSnapshot `json:"snapshot,omitempty"`
	Timestamp string                `json:"timestamp"`
}

type InteractionStatusUpdate struct {
	Type      string `json:"type"`
	MeetingID string `json:"meetingId"`
	EventID   string `json:"eventId,omitempty"`
	Status    string `json:"status"`
	Message   string `json:"message"`
	Timestamp string `json:"timestamp"`
}

type TranscriptDelta struct {
	Type      string     `json:"type"`
	MeetingID string     `json:"meetingId"`
	Utterance *Utterance `json:"utterance"`
	Timestamp string     `json:"timestamp"`
}

type STTErrorEvent struct {
	Type      string `json:"type"`
	MeetingID string `json:"meetingId,omitempty"`
	Provider  string `json:"provider"`
	Message   string `json:"message"`
	Timestamp string `json:"timestamp"`
}

type StateUpdate struct {
	Type            string      `json:"type"`
	MeetingID       string      `json:"meetingId,omitempty"`
	Status          string      `json:"status"`
	PreviousStatus  string      `json:"previousStatus,omitempty"`
	DurationSeconds int64       `json:"durationSeconds"`
	Meeting         *Meeting    `json:"meeting,omitempty"`
	Event           *StateEvent `json:"event,omitempty"`
	Timestamp       string      `json:"timestamp"`
}
