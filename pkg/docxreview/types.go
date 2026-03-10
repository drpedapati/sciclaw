package docxreview

type EditResult struct {
	Index   int    `json:"index"`
	Type    string `json:"type"`
	Success bool   `json:"success"`
	Message string `json:"message"`
}

type ProcessingResult struct {
	Input             string       `json:"input"`
	Output            string       `json:"output,omitempty"`
	Author            string       `json:"author"`
	ChangesAttempted  int          `json:"changes_attempted"`
	ChangesSucceeded  int          `json:"changes_succeeded"`
	CommentsAttempted int          `json:"comments_attempted"`
	CommentsSucceeded int          `json:"comments_succeeded"`
	Results           []EditResult `json:"results"`
	Success           bool         `json:"success"`
}

type ApplyRequest struct {
	InputPath      string
	ManifestPath   string
	OutputPath     string
	Author         string
	DryRun         bool
	AcceptExisting bool
}

type ApplyResult struct {
	ProcessingResult
	Status         string `json:"status"`
	ExitCode       int    `json:"exitCode"`
	DryRun         bool   `json:"dryRun"`
	AcceptExisting bool   `json:"acceptExisting"`
	OutputWritten  bool   `json:"outputWritten"`
}

type ReadResult struct {
	File       string           `json:"file"`
	Paragraphs []ParagraphInfo  `json:"paragraphs"`
	Comments   []CommentInfo    `json:"comments"`
	Metadata   DocumentMetadata `json:"metadata"`
	Summary    ReadSummary      `json:"summary"`
}

type ParagraphInfo struct {
	Index          int                 `json:"index"`
	Style          string              `json:"style,omitempty"`
	Text           string              `json:"text"`
	TrackedChanges []TrackedChangeInfo `json:"tracked_changes"`
}

type TrackedChangeInfo struct {
	Type   string `json:"type"`
	Text   string `json:"text"`
	Author string `json:"author"`
	Date   string `json:"date,omitempty"`
	ID     string `json:"id"`
}

type CommentInfo struct {
	ID             string `json:"id"`
	Author         string `json:"author"`
	Date           string `json:"date,omitempty"`
	AnchorText     string `json:"anchor_text"`
	Text           string `json:"text"`
	ParagraphIndex int    `json:"paragraph_index"`
}

type DocumentMetadata struct {
	Title          string `json:"title,omitempty"`
	Author         string `json:"author,omitempty"`
	LastModifiedBy string `json:"last_modified_by,omitempty"`
	Created        string `json:"created,omitempty"`
	Modified       string `json:"modified,omitempty"`
	Revision       *int   `json:"revision,omitempty"`
	WordCount      int    `json:"word_count"`
	ParagraphCount int    `json:"paragraph_count"`
}

type ReadSummary struct {
	TotalTrackedChanges int      `json:"total_tracked_changes"`
	Insertions          int      `json:"insertions"`
	Deletions           int      `json:"deletions"`
	TotalComments       int      `json:"total_comments"`
	ChangeAuthors       []string `json:"change_authors"`
	CommentAuthors      []string `json:"comment_authors"`
}

type DiffResult struct {
	OldFile        string            `json:"old_file"`
	NewFile        string            `json:"new_file"`
	Metadata       MetadataDiff      `json:"metadata"`
	Paragraphs     ParagraphDiff     `json:"paragraphs"`
	Comments       CommentDiff       `json:"comments"`
	TrackedChanges TrackedChangeDiff `json:"tracked_changes"`
	Summary        DiffSummary       `json:"summary"`
}

type MetadataDiff struct {
	Changes []FieldChange `json:"changes"`
}

type FieldChange struct {
	Field string      `json:"field"`
	Old   interface{} `json:"old,omitempty"`
	New   interface{} `json:"new,omitempty"`
}

type ParagraphDiff struct {
	Added    []ParagraphEntry        `json:"added"`
	Deleted  []ParagraphEntry        `json:"deleted"`
	Modified []ParagraphModification `json:"modified"`
}

type ParagraphEntry struct {
	Index int    `json:"index"`
	Style string `json:"style,omitempty"`
	Text  string `json:"text"`
}

type ParagraphModification struct {
	OldIndex          int                `json:"old_index"`
	NewIndex          int                `json:"new_index"`
	OldText           string             `json:"old_text"`
	NewText           string             `json:"new_text"`
	StyleChange       *StyleChange       `json:"style_change,omitempty"`
	FormattingChanges []FormattingChange `json:"formatting_changes"`
	WordChanges       []WordChange       `json:"word_changes"`
}

type StyleChange struct {
	Old string `json:"old,omitempty"`
	New string `json:"new,omitempty"`
}

type FormattingChange struct {
	Word     string `json:"word"`
	Property string `json:"property"`
	OldValue string `json:"old_value,omitempty"`
	NewValue string `json:"new_value,omitempty"`
}

type WordChange struct {
	Type     string `json:"type"`
	Old      string `json:"old,omitempty"`
	New      string `json:"new,omitempty"`
	Position int    `json:"position"`
}

type CommentDiff struct {
	Added    []CommentInfo         `json:"added"`
	Deleted  []CommentInfo         `json:"deleted"`
	Modified []CommentModification `json:"modified"`
}

type CommentModification struct {
	Author     string `json:"author"`
	AnchorText string `json:"anchor_text"`
	OldText    string `json:"old_text"`
	NewText    string `json:"new_text"`
}

type TrackedChangeDiff struct {
	Added   []TrackedChangeInfo `json:"added"`
	Deleted []TrackedChangeInfo `json:"deleted"`
}

type DiffSummary struct {
	TextChanges          int  `json:"text_changes"`
	ParagraphsAdded      int  `json:"paragraphs_added"`
	ParagraphsDeleted    int  `json:"paragraphs_deleted"`
	ParagraphsModified   int  `json:"paragraphs_modified"`
	CommentChanges       int  `json:"comment_changes"`
	TrackedChangeChanges int  `json:"tracked_change_changes"`
	FormattingChanges    int  `json:"formatting_changes"`
	StyleChanges         int  `json:"style_changes"`
	MetadataChanges      int  `json:"metadata_changes"`
	Identical            bool `json:"identical"`
}
