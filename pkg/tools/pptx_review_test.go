package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sipeed/picoclaw/pkg/pptxreview"
)

func TestPPTXReviewReadTool_Success(t *testing.T) {
	workspace := t.TempDir()
	inputPath := filepath.Join(workspace, "deck.pptx")
	if err := os.WriteFile(inputPath, []byte("pptx"), 0o644); err != nil {
		t.Fatalf("write pptx: %v", err)
	}

	client := pptxreview.NewClientWithOptions(pptxreview.ClientOptions{
		LookPathFn: func(file string) (string, error) { return "/usr/local/bin/pptx-review", nil },
		RunFn: func(ctx context.Context, binary string, args []string) (pptxreview.RunResult, error) {
			_ = ctx
			_ = binary
			_ = args
			return pptxreview.RunResult{Stdout: `{"slide_count":2,"slides":[{"slide":1,"title":"Intro"}]}`}, nil
		},
	})

	tool := newPPTXReviewReadToolWithClient(workspace, true, client)
	res := tool.Execute(context.Background(), map[string]interface{}{"input_path": "deck.pptx"})
	if res.IsError {
		t.Fatalf("expected success, got error: %s", res.ForLLM)
	}
	if !strings.Contains(res.ForLLM, `"slide_count": 2`) {
		t.Fatalf("expected read result json, got %s", res.ForLLM)
	}
}

func TestPPTXReviewDiffTool_BlocksOutsideWorkspace(t *testing.T) {
	workspace := t.TempDir()
	client := pptxreview.NewClientWithOptions(pptxreview.ClientOptions{
		LookPathFn: func(file string) (string, error) { return "/usr/local/bin/pptx-review", nil },
		RunFn: func(ctx context.Context, binary string, args []string) (pptxreview.RunResult, error) {
			t.Fatalf("runner should not be called when path is invalid")
			return pptxreview.RunResult{}, nil
		},
	})

	tool := newPPTXReviewDiffToolWithClient(workspace, true, client)
	res := tool.Execute(context.Background(), map[string]interface{}{"old_path": filepath.Join("..", "old.pptx"), "new_path": "new.pptx"})
	if !res.IsError {
		t.Fatal("expected error")
	}
	if !strings.Contains(res.ForLLM, "access denied") {
		t.Fatalf("expected access denied, got %s", res.ForLLM)
	}
}

func TestPPTXReviewApplyTool_Success(t *testing.T) {
	workspace := t.TempDir()
	inputPath := filepath.Join(workspace, "deck.pptx")
	manifestPath := filepath.Join(workspace, "manifest.json")
	outputPath := filepath.Join(workspace, "reviewed.pptx")
	if err := os.WriteFile(inputPath, []byte("pptx"), 0o644); err != nil {
		t.Fatalf("write pptx: %v", err)
	}
	if err := os.WriteFile(manifestPath, []byte(`{"changes":[]}`), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	client := pptxreview.NewClientWithOptions(pptxreview.ClientOptions{
		LookPathFn: func(file string) (string, error) { return "/usr/local/bin/pptx-review", nil },
		RunFn: func(ctx context.Context, binary string, args []string) (pptxreview.RunResult, error) {
			_ = ctx
			_ = binary
			_ = args
			if err := os.WriteFile(outputPath, []byte("updated"), 0o644); err != nil {
				t.Fatalf("write output: %v", err)
			}
			return pptxreview.RunResult{Stdout: fmt.Sprintf(`{"input":%q,"output":%q,"author":"Reviewer","changes_attempted":1,"changes_succeeded":1,"comments_attempted":0,"comments_succeeded":0,"results":[{"index":0,"type":"replace_text","success":true,"message":"Applied"}],"success":true}`, inputPath, outputPath)}, nil
		},
	})

	tool := newPPTXReviewApplyToolWithClient(workspace, true, client)
	res := tool.Execute(context.Background(), map[string]interface{}{
		"input_path":    "deck.pptx",
		"manifest_path": "manifest.json",
		"output_path":   "reviewed.pptx",
	})
	if res.IsError {
		t.Fatalf("expected success, got error: %s", res.ForLLM)
	}
	if !strings.Contains(res.ForLLM, `"status": "ok"`) || !strings.Contains(res.ForLLM, `"outputWritten": true`) {
		t.Fatalf("expected apply result json, got %s", res.ForLLM)
	}
}

func TestPPTXReviewApplyTool_PartialDoesNotRaiseToolError(t *testing.T) {
	workspace := t.TempDir()
	inputPath := filepath.Join(workspace, "deck.pptx")
	manifestPath := filepath.Join(workspace, "manifest.json")
	outputPath := filepath.Join(workspace, "reviewed.pptx")
	if err := os.WriteFile(inputPath, []byte("pptx"), 0o644); err != nil {
		t.Fatalf("write pptx: %v", err)
	}
	if err := os.WriteFile(manifestPath, []byte(`{"changes":[]}`), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	client := pptxreview.NewClientWithOptions(pptxreview.ClientOptions{
		LookPathFn: func(file string) (string, error) { return "/usr/local/bin/pptx-review", nil },
		RunFn: func(ctx context.Context, binary string, args []string) (pptxreview.RunResult, error) {
			_ = ctx
			_ = binary
			_ = args
			if err := os.WriteFile(outputPath, []byte("updated"), 0o644); err != nil {
				t.Fatalf("write output: %v", err)
			}
			return pptxreview.RunResult{Stdout: fmt.Sprintf(`{"input":%q,"output":%q,"author":"Reviewer","changes_attempted":2,"changes_succeeded":1,"comments_attempted":0,"comments_succeeded":0,"results":[{"index":0,"type":"replace_text","success":true,"message":"Applied"},{"index":1,"type":"set_notes","success":false,"message":"Missing slide"}],"success":false}`, inputPath, outputPath), ExitCode: 1}, fmt.Errorf("exit status 1")
		},
	})

	tool := newPPTXReviewApplyToolWithClient(workspace, true, client)
	res := tool.Execute(context.Background(), map[string]interface{}{
		"input_path":    "deck.pptx",
		"manifest_path": "manifest.json",
		"output_path":   "reviewed.pptx",
	})
	if res.IsError {
		t.Fatalf("expected partial structured result, got error: %s", res.ForLLM)
	}
	if !strings.Contains(res.ForLLM, `"status": "partial"`) {
		t.Fatalf("expected partial status, got %s", res.ForLLM)
	}
}

func TestPPTXReviewApplyTool_DryRunAllowsMissingOutputPath(t *testing.T) {
	workspace := t.TempDir()
	inputPath := filepath.Join(workspace, "deck.pptx")
	manifestPath := filepath.Join(workspace, "manifest.json")
	if err := os.WriteFile(inputPath, []byte("pptx"), 0o644); err != nil {
		t.Fatalf("write pptx: %v", err)
	}
	if err := os.WriteFile(manifestPath, []byte(`{"changes":[]}`), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	client := pptxreview.NewClientWithOptions(pptxreview.ClientOptions{
		LookPathFn: func(file string) (string, error) { return "/usr/local/bin/pptx-review", nil },
		RunFn: func(ctx context.Context, binary string, args []string) (pptxreview.RunResult, error) {
			_ = ctx
			_ = binary
			_ = args
			return pptxreview.RunResult{Stdout: fmt.Sprintf(`{"input":%q,"author":"Reviewer","changes_attempted":1,"changes_succeeded":1,"comments_attempted":0,"comments_succeeded":0,"results":[{"index":0,"type":"replace_text","success":true,"message":"Validated"}],"success":true}`, inputPath)}, nil
		},
	})

	tool := newPPTXReviewApplyToolWithClient(workspace, true, client)
	res := tool.Execute(context.Background(), map[string]interface{}{
		"input_path":    "deck.pptx",
		"manifest_path": "manifest.json",
		"dry_run":       true,
		"author":        "Reviewer",
	})
	if res.IsError {
		t.Fatalf("expected success, got error: %s", res.ForLLM)
	}
	if !strings.Contains(res.ForLLM, `"dryRun": true`) {
		t.Fatalf("expected dry-run metadata, got %s", res.ForLLM)
	}
}
