package providers

import "context"

type ToolCall struct {
	ID        string                 `json:"id"`
	Type      string                 `json:"type,omitempty"`
	Function  *FunctionCall          `json:"function,omitempty"`
	Name      string                 `json:"name,omitempty"`
	Arguments map[string]interface{} `json:"arguments,omitempty"`
}

type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// MediaAttachment is binary media produced by a provider turn (for example
// OpenAI Responses image_generation_call output). Prefer Path when the bytes
// are already on disk; otherwise Data holds decoded image bytes for the agent
// to persist under the workspace.
type MediaAttachment struct {
	Filename string `json:"filename,omitempty"`
	MIME     string `json:"mime,omitempty"`
	Path     string `json:"path,omitempty"`
	Data     []byte `json:"-"`
}

type LLMResponse struct {
	Content      string               `json:"content"`
	ToolCalls    []ToolCall           `json:"tool_calls,omitempty"`
	Media        []MediaAttachment    `json:"media,omitempty"`
	FinishReason string               `json:"finish_reason"`
	Usage        *UsageInfo           `json:"usage,omitempty"`
	Diagnostics  *ResponseDiagnostics `json:"diagnostics,omitempty"`
}

type ResponseDiagnostics struct {
	ContentSource  string `json:"content_source,omitempty"`
	ToolCallSource string `json:"tool_call_source,omitempty"`
}

type UsageInfo struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	ToolName   string     `json:"tool_name,omitempty"`
}

type LLMProvider interface {
	Chat(ctx context.Context, messages []Message, tools []ToolDefinition, model string, options map[string]interface{}) (*LLMResponse, error)
	GetDefaultModel() string
}

type ToolDefinition struct {
	Type     string                 `json:"type"`
	Function ToolFunctionDefinition `json:"function"`
}

type ToolFunctionDefinition struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters"`
}
