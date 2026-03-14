package pptxreview

import "encoding/json"

type EditResult struct {
	Index   int    `json:"index"`
	Type    string `json:"type"`
	Success bool   `json:"success"`
	Message string `json:"message"`
}

type ProcessingResult struct {
	Input             string       `json:"input,omitempty"`
	Output            string       `json:"output,omitempty"`
	Author            string       `json:"author,omitempty"`
	ChangesAttempted  int          `json:"changes_attempted"`
	ChangesSucceeded  int          `json:"changes_succeeded"`
	CommentsAttempted int          `json:"comments_attempted"`
	CommentsSucceeded int          `json:"comments_succeeded"`
	Results           []EditResult `json:"results"`
	Success           bool         `json:"success"`
}

type ApplyRequest struct {
	InputPath    string
	ManifestPath string
	OutputPath   string
	Author       string
	DryRun       bool
}

type ApplyResult struct {
	ProcessingResult
	Status        string `json:"status"`
	ExitCode      int    `json:"exitCode"`
	DryRun        bool   `json:"dryRun"`
	OutputWritten bool   `json:"outputWritten"`
}

type ReadResult struct {
	SlideCount int             `json:"slide_count"`
	Slides     json.RawMessage `json:"slides"`
}

type DiffResult struct {
	OldFile  string          `json:"old_file"`
	NewFile  string          `json:"new_file"`
	Metadata json.RawMessage `json:"metadata"`
	Slides   json.RawMessage `json:"slides"`
	Summary  json.RawMessage `json:"summary"`
}
