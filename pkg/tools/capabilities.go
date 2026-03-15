package tools

// ToolAccessClass describes whether a tool is safe to expose to a read-only
// task like `/btw`.
type ToolAccessClass string

const (
	ToolAccessReadOnly ToolAccessClass = "read_only"
	ToolAccessMixed    ToolAccessClass = "mixed"
	ToolAccessMutating ToolAccessClass = "mutating"
)

// AccessClassForTool centralizes the current task-safety classification.
// The long-term direction is explicit tool metadata, but a single classifier is
// already better than maintaining an ad hoc alternate tool world elsewhere.
func AccessClassForTool(tool Tool) ToolAccessClass {
	if tool == nil {
		return ToolAccessMutating
	}

	switch tool.Name() {
	case "read_file",
		"list_dir",
		"word_count",
		"web_search",
		"web_fetch",
		"pubmed_search",
		"pubmed_fetch",
		"docx_review_read",
		"docx_review_diff",
		"xlsx_review_read",
		"xlsx_review_diff",
		"pptx_review_read",
		"pptx_review_diff",
		"pdf_form_inspect",
		"pdf_form_schema",
		"channel_history":
		return ToolAccessReadOnly
	case "irl_project",
		"i2c",
		"spi":
		// These tools mix safe and mutating operations behind one function
		// surface, so they are not suitable for `/btw` until they are split or
		// advertise per-operation constraints.
		return ToolAccessMixed
	default:
		return ToolAccessMutating
	}
}

func ReadOnlyCompatibleTool(tool Tool) bool {
	return AccessClassForTool(tool) == ToolAccessReadOnly
}
