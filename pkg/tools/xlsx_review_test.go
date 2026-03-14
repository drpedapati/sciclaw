package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sipeed/picoclaw/pkg/xlsxreview"
)

func TestXLSXReviewReadTool_Success(t *testing.T) {
	workspace := t.TempDir()
	inputPath := filepath.Join(workspace, "sheet.xlsx")
	if err := os.WriteFile(inputPath, []byte("xlsx"), 0o644); err != nil {
		t.Fatalf("write xlsx: %v", err)
	}

	client := xlsxreview.NewClientWithOptions(xlsxreview.ClientOptions{
		LookPathFn: func(file string) (string, error) { return "/usr/local/bin/xlsx-review", nil },
		RunFn: func(ctx context.Context, binary string, args []string) (xlsxreview.RunResult, error) {
			_ = ctx
			_ = binary
			_ = args
			return xlsxreview.RunResult{Stdout: `{"workbook":{"sheet_count":1},"sheets":[{"name":"Data"}],"warnings":[]}`}, nil
		},
	})

	tool := newXLSXReviewReadToolWithClient(workspace, true, client)
	res := tool.Execute(context.Background(), map[string]interface{}{"input_path": "sheet.xlsx"})
	if res.IsError {
		t.Fatalf("expected success, got error: %s", res.ForLLM)
	}
	if !strings.Contains(res.ForLLM, `"sheet_count": 1`) {
		t.Fatalf("expected read result json, got %s", res.ForLLM)
	}
}

func TestXLSXReviewDiffTool_BlocksOutsideWorkspace(t *testing.T) {
	workspace := t.TempDir()
	client := xlsxreview.NewClientWithOptions(xlsxreview.ClientOptions{
		LookPathFn: func(file string) (string, error) { return "/usr/local/bin/xlsx-review", nil },
		RunFn: func(ctx context.Context, binary string, args []string) (xlsxreview.RunResult, error) {
			t.Fatalf("runner should not be called when path is invalid")
			return xlsxreview.RunResult{}, nil
		},
	})

	tool := newXLSXReviewDiffToolWithClient(workspace, true, client)
	res := tool.Execute(context.Background(), map[string]interface{}{"old_path": filepath.Join("..", "old.xlsx"), "new_path": "new.xlsx"})
	if !res.IsError {
		t.Fatal("expected error")
	}
	if !strings.Contains(res.ForLLM, "access denied") {
		t.Fatalf("expected access denied, got %s", res.ForLLM)
	}
}

func TestXLSXReviewApplyTool_Success(t *testing.T) {
	workspace := t.TempDir()
	inputPath := filepath.Join(workspace, "sheet.xlsx")
	manifestPath := filepath.Join(workspace, "manifest.json")
	outputPath := filepath.Join(workspace, "reviewed.xlsx")
	if err := os.WriteFile(inputPath, []byte("xlsx"), 0o644); err != nil {
		t.Fatalf("write xlsx: %v", err)
	}
	if err := os.WriteFile(manifestPath, []byte(`{"changes":[]}`), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	client := xlsxreview.NewClientWithOptions(xlsxreview.ClientOptions{
		LookPathFn: func(file string) (string, error) { return "/usr/local/bin/xlsx-review", nil },
		RunFn: func(ctx context.Context, binary string, args []string) (xlsxreview.RunResult, error) {
			_ = ctx
			_ = binary
			_ = args
			if err := os.WriteFile(outputPath, []byte("updated"), 0o644); err != nil {
				t.Fatalf("write output: %v", err)
			}
			return xlsxreview.RunResult{Stdout: fmt.Sprintf(`{"input":%q,"output":%q,"author":"Reviewer","changes_attempted":1,"changes_succeeded":1,"comments_attempted":0,"comments_succeeded":0,"results":[{"index":0,"type":"set_cell","success":true,"message":"Applied"}],"success":true}`, inputPath, outputPath)}, nil
		},
	})

	tool := newXLSXReviewApplyToolWithClient(workspace, true, client)
	res := tool.Execute(context.Background(), map[string]interface{}{
		"input_path":    "sheet.xlsx",
		"manifest_path": "manifest.json",
		"output_path":   "reviewed.xlsx",
	})
	if res.IsError {
		t.Fatalf("expected success, got error: %s", res.ForLLM)
	}
	if !strings.Contains(res.ForLLM, `"status": "ok"`) || !strings.Contains(res.ForLLM, `"outputWritten": true`) {
		t.Fatalf("expected apply result json, got %s", res.ForLLM)
	}
}

func TestXLSXReviewApplyTool_PartialDoesNotRaiseToolError(t *testing.T) {
	workspace := t.TempDir()
	inputPath := filepath.Join(workspace, "sheet.xlsx")
	manifestPath := filepath.Join(workspace, "manifest.json")
	outputPath := filepath.Join(workspace, "reviewed.xlsx")
	if err := os.WriteFile(inputPath, []byte("xlsx"), 0o644); err != nil {
		t.Fatalf("write xlsx: %v", err)
	}
	if err := os.WriteFile(manifestPath, []byte(`{"changes":[]}`), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	client := xlsxreview.NewClientWithOptions(xlsxreview.ClientOptions{
		LookPathFn: func(file string) (string, error) { return "/usr/local/bin/xlsx-review", nil },
		RunFn: func(ctx context.Context, binary string, args []string) (xlsxreview.RunResult, error) {
			_ = ctx
			_ = binary
			_ = args
			if err := os.WriteFile(outputPath, []byte("updated"), 0o644); err != nil {
				t.Fatalf("write output: %v", err)
			}
			return xlsxreview.RunResult{Stdout: fmt.Sprintf(`{"input":%q,"output":%q,"author":"Reviewer","changes_attempted":2,"changes_succeeded":1,"comments_attempted":0,"comments_succeeded":0,"results":[{"index":0,"type":"set_cell","success":true,"message":"Applied"},{"index":1,"type":"set_formula","success":false,"message":"Missing sheet"}],"success":false}`, inputPath, outputPath), ExitCode: 1}, fmt.Errorf("exit status 1")
		},
	})

	tool := newXLSXReviewApplyToolWithClient(workspace, true, client)
	res := tool.Execute(context.Background(), map[string]interface{}{
		"input_path":    "sheet.xlsx",
		"manifest_path": "manifest.json",
		"output_path":   "reviewed.xlsx",
	})
	if res.IsError {
		t.Fatalf("expected partial structured result, got error: %s", res.ForLLM)
	}
	if !strings.Contains(res.ForLLM, `"status": "partial"`) {
		t.Fatalf("expected partial status, got %s", res.ForLLM)
	}
}

func TestXLSXReviewApplyTool_DryRunAllowsMissingOutputPath(t *testing.T) {
	workspace := t.TempDir()
	inputPath := filepath.Join(workspace, "sheet.xlsx")
	manifestPath := filepath.Join(workspace, "manifest.json")
	if err := os.WriteFile(inputPath, []byte("xlsx"), 0o644); err != nil {
		t.Fatalf("write xlsx: %v", err)
	}
	if err := os.WriteFile(manifestPath, []byte(`{"changes":[]}`), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	client := xlsxreview.NewClientWithOptions(xlsxreview.ClientOptions{
		LookPathFn: func(file string) (string, error) { return "/usr/local/bin/xlsx-review", nil },
		RunFn: func(ctx context.Context, binary string, args []string) (xlsxreview.RunResult, error) {
			_ = ctx
			_ = binary
			_ = args
			return xlsxreview.RunResult{Stdout: fmt.Sprintf(`{"input":%q,"author":"Reviewer","changes_attempted":1,"changes_succeeded":1,"comments_attempted":0,"comments_succeeded":0,"results":[{"index":0,"type":"set_cell","success":true,"message":"Validated"}],"success":true}`, inputPath)}, nil
		},
	})

	tool := newXLSXReviewApplyToolWithClient(workspace, true, client)
	res := tool.Execute(context.Background(), map[string]interface{}{
		"input_path":    "sheet.xlsx",
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
