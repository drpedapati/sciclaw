package providers

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/openai/openai-go/v3"
	openaiopt "github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/responses"
)

func TestBuildCodexParams_BasicMessage(t *testing.T) {
	messages := []Message{
		{Role: "user", Content: "Hello"},
	}
	params := buildCodexParams(messages, nil, "openai/gpt-5.4", map[string]interface{}{
		"max_tokens": 2048,
	})
	if params.Model != "gpt-5.4" {
		t.Errorf("Model = %q, want %q", params.Model, "gpt-5.4")
	}
}

func TestBuildCodexParams_SystemAsInstructions(t *testing.T) {
	messages := []Message{
		{Role: "system", Content: "You are helpful"},
		{Role: "user", Content: "Hi"},
	}
	params := buildCodexParams(messages, nil, "gpt-4o", map[string]interface{}{})
	if !params.Instructions.Valid() {
		t.Fatal("Instructions should be set")
	}
	if params.Instructions.Or("") != "You are helpful" {
		t.Errorf("Instructions = %q, want %q", params.Instructions.Or(""), "You are helpful")
	}
}

func TestBuildCodexParams_ToolCallConversation(t *testing.T) {
	messages := []Message{
		{Role: "user", Content: "What's the weather?"},
		{
			Role: "assistant",
			ToolCalls: []ToolCall{
				{ID: "call_1", Name: "get_weather", Arguments: map[string]interface{}{"city": "SF"}},
			},
		},
		{Role: "tool", Content: `{"temp": 72}`, ToolCallID: "call_1"},
	}
	params := buildCodexParams(messages, nil, "gpt-4o", map[string]interface{}{})
	if params.Input.OfInputItemList == nil {
		t.Fatal("Input.OfInputItemList should not be nil")
	}
	if len(params.Input.OfInputItemList) != 3 {
		t.Errorf("len(Input items) = %d, want 3", len(params.Input.OfInputItemList))
	}
}

func TestBuildCodexParams_AssistantToolCallUsesLegacyFunctionName(t *testing.T) {
	messages := []Message{
		{Role: "user", Content: "Run ls"},
		{
			Role: "assistant",
			ToolCalls: []ToolCall{
				{
					ID:   "call_legacy",
					Name: "",
					Function: &FunctionCall{
						Name:      "list_dir",
						Arguments: `{"path":"."}`,
					},
				},
			},
		},
	}

	params := buildCodexParams(messages, nil, "gpt-4o", map[string]interface{}{})
	if params.Input.OfInputItemList == nil {
		t.Fatal("Input.OfInputItemList should not be nil")
	}
	if len(params.Input.OfInputItemList) != 2 {
		t.Fatalf("len(Input items) = %d, want 2", len(params.Input.OfInputItemList))
	}

	fc := params.Input.OfInputItemList[1].OfFunctionCall
	if fc == nil {
		t.Fatal("second input item should be a function call")
	}
	if fc.Name != "list_dir" {
		t.Errorf("function call name = %q, want %q", fc.Name, "list_dir")
	}
	if fc.Arguments != `{"path":"."}` {
		t.Errorf("function call args = %q, want %q", fc.Arguments, `{"path":"."}`)
	}
}

func TestBuildCodexParams_WithTools(t *testing.T) {
	tools := []ToolDefinition{
		{
			Type: "function",
			Function: ToolFunctionDefinition{
				Name:        "get_weather",
				Description: "Get weather",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"city": map[string]interface{}{"type": "string"},
					},
				},
			},
		},
	}
	params := buildCodexParams([]Message{{Role: "user", Content: "Hi"}}, tools, "gpt-4o", map[string]interface{}{})
	if len(params.Tools) != 2 {
		t.Fatalf("len(Tools) = %d, want 2 (function + image_generation)", len(params.Tools))
	}
	if params.Tools[0].OfFunction == nil {
		t.Fatal("first tool should be a function tool")
	}
	if params.Tools[0].OfFunction.Name != "get_weather" {
		t.Errorf("Tool name = %q, want %q", params.Tools[0].OfFunction.Name, "get_weather")
	}
	if params.Tools[1].OfImageGeneration == nil {
		t.Fatal("second tool should be image_generation")
	}
}

func TestParseCodexResponse_ImageGenerationCall(t *testing.T) {
	// 1x1 PNG
	png := []byte{
		0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a,
		0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52,
		0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
		0x08, 0x02, 0x00, 0x00, 0x00, 0x90, 0x77, 0x53,
		0xde, 0x00, 0x00, 0x00, 0x0c, 0x49, 0x44, 0x41,
		0x54, 0x08, 0xd7, 0x63, 0xf8, 0xcf, 0xc0, 0x00,
		0x00, 0x00, 0x03, 0x00, 0x01, 0x00, 0x05, 0xfe,
		0xd4, 0xef, 0x00, 0x00, 0x00, 0x00, 0x49, 0x45,
		0x4e, 0x44, 0xae, 0x42, 0x60, 0x82,
	}
	b64 := base64.StdEncoding.EncodeToString(png)
	respJSON := fmt.Sprintf(`{
		"id": "resp_test",
		"object": "response",
		"status": "completed",
		"output": [
			{
				"id": "ig_1",
				"type": "image_generation_call",
				"status": "completed",
				"result": %q
			},
			{
				"id": "msg_1",
				"type": "message",
				"role": "assistant",
				"status": "completed",
				"content": [
					{"type": "output_text", "text": "Here is your image."}
				]
			}
		],
		"usage": {
			"input_tokens": 10,
			"output_tokens": 5,
			"total_tokens": 15,
			"input_tokens_details": {"cached_tokens": 0},
			"output_tokens_details": {"reasoning_tokens": 0}
		}
	}`, b64)

	var resp responses.Response
	if err := json.Unmarshal([]byte(respJSON), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	result := parseCodexResponse(&resp)
	if result.Content != "Here is your image." {
		t.Errorf("Content = %q, want %q", result.Content, "Here is your image.")
	}
	if len(result.Media) != 1 {
		t.Fatalf("len(Media) = %d, want 1", len(result.Media))
	}
	if result.Media[0].MIME != "image/png" {
		t.Errorf("MIME = %q, want image/png", result.Media[0].MIME)
	}
	if len(result.Media[0].Data) != len(png) {
		t.Fatalf("media bytes = %d, want %d", len(result.Media[0].Data), len(png))
	}
	if result.FinishReason != "stop" {
		t.Errorf("FinishReason = %q, want stop", result.FinishReason)
	}
}

func TestBuildCodexParams_StoreIsFalse(t *testing.T) {
	params := buildCodexParams([]Message{{Role: "user", Content: "Hi"}}, nil, "gpt-4o", map[string]interface{}{})
	if !params.Store.Valid() || params.Store.Or(true) != false {
		t.Error("Store should be explicitly set to false")
	}
}

func TestParseCodexResponse_TextOutput(t *testing.T) {
	respJSON := `{
		"id": "resp_test",
		"object": "response",
		"status": "completed",
		"output": [
			{
				"id": "msg_1",
				"type": "message",
				"role": "assistant",
				"status": "completed",
				"content": [
					{"type": "output_text", "text": "Hello there!"}
				]
			}
		],
		"usage": {
			"input_tokens": 10,
			"output_tokens": 5,
			"total_tokens": 15,
			"input_tokens_details": {"cached_tokens": 0},
			"output_tokens_details": {"reasoning_tokens": 0}
		}
	}`

	var resp responses.Response
	if err := json.Unmarshal([]byte(respJSON), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	result := parseCodexResponse(&resp)
	if result.Content != "Hello there!" {
		t.Errorf("Content = %q, want %q", result.Content, "Hello there!")
	}
	if result.FinishReason != "stop" {
		t.Errorf("FinishReason = %q, want %q", result.FinishReason, "stop")
	}
	if result.Usage.TotalTokens != 15 {
		t.Errorf("TotalTokens = %d, want 15", result.Usage.TotalTokens)
	}
}

func TestParseCodexResponse_FunctionCall(t *testing.T) {
	respJSON := `{
		"id": "resp_test",
		"object": "response",
		"status": "completed",
		"output": [
			{
				"id": "fc_1",
				"type": "function_call",
				"call_id": "call_abc",
				"name": "get_weather",
				"arguments": "{\"city\":\"SF\"}",
				"status": "completed"
			}
		],
		"usage": {
			"input_tokens": 10,
			"output_tokens": 8,
			"total_tokens": 18,
			"input_tokens_details": {"cached_tokens": 0},
			"output_tokens_details": {"reasoning_tokens": 0}
		}
	}`

	var resp responses.Response
	if err := json.Unmarshal([]byte(respJSON), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	result := parseCodexResponse(&resp)
	if len(result.ToolCalls) != 1 {
		t.Fatalf("len(ToolCalls) = %d, want 1", len(result.ToolCalls))
	}
	tc := result.ToolCalls[0]
	if tc.Name != "get_weather" {
		t.Errorf("ToolCall.Name = %q, want %q", tc.Name, "get_weather")
	}
	if tc.ID != "call_abc" {
		t.Errorf("ToolCall.ID = %q, want %q", tc.ID, "call_abc")
	}
	if tc.Arguments["city"] != "SF" {
		t.Errorf("ToolCall.Arguments[city] = %v, want SF", tc.Arguments["city"])
	}
	if result.FinishReason != "tool_calls" {
		t.Errorf("FinishReason = %q, want %q", result.FinishReason, "tool_calls")
	}
}

func TestCodexProvider_ChatRoundTrip(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/responses" {
			http.Error(w, "not found: "+r.URL.Path, http.StatusNotFound)
			return
		}
		if r.Header.Get("Authorization") != "Bearer test-token" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if r.Header.Get("Chatgpt-Account-Id") != "acc-123" {
			http.Error(w, "missing account id", http.StatusBadRequest)
			return
		}

		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "bad json body", http.StatusBadRequest)
			return
		}
		if stream, ok := body["stream"].(bool); !ok || !stream {
			http.Error(w, "stream must be true", http.StatusBadRequest)
			return
		}

		event := map[string]interface{}{
			"type":            "response.completed",
			"sequence_number": 1,
			"response": map[string]interface{}{
				"id":     "resp_test",
				"object": "response",
				"status": "completed",
				"output": []map[string]interface{}{
					{
						"id":     "msg_1",
						"type":   "message",
						"role":   "assistant",
						"status": "completed",
						"content": []map[string]interface{}{
							{"type": "output_text", "text": "Hi from Codex!"},
						},
					},
				},
				"usage": map[string]interface{}{
					"input_tokens":          12,
					"output_tokens":         6,
					"total_tokens":          18,
					"input_tokens_details":  map[string]interface{}{"cached_tokens": 0},
					"output_tokens_details": map[string]interface{}{"reasoning_tokens": 0},
				},
			},
		}
		eventJSON, err := json.Marshal(event)
		if err != nil {
			http.Error(w, "marshal event failed", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprintf(w, "event: response.completed\n")
		_, _ = fmt.Fprintf(w, "data: %s\n\n", string(eventJSON))
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer server.Close()

	provider := NewCodexProvider("test-token", "acc-123")
	provider.client = createOpenAITestClient(server.URL, "test-token", "acc-123")

	messages := []Message{{Role: "user", Content: "Hello"}}
	resp, err := provider.Chat(t.Context(), messages, nil, "gpt-4o", map[string]interface{}{"max_tokens": 1024})
	if err != nil {
		t.Fatalf("Chat() error: %v", err)
	}
	if resp.Content != "Hi from Codex!" {
		t.Errorf("Content = %q, want %q", resp.Content, "Hi from Codex!")
	}
	if resp.FinishReason != "stop" {
		t.Errorf("FinishReason = %q, want %q", resp.FinishReason, "stop")
	}
	if resp.Usage.TotalTokens != 18 {
		t.Errorf("TotalTokens = %d, want 18", resp.Usage.TotalTokens)
	}
}

func TestCodexProvider_GetDefaultModel(t *testing.T) {
	p := NewCodexProvider("test-token", "")
	if got := p.GetDefaultModel(); got != "gpt-5.6-sol" {
		t.Errorf("GetDefaultModel() = %q, want %q", got, "gpt-5.6-sol")
	}
}

func TestCodexProvider_ChatRoundTrip_OutputTextDeltaFallback(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/responses" {
			http.Error(w, "not found: "+r.URL.Path, http.StatusNotFound)
			return
		}
		var reqBody map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		if stream, ok := reqBody["stream"].(bool); !ok || !stream {
			http.Error(w, "stream must be true", http.StatusBadRequest)
			return
		}

		resp := map[string]interface{}{
			"id":     "resp_test",
			"object": "response",
			"status": "completed",
			"output": nil,
		}
		writeOutputTextDeltaSSE(w, "OK", resp)
	}))
	defer server.Close()

	provider := NewCodexProvider("test-token", "acc-123")
	provider.client = createOpenAITestClient(server.URL, "test-token", "acc-123")

	resp, err := provider.Chat(t.Context(), []Message{{Role: "user", Content: "Hello"}}, nil, "gpt-5.6-sol", map[string]interface{}{})
	if err != nil {
		t.Fatalf("Chat() error: %v", err)
	}
	if resp.Content != "OK" {
		t.Errorf("Content = %q, want %q", resp.Content, "OK")
	}
}

func TestCodexProvider_Chat_IncompleteDoesNotTreatDeltaAsComplete(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/responses" {
			http.Error(w, "not found: "+r.URL.Path, http.StatusNotFound)
			return
		}
		resp := map[string]interface{}{
			"id":     "resp_trunc",
			"object": "response",
			"status": "incomplete",
			"output": nil,
		}
		writeOutputTextDeltaIncompleteSSE(w, "mid-sentence fragment", resp)
	}))
	defer server.Close()

	provider := NewCodexProvider("test-token", "acc-123")
	provider.client = createOpenAITestClient(server.URL, "test-token", "acc-123")

	resp, err := provider.Chat(t.Context(), []Message{{Role: "user", Content: "Hello"}}, nil, "gpt-5.6-sol", map[string]interface{}{})
	if err == nil {
		t.Fatal("expected incomplete stream to return an error")
	}
	if !strings.Contains(err.Error(), "incomplete") {
		t.Fatalf("error = %q, want incomplete", err.Error())
	}
	if resp == nil {
		t.Fatal("expected partial response payload alongside error")
	}
	if resp.FinishReason != "length" {
		t.Fatalf("FinishReason = %q, want length", resp.FinishReason)
	}
	if resp.Content != "" {
		t.Fatalf("Content = %q, want empty (delta fallback skipped for incomplete)", resp.Content)
	}
}

func TestResolveCodexModel(t *testing.T) {
	fallback := (&CodexProvider{}).GetDefaultModel()
	tests := []struct {
		name         string
		input        string
		wantModel    string
		wantFallback bool
		wantErr      bool
	}{
		{"empty", "", fallback, true, false},
		{"sol", "gpt-5.6-sol", "gpt-5.6-sol", false, false},
		{"prefixed", "openai/gpt-5.6-terra", "gpt-5.6-terra", false, false},
		{"gpt52 remapped", "gpt-5.2", fallback, true, false},
		{"gpt52 codex kept", "gpt-5.2-codex", "gpt-5.2-codex", false, false},
		{"claude", "claude-sonnet-4.6", "", false, true},
		{"anthropic ns", "anthropic/claude-sonnet-4.6", "", false, true},
		{"o1", "o1-pro", "o1-pro", false, false},
		{"o3", "o3-mini", "o3-mini", false, false},
		{"codex mini", "codex-mini-latest", "codex-mini-latest", false, false},
		{"unknown family", "banana-1", "", false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotModel, reason, err := resolveCodexModel(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("resolveCodexModel(%q) expected error", tt.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("resolveCodexModel(%q) unexpected error: %v", tt.input, err)
			}
			if gotModel != tt.wantModel {
				t.Fatalf("resolveCodexModel(%q) model = %q, want %q", tt.input, gotModel, tt.wantModel)
			}
			if tt.wantFallback && reason == "" {
				t.Fatalf("resolveCodexModel(%q) expected fallback reason", tt.input)
			}
			if !tt.wantFallback && reason != "" {
				t.Fatalf("resolveCodexModel(%q) unexpected fallback reason: %q", tt.input, reason)
			}
		})
	}
}

func TestCodexAPIErrorFields_NoSecretLeakage(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "https://chatgpt.com/backend-api/codex/responses", nil)
	apiErr := &openai.Error{
		StatusCode: 400,
		Type:       "invalid_request_error",
		Code:       "unsupported_model",
		Param:      "model",
		Message:    "model not allowed",
		Request:    req,
		Response: &http.Response{
			StatusCode: 400,
			Header:     http.Header{"X-Request-Id": []string{"req_abc"}},
		},
	}
	fields := codexAPIErrorFields(apiErr, "claude-sonnet-4.6", "gpt-5.6-sol", 2, 1, true)
	if fields["status_code"] != 400 {
		t.Fatalf("status_code = %v, want 400", fields["status_code"])
	}
	if fields["api_code"] != "unsupported_model" {
		t.Fatalf("api_code = %v", fields["api_code"])
	}
	if fields["api_param"] != "model" {
		t.Fatalf("api_param = %v", fields["api_param"])
	}
	if fields["request_id"] != "req_abc" {
		t.Fatalf("request_id = %v", fields["request_id"])
	}
	if fields["hint"] == nil {
		t.Fatal("expected 400 hint")
	}
	for k, v := range fields {
		s := fmt.Sprintf("%v", v)
		low := strings.ToLower(s)
		if strings.Contains(low, "bearer") || strings.Contains(strings.ToLower(k), "authorization") {
			t.Fatalf("fields leaked auth material: %s=%v", k, v)
		}
		if strings.Contains(s, "sk-") || strings.Contains(s, "eyJ") {
			t.Fatalf("fields look like secrets: %s=%v", k, v)
		}
	}
}

func TestNormalizeOpenAIModel(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"gpt-5.4", "gpt-5.4"},
		{"openai/gpt-5.4", "gpt-5.4"},
	}
	for _, tt := range tests {
		if got := normalizeOpenAIModel(tt.in); got != tt.want {
			t.Fatalf("normalizeOpenAIModel(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func createOpenAITestClient(baseURL, token, accountID string) *openai.Client {
	opts := []openaiopt.RequestOption{
		openaiopt.WithBaseURL(baseURL),
		openaiopt.WithAPIKey(token),
	}
	if accountID != "" {
		opts = append(opts, openaiopt.WithHeader("Chatgpt-Account-Id", accountID))
	}
	c := openai.NewClient(opts...)
	return &c
}

func writeOutputTextDeltaSSE(w http.ResponseWriter, delta string, response map[string]interface{}) {
	deltaEvent := map[string]interface{}{
		"type":            "response.output_text.delta",
		"sequence_number": 1,
		"delta":           delta,
	}
	completedEvent := map[string]interface{}{
		"type":            "response.completed",
		"sequence_number": 2,
		"response":        response,
	}
	deltaBytes, _ := json.Marshal(deltaEvent)
	completedBytes, _ := json.Marshal(completedEvent)
	w.Header().Set("Content-Type", "text/event-stream")
	_, _ = fmt.Fprintf(w, "event: response.output_text.delta\n")
	_, _ = fmt.Fprintf(w, "data: %s\n\n", string(deltaBytes))
	_, _ = fmt.Fprintf(w, "event: response.completed\n")
	_, _ = fmt.Fprintf(w, "data: %s\n\n", string(completedBytes))
	_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
}

func writeOutputTextDeltaIncompleteSSE(w http.ResponseWriter, delta string, response map[string]interface{}) {
	deltaEvent := map[string]interface{}{
		"type":            "response.output_text.delta",
		"sequence_number": 1,
		"delta":           delta,
	}
	incompleteEvent := map[string]interface{}{
		"type":            "response.incomplete",
		"sequence_number": 2,
		"response":        response,
	}
	deltaBytes, _ := json.Marshal(deltaEvent)
	incompleteBytes, _ := json.Marshal(incompleteEvent)
	w.Header().Set("Content-Type", "text/event-stream")
	_, _ = fmt.Fprintf(w, "event: response.output_text.delta\n")
	_, _ = fmt.Fprintf(w, "data: %s\n\n", string(deltaBytes))
	_, _ = fmt.Fprintf(w, "event: response.incomplete\n")
	_, _ = fmt.Fprintf(w, "data: %s\n\n", string(incompleteBytes))
	_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
}
