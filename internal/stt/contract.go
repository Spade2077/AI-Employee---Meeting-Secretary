// 本文件固定会议核心服务与 ASR 适配层之间的协议常量。
// 后续火山、SenseVoice、Zipformer 或双路 ASR 都必须归一化成这些语义，
// 这样会议状态机、会中交互、结构化纪要和前端展示不需要感知底层模型变化。
package stt

const (
	// ContractVersion 是 Go 主服务消费 ASR 结果的稳定协议版本。
	ContractVersion = "meeting-asr-v1"

	// AudioFormatPCM16KMono 是当前浏览器麦克风上行到后端的标准音频格式。
	// 本地 ASR 服务第一版也按这个格式接收，避免在 Go 主服务里做额外重采样。
	AudioFormatPCM16KMono = "pcm_s16le_16000_mono"

	ProviderVolcano    = "volcano"
	ProviderLocal      = "local"
	ProviderSenseVoice = "sensevoice"
	ProviderZipformer  = "zipformer"

	DefaultSpeakerLabel = "speaker_1"
)

// LocalSessionStart 是后续 Go local adapter 发给本地 ASR 服务的会话启动语义。
// 当前第一阶段只冻结协议，不强制现有 volcano adapter 使用该结构。
type LocalSessionStart struct {
	Type                       string `json:"type"`
	ContractVersion            string `json:"contractVersion"`
	MeetingID                  string `json:"meetingId"`
	AudioFormat                string `json:"audioFormat"`
	SampleRate                 int    `json:"sampleRate"`
	Channels                   int    `json:"channels"`
	Language                   string `json:"language,omitempty"`
	EnableSpeakerDiarization   bool   `json:"enableSpeakerDiarization"`
	EnablePunctuation          bool   `json:"enablePunctuation"`
	EnableInverseTextNormalize bool   `json:"enableInverseTextNormalize"`
}

// LocalTranscriptEvent 是本地 ASR 服务返回给 Go local adapter 的标准事件。
// 时间用毫秒偏移，adapter 再转换成 TranscriptResult 的 StartedAt/EndedAt。
type LocalTranscriptEvent struct {
	Type            string  `json:"type"`
	ContractVersion string  `json:"contractVersion"`
	MeetingID       string  `json:"meetingId"`
	SpeakerLabel    string  `json:"speakerLabel"`
	Text            string  `json:"text"`
	IsFinal         bool    `json:"isFinal"`
	Confidence      float64 `json:"confidence"`
	Provider        string  `json:"provider"`
	StartedAtMs     int64   `json:"startedAtMs"`
	EndedAtMs       int64   `json:"endedAtMs"`
}
