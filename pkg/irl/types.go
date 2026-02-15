package irl

type CommandStatus string

const (
	StatusSuccess CommandStatus = "success"
	StatusFailure CommandStatus = "failure"
	StatusPartial CommandStatus = "partial"
)

type CommandRecord struct {
	EventType  string        `json:"event_type"`
	EventID    string        `json:"event_id"`
	Timestamp  string        `json:"timestamp"`
	Operation  string        `json:"operation"`
	Attempt    int           `json:"attempt"`
	IRLPath    string        `json:"irl_path"`
	IRLVersion string        `json:"irl_version,omitempty"`
	Command    []string      `json:"command"`
	CWD        string        `json:"cwd"`
	ExitCode   int           `json:"exit_code"`
	Status     CommandStatus `json:"status"`
	DurationMs int64         `json:"duration_ms"`
	Stdout     string        `json:"stdout,omitempty"`
	Stderr     string        `json:"stderr,omitempty"`
	Parsed     interface{}   `json:"parsed,omitempty"`
	Error      string        `json:"error,omitempty"`
	StorePath  string        `json:"store_path,omitempty"`
	StoreError string        `json:"store_error,omitempty"`
}

type CommandSummary struct {
	EventID    string        `json:"event_id"`
	Operation  string        `json:"operation"`
	Attempt    int           `json:"attempt"`
	Command    []string      `json:"command"`
	ExitCode   int           `json:"exit_code"`
	Status     CommandStatus `json:"status"`
	StorePath  string        `json:"store_path,omitempty"`
	Error      string        `json:"error,omitempty"`
	StoreError string        `json:"store_error,omitempty"`
}

type OperationResult struct {
	Operation string                 `json:"operation"`
	Status    CommandStatus          `json:"status"`
	Data      map[string]interface{} `json:"data,omitempty"`
	Commands  []CommandSummary       `json:"commands"`
}
