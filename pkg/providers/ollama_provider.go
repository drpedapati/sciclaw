package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// OllamaProvider uses Ollama's native /api/chat endpoint so local runtime
// options (for example think=false) can be applied reliably.
type OllamaProvider struct {
	apiBase    string
	httpClient *http.Client
}

func NewOllamaProvider(apiBase string, timeout time.Duration) *OllamaProvider {
	base := strings.TrimRight(strings.TrimSpace(apiBase), "/")
	if base == "" {
		base = "http://localhost:11434"
	}
	return &OllamaProvider{
		apiBase:    base,
		httpClient: &http.Client{Timeout: timeout},
	}
}

func (p *OllamaProvider) Chat(ctx context.Context, messages []Message, tools []ToolDefinition, model string, options map[string]interface{}) (*LLMResponse, error) {
	if strings.TrimSpace(model) == "" {
		return nil, fmt.Errorf("model is required")
	}

	ollamaMessages := make([]map[string]interface{}, 0, len(messages))
	for _, m := range messages {
		msg := map[string]interface{}{
			"role":    m.Role,
			"content": m.Content,
		}
		if m.Role == "tool" {
			toolName := strings.TrimSpace(m.ToolName)
			if toolName == "" {
				toolName = strings.TrimSpace(m.ToolCallID)
			}
			if toolName != "" {
				msg["tool_name"] = toolName
			}
		}
		if len(m.ToolCalls) > 0 {
			msg["tool_calls"] = convertToolCallsToOllama(m.ToolCalls)
		}
		ollamaMessages = append(ollamaMessages, msg)
	}

	requestBody := map[string]interface{}{
		"model":    model,
		"messages": ollamaMessages,
		"stream":   false,
	}

	ollamaOptions := map[string]interface{}{}
	if maxTokens, ok := options["max_tokens"].(int); ok && maxTokens > 0 {
		ollamaOptions["num_predict"] = maxTokens
	}
	if temperature, ok := options["temperature"].(float64); ok {
		ollamaOptions["temperature"] = temperature
	}
	// Keep local context size reasonable by default to avoid pathological
	// latency on commodity GPUs/CPUs.
	ollamaOptions["num_ctx"] = 8192
	requestBody["options"] = ollamaOptions

	if len(tools) > 0 {
		requestBody["tools"] = tools
	}

	if shouldDisableThinking(model) {
		requestBody["think"] = false
	}

	jsonData, err := json.Marshal(requestBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal ollama request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", p.apiBase+"/api/chat", bytes.NewReader(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create ollama request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send ollama request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read ollama response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ollama API request failed:\n  Status: %d\n  Body:   %s", resp.StatusCode, string(body))
	}

	return parseOllamaChatResponse(body)
}

func (p *OllamaProvider) GetDefaultModel() string { return "" }

func shouldDisableThinking(model string) bool {
	lower := strings.ToLower(strings.TrimSpace(model))
	return strings.Contains(lower, "qwen")
}

func convertToolCallsToOllama(calls []ToolCall) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(calls))
	for _, tc := range calls {
		name := strings.TrimSpace(tc.Name)
		if name == "" && tc.Function != nil {
			name = strings.TrimSpace(tc.Function.Name)
		}
		if name == "" {
			continue
		}
		arguments := tc.Arguments
		if arguments == nil && tc.Function != nil && strings.TrimSpace(tc.Function.Arguments) != "" {
			tmp := map[string]interface{}{}
			if err := json.Unmarshal([]byte(tc.Function.Arguments), &tmp); err == nil {
				arguments = tmp
			}
		}
		if arguments == nil {
			arguments = map[string]interface{}{}
		}
		out = append(out, map[string]interface{}{
			"function": map[string]interface{}{
				"name":      name,
				"arguments": arguments,
			},
		})
	}
	return out
}

func parseOllamaChatResponse(body []byte) (*LLMResponse, error) {
	var raw struct {
		Message struct {
			Content   string `json:"content"`
			Thinking  string `json:"thinking"`
			Reasoning string `json:"reasoning"`
			ToolCalls []struct {
				Function struct {
					Name      string          `json:"name"`
					Arguments json.RawMessage `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"message"`
		DoneReason      string `json:"done_reason"`
		PromptEvalCount int    `json:"prompt_eval_count"`
		EvalCount       int    `json:"eval_count"`
	}

	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("failed to unmarshal ollama response: %w", err)
	}

	toolCalls := make([]ToolCall, 0, len(raw.Message.ToolCalls))
	for i, tc := range raw.Message.ToolCalls {
		name := strings.TrimSpace(tc.Function.Name)
		if name == "" {
			continue
		}
		args := map[string]interface{}{}
		if len(tc.Function.Arguments) > 0 {
			if err := json.Unmarshal(tc.Function.Arguments, &args); err != nil {
				args["raw"] = string(tc.Function.Arguments)
			}
		}
		toolCalls = append(toolCalls, ToolCall{
			ID:        fmt.Sprintf("ollama-tool-%d", i+1),
			Name:      name,
			Arguments: args,
		})
	}

	content := raw.Message.Content
	if strings.TrimSpace(content) == "" {
		if strings.TrimSpace(raw.Message.Reasoning) != "" {
			content = raw.Message.Reasoning
		} else if strings.TrimSpace(raw.Message.Thinking) != "" {
			content = raw.Message.Thinking
		}
	}

	return &LLMResponse{
		Content:      content,
		ToolCalls:    toolCalls,
		FinishReason: raw.DoneReason,
		Usage: &UsageInfo{
			PromptTokens:     raw.PromptEvalCount,
			CompletionTokens: raw.EvalCount,
			TotalTokens:      raw.PromptEvalCount + raw.EvalCount,
		},
	}, nil
}
