package tools

import (
	"context"
	"strings"

	"github.com/sipeed/picoclaw/pkg/xlsxreview"
)

type xlsxReviewToolBase struct {
	workspace               string
	restrict                bool
	sharedWorkspace         string
	sharedWorkspaceReadOnly bool
	client                  *xlsxreview.Client
}

func newXLSXReviewToolBase(workspace string, restrict bool) xlsxReviewToolBase {
	return xlsxReviewToolBase{workspace: workspace, restrict: restrict, client: xlsxreview.NewClient()}
}

func (b *xlsxReviewToolBase) SetSharedWorkspacePolicy(sharedWorkspace string, sharedWorkspaceReadOnly bool) {
	b.sharedWorkspace = strings.TrimSpace(sharedWorkspace)
	b.sharedWorkspaceReadOnly = sharedWorkspaceReadOnly
}

func (b *xlsxReviewToolBase) resolveReadPath(path string) (string, error) {
	return validatePathWithPolicy(path, b.workspace, b.restrict, AccessRead, b.sharedWorkspace, b.sharedWorkspaceReadOnly)
}

func (b *xlsxReviewToolBase) resolveWritePath(path string) (string, error) {
	return validatePathWithPolicy(path, b.workspace, b.restrict, AccessWrite, b.sharedWorkspace, b.sharedWorkspaceReadOnly)
}

type XLSXReviewReadTool struct{ base xlsxReviewToolBase }

func NewXLSXReviewReadTool(workspace string, restrict bool) *XLSXReviewReadTool {
	return &XLSXReviewReadTool{base: newXLSXReviewToolBase(workspace, restrict)}
}

func newXLSXReviewReadToolWithClient(workspace string, restrict bool, client *xlsxreview.Client) *XLSXReviewReadTool {
	t := NewXLSXReviewReadTool(workspace, restrict)
	if client != nil {
		t.base.client = client
	}
	return t
}

func (t *XLSXReviewReadTool) SetSharedWorkspacePolicy(sharedWorkspace string, sharedWorkspaceReadOnly bool) {
	t.base.SetSharedWorkspacePolicy(sharedWorkspace, sharedWorkspaceReadOnly)
}
func (t *XLSXReviewReadTool) Name() string { return "xlsx_review_read" }
func (t *XLSXReviewReadTool) Description() string {
	return "Read an existing .xlsx workbook as structured JSON, including workbook metadata, sheets, and warnings."
}
func (t *XLSXReviewReadTool) Parameters() map[string]interface{} {
	return map[string]interface{}{"type": "object", "properties": map[string]interface{}{"input_path": map[string]interface{}{"type": "string", "description": "Path to the .xlsx file to read"}}, "required": []string{"input_path"}}
}
func (t *XLSXReviewReadTool) Execute(ctx context.Context, args map[string]interface{}) *ToolResult {
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

type XLSXReviewDiffTool struct{ base xlsxReviewToolBase }

func NewXLSXReviewDiffTool(workspace string, restrict bool) *XLSXReviewDiffTool {
	return &XLSXReviewDiffTool{base: newXLSXReviewToolBase(workspace, restrict)}
}

func newXLSXReviewDiffToolWithClient(workspace string, restrict bool, client *xlsxreview.Client) *XLSXReviewDiffTool {
	t := NewXLSXReviewDiffTool(workspace, restrict)
	if client != nil {
		t.base.client = client
	}
	return t
}

func (t *XLSXReviewDiffTool) SetSharedWorkspacePolicy(sharedWorkspace string, sharedWorkspaceReadOnly bool) {
	t.base.SetSharedWorkspacePolicy(sharedWorkspace, sharedWorkspaceReadOnly)
}
func (t *XLSXReviewDiffTool) Name() string { return "xlsx_review_diff" }
func (t *XLSXReviewDiffTool) Description() string {
	return "Diff two .xlsx workbooks semantically and return structured cell, formula, sheet, structure, and metadata differences."
}
func (t *XLSXReviewDiffTool) Parameters() map[string]interface{} {
	return map[string]interface{}{"type": "object", "properties": map[string]interface{}{"old_path": map[string]interface{}{"type": "string", "description": "Path to the original .xlsx file"}, "new_path": map[string]interface{}{"type": "string", "description": "Path to the updated .xlsx file"}}, "required": []string{"old_path", "new_path"}}
}
func (t *XLSXReviewDiffTool) Execute(ctx context.Context, args map[string]interface{}) *ToolResult {
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

type XLSXReviewApplyTool struct{ base xlsxReviewToolBase }

func NewXLSXReviewApplyTool(workspace string, restrict bool) *XLSXReviewApplyTool {
	return &XLSXReviewApplyTool{base: newXLSXReviewToolBase(workspace, restrict)}
}

func newXLSXReviewApplyToolWithClient(workspace string, restrict bool, client *xlsxreview.Client) *XLSXReviewApplyTool {
	t := NewXLSXReviewApplyTool(workspace, restrict)
	if client != nil {
		t.base.client = client
	}
	return t
}

func (t *XLSXReviewApplyTool) SetSharedWorkspacePolicy(sharedWorkspace string, sharedWorkspaceReadOnly bool) {
	t.base.SetSharedWorkspacePolicy(sharedWorkspace, sharedWorkspaceReadOnly)
}
func (t *XLSXReviewApplyTool) Name() string { return "xlsx_review_apply" }
func (t *XLSXReviewApplyTool) Description() string {
	return "Validate or apply an xlsx-review JSON manifest against an existing .xlsx workbook. Prefer dry_run first, then write to a new output path."
}
func (t *XLSXReviewApplyTool) Parameters() map[string]interface{} {
	return map[string]interface{}{"type": "object", "properties": map[string]interface{}{
		"input_path":    map[string]interface{}{"type": "string", "description": "Path to the input .xlsx file"},
		"manifest_path": map[string]interface{}{"type": "string", "description": "Path to an xlsx-review JSON manifest file"},
		"output_path":   map[string]interface{}{"type": "string", "description": "Path for the edited .xlsx output file. Required unless dry_run is true."},
		"dry_run":       map[string]interface{}{"type": "boolean", "description": "Validate the manifest without modifying the workbook"},
		"author":        map[string]interface{}{"type": "string", "description": "Optional comment author override"},
	}, "required": []string{"input_path", "manifest_path"}}
}
func (t *XLSXReviewApplyTool) Execute(ctx context.Context, args map[string]interface{}) *ToolResult {
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
	result, err := t.base.client.Apply(ctx, xlsxreview.ApplyRequest{InputPath: resolvedInputPath, ManifestPath: resolvedManifestPath, OutputPath: resolvedOutputPath, Author: getString(args, "author"), DryRun: dryRun})
	if err != nil {
		return ErrorResult(err.Error()).WithError(err)
	}
	return NewToolResult(mustJSON(result))
}
