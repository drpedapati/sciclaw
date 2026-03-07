package providers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestOllamaProviderChat_RequestShapeAndParse(t *testing.T) {
	var captured map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chat" {
			t.Fatalf("path=%q want /api/chat", r.URL.Path)
		}
		defer r.Body.Close()
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"message": {
				"content": "done",
				"tool_calls": [
					{
						"function": {
							"name": "exec",
							"arguments": {"cmd":"ls"}
						}
					}
				]
			},
			"done_reason": "stop",
			"prompt_eval_count": 11,
			"eval_count": 5
		}`))
	}))
	defer srv.Close()

	p := NewOllamaProvider(srv.URL, 10*time.Second)
	messages := []Message{
		{Role: "user", Content: "Run ls"},
		{Role: "tool", Content: "ok", ToolName: "exec"},
	}
	tools := []ToolDefinition{
		{
			Type: "function",
			Function: ToolFunctionDefinition{
				Name:        "exec",
				Description: "run shell commands",
				Parameters: map[string]interface{}{
					"type": "object",
				},
			},
		},
	}
	resp, err := p.Chat(context.Background(), messages, tools, "qwen3.5:4b", map[string]interface{}{
		"max_tokens":  123,
		"temperature": 0.2,
	})
	if err != nil {
		t.Fatalf("Chat returned error: %v", err)
	}
	if resp.Content != "done" {
		t.Fatalf("content=%q want done", resp.Content)
	}
	if resp.FinishReason != "stop" {
		t.Fatalf("finish_reason=%q want stop", resp.FinishReason)
	}
	if resp.Usage == nil || resp.Usage.PromptTokens != 11 || resp.Usage.CompletionTokens != 5 || resp.Usage.TotalTokens != 16 {
		t.Fatalf("usage=%+v", resp.Usage)
	}
	if len(resp.ToolCalls) != 1 || resp.ToolCalls[0].Name != "exec" {
		t.Fatalf("tool calls=%+v", resp.ToolCalls)
	}
	if resp.ToolCalls[0].Arguments["cmd"] != "ls" {
		t.Fatalf("tool args=%+v", resp.ToolCalls[0].Arguments)
	}

	if captured["think"] != false {
		t.Fatalf("think=%v want false", captured["think"])
	}
	options, ok := captured["options"].(map[string]interface{})
	if !ok {
		t.Fatalf("options missing or wrong type: %#v", captured["options"])
	}
	if got := int(options["num_predict"].(float64)); got != 123 {
		t.Fatalf("num_predict=%d want 123", got)
	}
	if got := int(options["num_ctx"].(float64)); got != 8192 {
		t.Fatalf("num_ctx=%d want 8192", got)
	}
	msgs, ok := captured["messages"].([]interface{})
	if !ok || len(msgs) != 2 {
		t.Fatalf("messages=%#v", captured["messages"])
	}
	second, ok := msgs[1].(map[string]interface{})
	if !ok {
		t.Fatalf("second message type=%T", msgs[1])
	}
	if second["tool_name"] != "exec" {
		t.Fatalf("tool_name=%v want exec", second["tool_name"])
	}
}

func TestParseOllamaChatResponse_FallsBackToReasoning(t *testing.T) {
	body := []byte(`{
		"message": {
			"content": "",
			"reasoning": "reasoning-only output"
		},
		"done_reason": "length",
		"prompt_eval_count": 3,
		"eval_count": 7
	}`)
	resp, err := parseOllamaChatResponse(body)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if resp.Content != "reasoning-only output" {
		t.Fatalf("content=%q", resp.Content)
	}
	if resp.Usage == nil || resp.Usage.TotalTokens != 10 {
		t.Fatalf("usage=%+v", resp.Usage)
	}
}
