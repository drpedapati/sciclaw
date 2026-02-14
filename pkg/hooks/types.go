package hooks

import (
	"context"
	"time"
)

// Event defines a hook lifecycle trigger.
type Event string

const (
	EventBeforeTurn Event = "before_turn"
	EventAfterTurn  Event = "after_turn"
	EventBeforeLLM  Event = "before_llm"
	EventAfterLLM   Event = "after_llm"
	EventBeforeTool Event = "before_tool"
	EventAfterTool  Event = "after_tool"
	EventOnError    Event = "on_error"
)

var knownEvents = []Event{
	EventBeforeTurn,
	EventAfterTurn,
	EventBeforeLLM,
	EventAfterLLM,
	EventBeforeTool,
	EventAfterTool,
	EventOnError,
}

func KnownEvents() []Event {
	out := make([]Event, len(knownEvents))
	copy(out, knownEvents)
	return out
}

func IsKnownEvent(ev Event) bool {
	for _, known := range knownEvents {
		if known == ev {
			return true
		}
	}
	return false
}

// Context is an immutable hook event snapshot.
type Context struct {
	Timestamp          time.Time      `json:"timestamp"`
	TurnID             string         `json:"turn_id"`
	SessionKey         string         `json:"session_key,omitempty"`
	Channel            string         `json:"channel,omitempty"`
	ChatID             string         `json:"chat_id,omitempty"`
	Model              string         `json:"model,omitempty"`
	UserMessage        string         `json:"user_message,omitempty"`
	ToolName           string         `json:"tool_name,omitempty"`
	ToolArgs           map[string]any `json:"tool_args,omitempty"`
	ToolResult         string         `json:"tool_result,omitempty"`
	LLMResponseSummary string         `json:"llm_response_summary,omitempty"`
	ErrorMessage       string         `json:"error_message,omitempty"`
	Metadata           map[string]any `json:"metadata,omitempty"`
}

// Result is the hook execution result.
type Result struct {
	Status     string         `json:"status"`
	Message    string         `json:"message,omitempty"`
	Metadata   map[string]any `json:"metadata,omitempty"`
	Err        error          `json:"-"`
	DurationMs int64          `json:"duration_ms"`
}

const (
	StatusOK    = "ok"
	StatusError = "error"
)

// Handler handles hook events.
type Handler interface {
	Name() string
	Handle(ctx context.Context, ev Event, data Context) Result
}

// AuditEntry is persisted for reproducibility and troubleshooting.
type AuditEntry struct {
	TurnID     string         `json:"turn_id"`
	Event      Event          `json:"event"`
	Handler    string         `json:"handler"`
	Status     string         `json:"status"`
	Message    string         `json:"message,omitempty"`
	Error      string         `json:"error,omitempty"`
	DurationMs int64          `json:"duration_ms"`
	Timestamp  time.Time      `json:"timestamp"`
	SessionKey string         `json:"session_key,omitempty"`
	Channel    string         `json:"channel,omitempty"`
	ChatID     string         `json:"chat_id,omitempty"`
	Metadata   map[string]any `json:"metadata,omitempty"`
}

// AuditSink writes hook audit entries.
type AuditSink interface {
	Write(entry AuditEntry) error
}
