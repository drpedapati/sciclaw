package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sipeed/picoclaw/pkg/docxreview"
)

func TestDOCXReviewReadTool_Success(t *testing.T) {
	workspace := t.TempDir()
	inputPath := filepath.Join(workspace, "paper.docx")
	if err := os.WriteFile(inputPath, []byte("docx"), 0o644); err != nil {
		t.Fatalf("write docx: %v", err)
	}

	client := docxreview.NewClientWithOptions(docxreview.ClientOptions{
		LookPathFn: func(file string) (string, error) { return "/usr/local/bin/docx-review", nil },
		RunFn: func(ctx context.Context, binary string, args []string) (docxreview.RunResult, error) {
			_ = ctx
			_ = binary
			_ = args
			return docxreview.RunResult{Stdout: fmt.Sprintf(`{"file":%q,"paragraphs":[],"comments":[],"metadata":{"word_count":12,"paragraph_count":2},"summary":{"total_tracked_changes":0,"insertions":0,"deletions":0,"total_comments":0,"change_authors":[],"comment_authors":[]}}`, inputPath)}, nil
		},
	})

	tool := newDOCXReviewReadToolWithClient(workspace, true, client)
	res := tool.Execute(context.Background(), map[string]interface{}{"input_path": "paper.docx"})
	if res.IsError {
		t.Fatalf("expected success, got error: %s", res.ForLLM)
	}
	if !strings.Contains(res.ForLLM, `"word_count": 12`) {
		t.Fatalf("expected read result json, got %s", res.ForLLM)
	}
}

func TestDOCXReviewDiffTool_BlocksOutsideWorkspace(t *testing.T) {
	workspace := t.TempDir()
	client := docxreview.NewClientWithOptions(docxreview.ClientOptions{
		LookPathFn: func(file string) (string, error) { return "/usr/local/bin/docx-review", nil },
		RunFn: func(ctx context.Context, binary string, args []string) (docxreview.RunResult, error) {
			t.Fatalf("runner should not be called when path is invalid")
			return docxreview.RunResult{}, nil
		},
	})

	tool := newDOCXReviewDiffToolWithClient(workspace, true, client)
	res := tool.Execute(context.Background(), map[string]interface{}{"old_path": filepath.Join("..", "old.docx"), "new_path": "new.docx"})
	if !res.IsError {
		t.Fatal("expected error")
	}
	if !strings.Contains(res.ForLLM, "access denied") {
		t.Fatalf("expected access denied, got %s", res.ForLLM)
	}
}

func TestDOCXReviewApplyTool_Success(t *testing.T) {
	workspace := t.TempDir()
	inputPath := filepath.Join(workspace, "paper.docx")
	manifestPath := filepath.Join(workspace, "manifest.json")
	outputPath := filepath.Join(workspace, "reviewed.docx")
	if err := os.WriteFile(inputPath, []byte("docx"), 0o644); err != nil {
		t.Fatalf("write docx: %v", err)
	}
	if err := os.WriteFile(manifestPath, []byte(`{"changes":[]}`), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	client := docxreview.NewClientWithOptions(docxreview.ClientOptions{
		LookPathFn: func(file string) (string, error) { return "/usr/local/bin/docx-review", nil },
		RunFn: func(ctx context.Context, binary string, args []string) (docxreview.RunResult, error) {
			_ = ctx
			_ = binary
			_ = args
			if err := os.WriteFile(outputPath, []byte("updated"), 0o644); err != nil {
				t.Fatalf("write output: %v", err)
			}
			return docxreview.RunResult{Stdout: fmt.Sprintf(`{"input":%q,"output":%q,"author":"Reviewer","changes_attempted":1,"changes_succeeded":1,"comments_attempted":0,"comments_succeeded":0,"results":[{"index":0,"type":"replace","success":true,"message":"Applied"}],"success":true}`, inputPath, outputPath)}, nil
		},
	})

	tool := newDOCXReviewApplyToolWithClient(workspace, true, client)
	res := tool.Execute(context.Background(), map[string]interface{}{
		"input_path":    "paper.docx",
		"manifest_path": "manifest.json",
		"output_path":   "reviewed.docx",
	})
	if res.IsError {
		t.Fatalf("expected success, got error: %s", res.ForLLM)
	}
	if !strings.Contains(res.ForLLM, `"status": "ok"`) || !strings.Contains(res.ForLLM, `"outputWritten": true`) {
		t.Fatalf("expected apply result json, got %s", res.ForLLM)
	}
}

func TestDOCXReviewApplyTool_PartialDoesNotRaiseToolError(t *testing.T) {
	workspace := t.TempDir()
	inputPath := filepath.Join(workspace, "paper.docx")
	manifestPath := filepath.Join(workspace, "manifest.json")
	outputPath := filepath.Join(workspace, "reviewed.docx")
	if err := os.WriteFile(inputPath, []byte("docx"), 0o644); err != nil {
		t.Fatalf("write docx: %v", err)
	}
	if err := os.WriteFile(manifestPath, []byte(`{"changes":[]}`), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	client := docxreview.NewClientWithOptions(docxreview.ClientOptions{
		LookPathFn: func(file string) (string, error) { return "/usr/local/bin/docx-review", nil },
		RunFn: func(ctx context.Context, binary string, args []string) (docxreview.RunResult, error) {
			_ = ctx
			_ = binary
			_ = args
			if err := os.WriteFile(outputPath, []byte("updated"), 0o644); err != nil {
				t.Fatalf("write output: %v", err)
			}
			return docxreview.RunResult{Stdout: fmt.Sprintf(`{"input":%q,"output":%q,"author":"Reviewer","changes_attempted":2,"changes_succeeded":1,"comments_attempted":0,"comments_succeeded":0,"results":[{"index":0,"type":"replace","success":true,"message":"Applied"},{"index":1,"type":"replace","success":false,"message":"Not found"}],"success":false}`, inputPath, outputPath), ExitCode: 1}, fmt.Errorf("exit status 1")
		},
	})

	tool := newDOCXReviewApplyToolWithClient(workspace, true, client)
	res := tool.Execute(context.Background(), map[string]interface{}{
		"input_path":    "paper.docx",
		"manifest_path": "manifest.json",
		"output_path":   "reviewed.docx",
	})
	if res.IsError {
		t.Fatalf("expected partial structured result, got error: %s", res.ForLLM)
	}
	if !strings.Contains(res.ForLLM, `"status": "partial"`) {
		t.Fatalf("expected partial status, got %s", res.ForLLM)
	}
}

func TestDOCXReviewApplyTool_DryRunAllowsMissingOutputPath(t *testing.T) {
	workspace := t.TempDir()
	inputPath := filepath.Join(workspace, "paper.docx")
	manifestPath := filepath.Join(workspace, "manifest.json")
	if err := os.WriteFile(inputPath, []byte("docx"), 0o644); err != nil {
		t.Fatalf("write docx: %v", err)
	}
	if err := os.WriteFile(manifestPath, []byte(`{"changes":[]}`), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	client := docxreview.NewClientWithOptions(docxreview.ClientOptions{
		LookPathFn: func(file string) (string, error) { return "/usr/local/bin/docx-review", nil },
		RunFn: func(ctx context.Context, binary string, args []string) (docxreview.RunResult, error) {
			_ = ctx
			_ = binary
			_ = args
			return docxreview.RunResult{Stdout: fmt.Sprintf(`{"input":%q,"author":"Reviewer","changes_attempted":1,"changes_succeeded":1,"comments_attempted":0,"comments_succeeded":0,"results":[{"index":0,"type":"replace","success":true,"message":"Validated"}],"success":true}`, inputPath)}, nil
		},
	})

	tool := newDOCXReviewApplyToolWithClient(workspace, true, client)
	res := tool.Execute(context.Background(), map[string]interface{}{
		"input_path":      "paper.docx",
		"manifest_path":   "manifest.json",
		"dry_run":         true,
		"accept_existing": true,
	})
	if res.IsError {
		t.Fatalf("expected success, got error: %s", res.ForLLM)
	}
	if !strings.Contains(res.ForLLM, `"dryRun": true`) || !strings.Contains(res.ForLLM, `"acceptExisting": true`) {
		t.Fatalf("expected dry-run metadata, got %s", res.ForLLM)
	}
}
