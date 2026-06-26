// 本文件负责通用文本处理、ID 生成和辅助工具函数。
package main

import (
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
	"unicode"
)

func operatorFromRequest(r *http.Request) string {
	operator := r.URL.Query().Get("operator")
	if operator == "" {
		operator = r.Header.Get("X-Operator")
	}
	return defaultOperator(operator)
}

func defaultOperator(operator string) string {
	if strings.TrimSpace(operator) == "" {
		return "screen"
	}
	return operator
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]any{
		"error": message,
	})
}

func nowString() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}

func computeDurationSeconds(startedAt, endedAt string) int64 {
	if startedAt == "" || endedAt == "" {
		return 0
	}
	start, err := time.Parse(time.RFC3339Nano, startedAt)
	if err != nil {
		return 0
	}
	end, err := time.Parse(time.RFC3339Nano, endedAt)
	if err != nil {
		return 0
	}
	if end.Before(start) {
		return 0
	}
	return int64(end.Sub(start).Seconds())
}

func transcriptForIntelligence(utterances []Utterance) string {
	var builder strings.Builder
	for _, u := range utterances {
		if u.IsDeleted || !u.IsFinal || strings.TrimSpace(u.Text) == "" {
			continue
		}
		if builder.Len() > 0 {
			builder.WriteByte('\n')
		}
		speaker := strings.TrimSpace(u.SpeakerLabel)
		if speaker == "" {
			speaker = "speaker"
		}
		builder.WriteString(speaker)
		builder.WriteString(": ")
		builder.WriteString(strings.TrimSpace(u.Text))
	}
	return builder.String()
}

func chatCompletionsEndpoint(baseURL string) (string, error) {
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		return "", fmt.Errorf("llm base url is empty")
	}
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return "", err
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("llm base url must include scheme and host")
	}
	path := strings.TrimRight(parsed.Path, "/")
	if strings.HasSuffix(path, "/chat/completions") {
		parsed.Path = path
		return parsed.String(), nil
	}
	parsed.Path = path + "/chat/completions"
	return parsed.String(), nil
}

func envForLLMUseCase(useCase LLMUseCase, suffix string) string {
	if useCase != "" && useCase != LLMUseCaseDefault {
		key := "LLM_" + strings.ToUpper(string(useCase)) + "_" + suffix
		if value := os.Getenv(key); value != "" {
			return value
		}
	}
	return os.Getenv("LLM_" + suffix)
}

func normalizeLLMUseCase(value string) LLMUseCase {
	switch LLMUseCase(strings.ToLower(strings.TrimSpace(value))) {
	case LLMUseCaseIntelligence:
		return LLMUseCaseIntelligence
	case LLMUseCaseInteraction:
		return LLMUseCaseInteraction
	default:
		return LLMUseCaseDefault
	}
}

func normalizeLLMThinkingMode(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case LLMThinkingEnabled, "enable", "on", "true", "1":
		return LLMThinkingEnabled
	case LLMThinkingDisabled, "disable", "off", "false", "0":
		return LLMThinkingDisabled
	default:
		return LLMThinkingDefault
	}
}

func extractJSONObject(content string) string {
	content = strings.TrimSpace(content)
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(content, "```")
	content = strings.TrimSpace(content)
	start := strings.Index(content, "{")
	end := strings.LastIndex(content, "}")
	if start >= 0 && end > start {
		return content[start : end+1]
	}
	return content
}

func limitTail(text string, maxRunes int) string {
	runes := []rune(text)
	if len(runes) <= maxRunes {
		return text
	}
	return string(runes[len(runes)-maxRunes:])
}

func normalizeForCompare(value string) string {
	var builder strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(value)) {
		if unicode.IsSpace(r) || unicode.IsPunct(r) || unicode.IsSymbol(r) {
			continue
		}
		builder.WriteRune(r)
	}
	return builder.String()
}

func similarStructuredText(left, right string) bool {
	leftKey := normalizeForCompare(left)
	rightKey := normalizeForCompare(right)
	if leftKey == "" || rightKey == "" {
		return false
	}
	if leftKey == rightKey {
		return true
	}
	minLen := minInt(len([]rune(leftKey)), len([]rune(rightKey)))
	if minLen >= 8 && (strings.Contains(leftKey, rightKey) || strings.Contains(rightKey, leftKey)) {
		return true
	}
	score := structuredTextSimilarity(leftKey, rightKey)
	return minLen >= 12 && score >= 0.55
}

func structuredTextSimilarity(left, right string) float64 {
	return ngramJaccard(normalizeForCompare(left), normalizeForCompare(right), 2)
}

func ngramJaccard(left, right string, size int) float64 {
	leftGrams := ngramSet(left, size)
	rightGrams := ngramSet(right, size)
	if len(leftGrams) == 0 || len(rightGrams) == 0 {
		return 0
	}
	intersection := 0
	for gram := range leftGrams {
		if rightGrams[gram] {
			intersection++
		}
	}
	union := len(leftGrams) + len(rightGrams) - intersection
	if union == 0 {
		return 0
	}
	return float64(intersection) / float64(union)
}

func ngramSet(value string, size int) map[string]bool {
	runes := []rune(value)
	if len(runes) == 0 {
		return map[string]bool{}
	}
	if len(runes) <= size {
		return map[string]bool{string(runes): true}
	}
	grams := make(map[string]bool, len(runes)-size+1)
	for i := 0; i+size <= len(runes); i++ {
		grams[string(runes[i:i+size])] = true
	}
	return grams
}

func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}

func pcm16Peak(data []byte) int {
	peak := 0
	for i := 0; i+1 < len(data); i += 2 {
		sample := int(int16(binary.LittleEndian.Uint16(data[i : i+2])))
		if sample < 0 {
			sample = -sample
		}
		if sample > peak {
			peak = sample
		}
	}
	return peak
}

func newID(prefix string) string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%s_%d", prefix, time.Now().UnixNano())
	}
	return prefix + "_" + hex.EncodeToString(b[:])
}
