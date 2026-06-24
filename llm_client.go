package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

type LLMMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type LLMClient interface {
	Complete(ctx context.Context, cfg *LLMConfig, messages []LLMMessage, temperature float64) (string, error)
}

type OpenAICompatibleLLMClient struct{}

func (OpenAICompatibleLLMClient) Complete(ctx context.Context, cfg *LLMConfig, messages []LLMMessage, temperature float64) (string, error) {
	endpoint, err := chatCompletionsEndpoint(cfg.BaseURL)
	if err != nil {
		return "", err
	}
	reqBody := map[string]any{
		"model":       cfg.Model,
		"messages":    messages,
		"temperature": temperature,
	}
	if mode := normalizeLLMThinkingMode(cfg.ThinkingMode); mode != LLMThinkingDefault {
		reqBody["thinking"] = map[string]any{
			"type": mode,
		}
	}
	reqBytes, err := json.Marshal(reqBody)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(reqBytes))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if strings.TrimSpace(cfg.apiKey) != "" {
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(cfg.apiKey))
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var chatResp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&chatResp); err != nil {
		return "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if chatResp.Error.Message != "" {
			return "", fmt.Errorf("llm returned %s: %s", resp.Status, chatResp.Error.Message)
		}
		return "", fmt.Errorf("llm returned %s", resp.Status)
	}
	if len(chatResp.Choices) == 0 || strings.TrimSpace(chatResp.Choices[0].Message.Content) == "" {
		return "", fmt.Errorf("llm response has no content")
	}
	return chatResp.Choices[0].Message.Content, nil
}
