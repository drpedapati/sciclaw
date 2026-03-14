package xlsxreview

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
	Workbook json.RawMessage `json:"workbook"`
	Sheets   json.RawMessage `json:"sheets"`
	Warnings []string        `json:"warnings,omitempty"`
}

type DiffResult struct {
	OldFile        string          `json:"old_file"`
	NewFile        string          `json:"new_file"`
	CellChanges    json.RawMessage `json:"cell_changes"`
	FormulaChanges json.RawMessage `json:"formula_changes"`
	MetadataDiff   json.RawMessage `json:"metadata_diff"`
	SheetsDiff     json.RawMessage `json:"sheets_diff"`
	StructureDiff  json.RawMessage `json:"structure_diff"`
	Summary        json.RawMessage `json:"summary"`
}
