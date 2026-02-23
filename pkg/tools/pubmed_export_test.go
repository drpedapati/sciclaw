package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPubMedExportTool_Success(t *testing.T) {
	workspace := t.TempDir()
	var gotBinary string
	var gotArgs []string

	tool := newPubMedExportToolWithRunner(workspace, true, func(ctx context.Context, binary string, args []string, cwd string, env map[string]string) (string, string, error) {
		_ = ctx
		gotBinary = binary
		gotArgs = append([]string{}, args...)
		if cwd != workspace {
			t.Fatalf("expected cwd %q, got %q", workspace, cwd)
		}
		out := args[len(args)-1]
		if err := os.WriteFile(out, []byte("TY  - JOUR\nER  - \n"), 0o644); err != nil {
			t.Fatalf("failed to write fake ris output: %v", err)
		}
		return "", "", nil
	})
	tool.findBin = func() (string, error) { return "/usr/bin/pubmed", nil }

	tool.SetExtraEnv(map[string]string{"NCBI_API_KEY": "test"})

	res := tool.Execute(context.Background(), map[string]interface{}{
		"pmids":       []interface{}{"38000001", "38000002"},
		"output_file": "exports/citations.ris",
	})

	if res.IsError {
		t.Fatalf("expected success, got error: %s", res.ForLLM)
	}
	if !strings.Contains(res.ForLLM, `"status": "ok"`) {
		t.Fatalf("expected json success result, got: %s", res.ForLLM)
	}
	if len(gotArgs) < 4 {
		t.Fatalf("unexpected args: %v", gotArgs)
	}
	if gotArgs[0] != "fetch" || gotArgs[len(gotArgs)-2] != "--ris" {
		t.Fatalf("expected fetch --ris args, got %v", gotArgs)
	}
	if strings.TrimSpace(gotBinary) == "" {
		t.Fatalf("expected binary to be set")
	}
}

func TestPubMedExportTool_BlocksOutsideWorkspace(t *testing.T) {
	workspace := t.TempDir()
	tool := newPubMedExportToolWithRunner(workspace, true, func(ctx context.Context, binary string, args []string, cwd string, env map[string]string) (string, string, error) {
		t.Fatalf("runner should not be called when path is invalid")
		return "", "", nil
	})
	tool.findBin = func() (string, error) { return "/usr/bin/pubmed", nil }

	res := tool.Execute(context.Background(), map[string]interface{}{
		"pmids":       []interface{}{"38000001"},
		"output_file": filepath.Join("..", "outside.ris"),
	})

	if !res.IsError {
		t.Fatalf("expected error for outside workspace path")
	}
}

func TestPubMedExportTool_BlocksSharedWorkspaceWriteWhenReadOnly(t *testing.T) {
	workspace := t.TempDir()
	shared := t.TempDir()

	tool := newPubMedExportToolWithRunner(workspace, true, func(ctx context.Context, binary string, args []string, cwd string, env map[string]string) (string, string, error) {
		t.Fatalf("runner should not be called when shared workspace is read-only")
		return "", "", nil
	})
	tool.SetSharedWorkspacePolicy(shared, true)
	tool.findBin = func() (string, error) { return "/usr/bin/pubmed", nil }

	res := tool.Execute(context.Background(), map[string]interface{}{
		"pmids":       []interface{}{"38000001"},
		"output_file": filepath.Join(shared, "citations.ris"),
	})

	if !res.IsError {
		t.Fatal("expected write to read-only shared workspace to be blocked")
	}
	if !strings.Contains(res.ForLLM, "read-only") {
		t.Fatalf("expected read-only error, got: %s", res.ForLLM)
	}
}

func TestParsePMIDList_StringInput(t *testing.T) {
	pmids, err := parsePMIDList("38000001, 38000002 38000003")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pmids) != 3 {
		t.Fatalf("expected 3 pmids, got %d", len(pmids))
	}
}
