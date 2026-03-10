package tools

import (
	"context"
	"strings"

	"github.com/sipeed/picoclaw/pkg/docxreview"
)

type docxReviewToolBase struct {
	workspace               string
	restrict                bool
	sharedWorkspace         string
	sharedWorkspaceReadOnly bool
	client                  *docxreview.Client
}

func newDocxReviewToolBase(workspace string, restrict bool) docxReviewToolBase {
	return docxReviewToolBase{
		workspace: workspace,
		restrict:  restrict,
		client:    docxreview.NewClient(),
	}
}

func (b *docxReviewToolBase) SetSharedWorkspacePolicy(sharedWorkspace string, sharedWorkspaceReadOnly bool) {
	b.sharedWorkspace = strings.TrimSpace(sharedWorkspace)
	b.sharedWorkspaceReadOnly = sharedWorkspaceReadOnly
}

func (b *docxReviewToolBase) resolveReadPath(path string) (string, error) {
	return validatePathWithPolicy(path, b.workspace, b.restrict, AccessRead, b.sharedWorkspace, b.sharedWorkspaceReadOnly)
}

func (b *docxReviewToolBase) resolveWritePath(path string) (string, error) {
	return validatePathWithPolicy(path, b.workspace, b.restrict, AccessWrite, b.sharedWorkspace, b.sharedWorkspaceReadOnly)
}

type DOCXReviewReadTool struct {
	base docxReviewToolBase
}

func NewDOCXReviewReadTool(workspace string, restrict bool) *DOCXReviewReadTool {
	return &DOCXReviewReadTool{base: newDocxReviewToolBase(workspace, restrict)}
}

func newDOCXReviewReadToolWithClient(workspace string, restrict bool, client *docxreview.Client) *DOCXReviewReadTool {
	t := NewDOCXReviewReadTool(workspace, restrict)
	if client != nil {
		t.base.client = client
	}
	return t
}

func (t *DOCXReviewReadTool) SetSharedWorkspacePolicy(sharedWorkspace string, sharedWorkspaceReadOnly bool) {
	t.base.SetSharedWorkspacePolicy(sharedWorkspace, sharedWorkspaceReadOnly)
}

func (t *DOCXReviewReadTool) Name() string {
	return "docx_review_read"
}

func (t *DOCXReviewReadTool) Description() string {
	return "Read an existing .docx review document as structured JSON, including paragraphs, tracked changes, comments, metadata, and summary counts."
}

func (t *DOCXReviewReadTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"input_path": map[string]interface{}{
				"type":        "string",
				"description": "Path to the .docx file to read",
			},
		},
		"required": []string{"input_path"},
	}
}

func (t *DOCXReviewReadTool) Execute(ctx context.Context, args map[string]interface{}) *ToolResult {
	inputPath := getString(args, "input_path")
	if strings.TrimSpace(inputPath) == "" {
		return ErrorResult("input_path is required")
	}
	resolvedInputPath, err := t.base.resolveReadPath(inputPath)
	if err != nil {
		return UserErrorResult(err.Error()).WithError(err)
	}
	result, err := t.base.client.Read(ctx, resolvedInputPath)
	if err != nil {
		return ErrorResult(err.Error()).WithError(err)
	}
	return NewToolResult(mustJSON(result))
}

type DOCXReviewDiffTool struct {
	base docxReviewToolBase
}

func NewDOCXReviewDiffTool(workspace string, restrict bool) *DOCXReviewDiffTool {
	return &DOCXReviewDiffTool{base: newDocxReviewToolBase(workspace, restrict)}
}

func newDOCXReviewDiffToolWithClient(workspace string, restrict bool, client *docxreview.Client) *DOCXReviewDiffTool {
	t := NewDOCXReviewDiffTool(workspace, restrict)
	if client != nil {
		t.base.client = client
	}
	return t
}

func (t *DOCXReviewDiffTool) SetSharedWorkspacePolicy(sharedWorkspace string, sharedWorkspaceReadOnly bool) {
	t.base.SetSharedWorkspacePolicy(sharedWorkspace, sharedWorkspaceReadOnly)
}

func (t *DOCXReviewDiffTool) Name() string {
	return "docx_review_diff"
}

func (t *DOCXReviewDiffTool) Description() string {
	return "Diff two .docx files semantically and return structured text, formatting, comment, tracked-change, and metadata differences."
}

func (t *DOCXReviewDiffTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"old_path": map[string]interface{}{
				"type":        "string",
				"description": "Path to the original .docx file",
			},
			"new_path": map[string]interface{}{
				"type":        "string",
				"description": "Path to the updated .docx file",
			},
		},
		"required": []string{"old_path", "new_path"},
	}
}

func (t *DOCXReviewDiffTool) Execute(ctx context.Context, args map[string]interface{}) *ToolResult {
	oldPath := getString(args, "old_path")
	newPath := getString(args, "new_path")
	if strings.TrimSpace(oldPath) == "" {
		return ErrorResult("old_path is required")
	}
	if strings.TrimSpace(newPath) == "" {
		return ErrorResult("new_path is required")
	}
	resolvedOldPath, err := t.base.resolveReadPath(oldPath)
	if err != nil {
		return UserErrorResult(err.Error()).WithError(err)
	}
	resolvedNewPath, err := t.base.resolveReadPath(newPath)
	if err != nil {
		return UserErrorResult(err.Error()).WithError(err)
	}
	result, err := t.base.client.Diff(ctx, resolvedOldPath, resolvedNewPath)
	if err != nil {
		return ErrorResult(err.Error()).WithError(err)
	}
	return NewToolResult(mustJSON(result))
}

type DOCXReviewApplyTool struct {
	base docxReviewToolBase
}

func NewDOCXReviewApplyTool(workspace string, restrict bool) *DOCXReviewApplyTool {
	return &DOCXReviewApplyTool{base: newDocxReviewToolBase(workspace, restrict)}
}

func newDOCXReviewApplyToolWithClient(workspace string, restrict bool, client *docxreview.Client) *DOCXReviewApplyTool {
	t := NewDOCXReviewApplyTool(workspace, restrict)
	if client != nil {
		t.base.client = client
	}
	return t
}

func (t *DOCXReviewApplyTool) SetSharedWorkspacePolicy(sharedWorkspace string, sharedWorkspaceReadOnly bool) {
	t.base.SetSharedWorkspacePolicy(sharedWorkspace, sharedWorkspaceReadOnly)
}

func (t *DOCXReviewApplyTool) Name() string {
	return "docx_review_apply"
}

func (t *DOCXReviewApplyTool) Description() string {
	return "Validate or apply a docx-review JSON manifest against an existing .docx file. Prefer dry_run first, then write to a new output path."
}

func (t *DOCXReviewApplyTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"input_path": map[string]interface{}{
				"type":        "string",
				"description": "Path to the input .docx file",
			},
			"manifest_path": map[string]interface{}{
				"type":        "string",
				"description": "Path to a docx-review JSON manifest file",
			},
			"output_path": map[string]interface{}{
				"type":        "string",
				"description": "Path for the reviewed .docx output file. Required unless dry_run is true.",
			},
			"dry_run": map[string]interface{}{
				"type":        "boolean",
				"description": "Validate the manifest without modifying the document",
			},
			"author": map[string]interface{}{
				"type":        "string",
				"description": "Optional tracked-change author override",
			},
			"accept_existing": map[string]interface{}{
				"type":        "boolean",
				"description": "Optional: accept existing tracked changes before applying new edits. Default false to preserve them.",
			},
		},
		"required": []string{"input_path", "manifest_path"},
	}
}

func (t *DOCXReviewApplyTool) Execute(ctx context.Context, args map[string]interface{}) *ToolResult {
	inputPath := getString(args, "input_path")
	manifestPath := getString(args, "manifest_path")
	outputPath := getString(args, "output_path")
	dryRun := getBool(args, "dry_run")
	if strings.TrimSpace(inputPath) == "" {
		return ErrorResult("input_path is required")
	}
	if strings.TrimSpace(manifestPath) == "" {
		return ErrorResult("manifest_path is required")
	}
	if !dryRun && strings.TrimSpace(outputPath) == "" {
		return ErrorResult("output_path is required unless dry_run is true")
	}

	resolvedInputPath, err := t.base.resolveReadPath(inputPath)
	if err != nil {
		return UserErrorResult(err.Error()).WithError(err)
	}
	resolvedManifestPath, err := t.base.resolveReadPath(manifestPath)
	if err != nil {
		return UserErrorResult(err.Error()).WithError(err)
	}
	resolvedOutputPath := ""
	if strings.TrimSpace(outputPath) != "" {
		resolvedOutputPath, err = t.base.resolveWritePath(outputPath)
		if err != nil {
			return UserErrorResult(err.Error()).WithError(err)
		}
	}

	result, err := t.base.client.Apply(ctx, docxreview.ApplyRequest{
		InputPath:      resolvedInputPath,
		ManifestPath:   resolvedManifestPath,
		OutputPath:     resolvedOutputPath,
		Author:         getString(args, "author"),
		DryRun:         dryRun,
		AcceptExisting: getBool(args, "accept_existing"),
	})
	if err != nil {
		return ErrorResult(err.Error()).WithError(err)
	}
	return NewToolResult(mustJSON(result))
}
