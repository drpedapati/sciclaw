package tools

import (
	"context"
	"strings"

	"github.com/sipeed/picoclaw/pkg/pptxreview"
)

type pptxReviewToolBase struct {
	workspace               string
	restrict                bool
	sharedWorkspace         string
	sharedWorkspaceReadOnly bool
	client                  *pptxreview.Client
}

func newPPTXReviewToolBase(workspace string, restrict bool) pptxReviewToolBase {
	return pptxReviewToolBase{workspace: workspace, restrict: restrict, client: pptxreview.NewClient()}
}

func (b *pptxReviewToolBase) SetSharedWorkspacePolicy(sharedWorkspace string, sharedWorkspaceReadOnly bool) {
	b.sharedWorkspace = strings.TrimSpace(sharedWorkspace)
	b.sharedWorkspaceReadOnly = sharedWorkspaceReadOnly
}
func (b *pptxReviewToolBase) resolveReadPath(path string) (string, error) {
	return validatePathWithPolicy(path, b.workspace, b.restrict, AccessRead, b.sharedWorkspace, b.sharedWorkspaceReadOnly)
}
func (b *pptxReviewToolBase) resolveWritePath(path string) (string, error) {
	return validatePathWithPolicy(path, b.workspace, b.restrict, AccessWrite, b.sharedWorkspace, b.sharedWorkspaceReadOnly)
}

type PPTXReviewReadTool struct{ base pptxReviewToolBase }

func NewPPTXReviewReadTool(workspace string, restrict bool) *PPTXReviewReadTool {
	return &PPTXReviewReadTool{base: newPPTXReviewToolBase(workspace, restrict)}
}
func newPPTXReviewReadToolWithClient(workspace string, restrict bool, client *pptxreview.Client) *PPTXReviewReadTool {
	t := NewPPTXReviewReadTool(workspace, restrict)
	if client != nil {
		t.base.client = client
	}
	return t
}
func (t *PPTXReviewReadTool) SetSharedWorkspacePolicy(sharedWorkspace string, sharedWorkspaceReadOnly bool) {
	t.base.SetSharedWorkspacePolicy(sharedWorkspace, sharedWorkspaceReadOnly)
}
func (t *PPTXReviewReadTool) Name() string { return "pptx_review_read" }
func (t *PPTXReviewReadTool) Description() string {
	return "Read an existing .pptx presentation as structured JSON, including slide content."
}
func (t *PPTXReviewReadTool) Parameters() map[string]interface{} {
	return map[string]interface{}{"type": "object", "properties": map[string]interface{}{"input_path": map[string]interface{}{"type": "string", "description": "Path to the .pptx file to read"}}, "required": []string{"input_path"}}
}
func (t *PPTXReviewReadTool) Execute(ctx context.Context, args map[string]interface{}) *ToolResult {
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

type PPTXReviewDiffTool struct{ base pptxReviewToolBase }

func NewPPTXReviewDiffTool(workspace string, restrict bool) *PPTXReviewDiffTool {
	return &PPTXReviewDiffTool{base: newPPTXReviewToolBase(workspace, restrict)}
}
func newPPTXReviewDiffToolWithClient(workspace string, restrict bool, client *pptxreview.Client) *PPTXReviewDiffTool {
	t := NewPPTXReviewDiffTool(workspace, restrict)
	if client != nil {
		t.base.client = client
	}
	return t
}
func (t *PPTXReviewDiffTool) SetSharedWorkspacePolicy(sharedWorkspace string, sharedWorkspaceReadOnly bool) {
	t.base.SetSharedWorkspacePolicy(sharedWorkspace, sharedWorkspaceReadOnly)
}
func (t *PPTXReviewDiffTool) Name() string { return "pptx_review_diff" }
func (t *PPTXReviewDiffTool) Description() string {
	return "Diff two .pptx presentations semantically and return structured slide, shape, note, and metadata differences."
}
func (t *PPTXReviewDiffTool) Parameters() map[string]interface{} {
	return map[string]interface{}{"type": "object", "properties": map[string]interface{}{"old_path": map[string]interface{}{"type": "string", "description": "Path to the original .pptx file"}, "new_path": map[string]interface{}{"type": "string", "description": "Path to the updated .pptx file"}}, "required": []string{"old_path", "new_path"}}
}
func (t *PPTXReviewDiffTool) Execute(ctx context.Context, args map[string]interface{}) *ToolResult {
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

type PPTXReviewApplyTool struct{ base pptxReviewToolBase }

func NewPPTXReviewApplyTool(workspace string, restrict bool) *PPTXReviewApplyTool {
	return &PPTXReviewApplyTool{base: newPPTXReviewToolBase(workspace, restrict)}
}
func newPPTXReviewApplyToolWithClient(workspace string, restrict bool, client *pptxreview.Client) *PPTXReviewApplyTool {
	t := NewPPTXReviewApplyTool(workspace, restrict)
	if client != nil {
		t.base.client = client
	}
	return t
}
func (t *PPTXReviewApplyTool) SetSharedWorkspacePolicy(sharedWorkspace string, sharedWorkspaceReadOnly bool) {
	t.base.SetSharedWorkspacePolicy(sharedWorkspace, sharedWorkspaceReadOnly)
}
func (t *PPTXReviewApplyTool) Name() string { return "pptx_review_apply" }
func (t *PPTXReviewApplyTool) Description() string {
	return "Validate or apply a pptx-review JSON manifest against an existing .pptx presentation. Prefer dry_run first, then write to a new output path."
}
func (t *PPTXReviewApplyTool) Parameters() map[string]interface{} {
	return map[string]interface{}{"type": "object", "properties": map[string]interface{}{
		"input_path":    map[string]interface{}{"type": "string", "description": "Path to the input .pptx file"},
		"manifest_path": map[string]interface{}{"type": "string", "description": "Path to a pptx-review JSON manifest file"},
		"output_path":   map[string]interface{}{"type": "string", "description": "Path for the edited .pptx output file. Required unless dry_run is true."},
		"dry_run":       map[string]interface{}{"type": "boolean", "description": "Validate the manifest without modifying the presentation"},
		"author":        map[string]interface{}{"type": "string", "description": "Optional comment author override"},
	}, "required": []string{"input_path", "manifest_path"}}
}
func (t *PPTXReviewApplyTool) Execute(ctx context.Context, args map[string]interface{}) *ToolResult {
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
	result, err := t.base.client.Apply(ctx, pptxreview.ApplyRequest{InputPath: resolvedInputPath, ManifestPath: resolvedManifestPath, OutputPath: resolvedOutputPath, Author: getString(args, "author"), DryRun: dryRun})
	if err != nil {
		return ErrorResult(err.Error()).WithError(err)
	}
	return NewToolResult(mustJSON(result))
}
