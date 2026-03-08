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
	// Ollama expects think=false as a top-level request field. Sending it under
	// options does not suppress hidden reasoning on current Qwen builds.
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
	toolCallSource := ""
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
	if len(toolCalls) > 0 {
		toolCallSource = "native"
	}

	content := raw.Message.Content
	contentSource := ""
	switch {
	case strings.TrimSpace(raw.Message.Content) != "":
		contentSource = "content"
	case strings.TrimSpace(raw.Message.Reasoning) != "":
		content = raw.Message.Reasoning
		contentSource = "reasoning"
	case strings.TrimSpace(raw.Message.Thinking) != "":
		content = raw.Message.Thinking
		contentSource = "thinking"
	}
	if len(toolCalls) == 0 {
		for _, candidate := range []struct {
			source string
			text   string
		}{
			{source: "content", text: raw.Message.Content},
			{source: "reasoning", text: raw.Message.Reasoning},
			{source: "thinking", text: raw.Message.Thinking},
		} {
			recovered := extractTextToolCalls(candidate.text)
			if len(recovered) == 0 {
				continue
			}
			toolCalls = recovered
			toolCallSource = candidate.source
			stripped := strings.TrimSpace(stripTextToolCalls(candidate.text))
			if contentSource == "" || contentSource == candidate.source {
				content = stripped
				contentSource = candidate.source
			}
			break
		}
	}

	finishReason := raw.DoneReason
	if len(toolCalls) > 0 && toolCallSource != "" && toolCallSource != "native" && (strings.TrimSpace(finishReason) == "" || finishReason == "stop") {
		finishReason = "tool_calls"
	}

	diagnostics := &ResponseDiagnostics{
		ContentSource:  contentSource,
		ToolCallSource: toolCallSource,
	}
	if diagnostics.ContentSource == "" && diagnostics.ToolCallSource == "" {
		diagnostics = nil
	}

	return &LLMResponse{
		Content:      content,
		ToolCalls:    toolCalls,
		FinishReason: finishReason,
		Usage: &UsageInfo{
			PromptTokens:     raw.PromptEvalCount,
			CompletionTokens: raw.EvalCount,
			TotalTokens:      raw.PromptEvalCount + raw.EvalCount,
		},
		Diagnostics: diagnostics,
	}, nil
}

func extractTextToolCalls(text string) []ToolCall {
	start, end, ok := toolCallsEnvelopeBounds(text)
	if !ok {
		return nil
	}

	var wrapper struct {
		ToolCalls []struct {
			ID       string `json:"id"`
			Type     string `json:"type"`
			Function struct {
				Name      string          `json:"name"`
				Arguments json.RawMessage `json:"arguments"`
			} `json:"function"`
		} `json:"tool_calls"`
	}
	if err := json.Unmarshal([]byte(text[start:end]), &wrapper); err != nil {
		return nil
	}

	result := make([]ToolCall, 0, len(wrapper.ToolCalls))
	for i, tc := range wrapper.ToolCalls {
		name := strings.TrimSpace(tc.Function.Name)
		if name == "" {
			continue
		}
		args, argsString := decodeTextToolArguments(tc.Function.Arguments)
		id := strings.TrimSpace(tc.ID)
		if id == "" {
			id = fmt.Sprintf("ollama-text-tool-%d", i+1)
		}
		result = append(result, ToolCall{
			ID:        id,
			Type:      "function",
			Name:      name,
			Arguments: args,
			Function: &FunctionCall{
				Name:      name,
				Arguments: argsString,
			},
		})
	}
	return result
}

func decodeTextToolArguments(raw json.RawMessage) (map[string]interface{}, string) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return map[string]interface{}{}, "{}"
	}

	args := map[string]interface{}{}
	if trimmed[0] == '"' {
		var encoded string
		if err := json.Unmarshal(trimmed, &encoded); err == nil {
			if err := json.Unmarshal([]byte(encoded), &args); err == nil {
				return args, encoded
			}
			return map[string]interface{}{"raw": encoded}, encoded
		}
	}

	if err := json.Unmarshal(trimmed, &args); err == nil {
		return args, string(trimmed)
	}
	return map[string]interface{}{"raw": string(trimmed)}, string(trimmed)
}

func stripTextToolCalls(text string) string {
	start, end, ok := toolCallsEnvelopeBounds(text)
	if !ok {
		return text
	}
	return strings.TrimSpace(text[:start] + text[end:])
}

func toolCallsEnvelopeBounds(text string) (int, int, bool) {
	keyPos := strings.Index(text, `"tool_calls"`)
	if keyPos == -1 {
		return 0, 0, false
	}
	start := strings.LastIndex(text[:keyPos], "{")
	if start == -1 {
		return 0, 0, false
	}
	end := findMatchingBrace(text, start)
	if end == start {
		return 0, 0, false
	}
	return start, end, true
}
