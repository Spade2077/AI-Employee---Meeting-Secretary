package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOpenAICompatibleLLMClientThinkingMode(t *testing.T) {
	var bodies []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		bodies = append(bodies, body)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{
				"message": map[string]any{"content": "{}"},
			}},
		})
	}))
	defer server.Close()

	client := OpenAICompatibleLLMClient{}
	_, err := client.Complete(context.Background(), &LLMConfig{
		BaseURL:      server.URL + "/v1",
		Model:        "test-model",
		ThinkingMode: LLMThinkingDefault,
	}, []LLMMessage{{Role: "user", Content: "hi"}}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := bodies[0]["thinking"]; ok {
		t.Fatalf("default thinking mode should not be sent: %#v", bodies[0])
	}

	_, err = client.Complete(context.Background(), &LLMConfig{
		BaseURL:      server.URL + "/v1",
		Model:        "test-model",
		ThinkingMode: LLMThinkingDisabled,
	}, []LLMMessage{{Role: "user", Content: "hi"}}, 0)
	if err != nil {
		t.Fatal(err)
	}
	thinking, ok := bodies[1]["thinking"].(map[string]any)
	if !ok || thinking["type"] != LLMThinkingDisabled {
		t.Fatalf("disabled thinking mode should be sent: %#v", bodies[1])
	}
}
