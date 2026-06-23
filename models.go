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
	Addr        string
	DB          string
	STTProvider string
}

type LLMUseCase string

const (
	LLMUseCaseDefault      LLMUseCase = "default"
	LLMUseCaseIntelligence LLMUseCase = "intelligence"
	LLMUseCaseInteraction  LLMUseCase = "interaction"
)

type Meeting struct {
	ID              string `json:"id"`
	Title           string `json:"title"`
	Type            string `json:"type"`
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
	BaseURL   string `json:"baseUrl"`
	Model     string `json:"model"`
	HasAPIKey bool   `json:"hasApiKey"`
	UpdatedAt string `json:"updatedAt,omitempty"`

	apiKey string
}

type MeetingSummary struct {
	ID                   string `json:"id"`
	MeetingID            string `json:"meetingId"`
	Content              string `json:"content"`
	SourceUtteranceCount int    `json:"sourceUtteranceCount"`
	CreatedAt            string `json:"createdAt"`
	UpdatedAt            string `json:"updatedAt"`
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
	ID         string `json:"id"`
	MeetingID  string `json:"meetingId"`
	Task       string `json:"task"`
	Owner      string `json:"owner,omitempty"`
	DueDate    string `json:"dueDate,omitempty"`
	Status     string `json:"status"`
	SourceText string `json:"sourceText,omitempty"`
	IsDeleted  bool   `json:"isDeleted"`
	CreatedAt  string `json:"createdAt"`
	UpdatedAt  string `json:"updatedAt"`
}

type IntelligenceSnapshot struct {
	Type        string              `json:"type"`
	MeetingID   string              `json:"meetingId"`
	Summary     *MeetingSummary     `json:"summary,omitempty"`
	Decisions   []MeetingDecision   `json:"decisions"`
	ActionItems []MeetingActionItem `json:"actionItems"`
	Timestamp   string              `json:"timestamp"`
}

type IntelligenceExtraction struct {
	Summary     string                 `json:"summary"`
	Decisions   []DecisionExtraction   `json:"decisions"`
	ActionItems []ActionItemExtraction `json:"action_items"`
}

type DecisionExtraction struct {
	Content    string `json:"content"`
	SourceText string `json:"source_text"`
}

type ActionItemExtraction struct {
	Task       string `json:"task"`
	Owner      string `json:"owner"`
	DueDate    string `json:"due_date"`
	Status     string `json:"status"`
	SourceText string `json:"source_text"`
}

type InteractionCommand struct {
	Intent      string  `json:"intent"`
	TargetType  string  `json:"target_type"`
	TargetIndex int     `json:"target_index"`
	TargetID    string  `json:"target_id"`
	Confidence  float64 `json:"confidence"`
	NeedConfirm bool    `json:"need_confirm"`
	Reply       string  `json:"reply"`
	Reason      string  `json:"reason"`
}

type InteractionEvent struct {
	ID              string `json:"id"`
	MeetingID       string `json:"meetingId"`
	RawTranscript   string `json:"rawTranscript"`
	WakeWord        string `json:"wakeWord"`
	Instruction     string `json:"instruction"`
	Intent          string `json:"intent"`
	TargetType      string `json:"targetType"`
	TargetIndex     int    `json:"targetIndex"`
	TargetID        string `json:"targetId,omitempty"`
	CommandJSON     string `json:"commandJson"`
	ExecutionStatus string `json:"executionStatus"`
	Message         string `json:"message"`
	ErrorMessage    string `json:"errorMessage,omitempty"`
	CreatedAt       string `json:"createdAt"`
}

type InteractionUpdate struct {
	Type      string                `json:"type"`
	MeetingID string                `json:"meetingId"`
	Event     *InteractionEvent     `json:"event"`
	Snapshot  *IntelligenceSnapshot `json:"snapshot,omitempty"`
	Timestamp string                `json:"timestamp"`
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
