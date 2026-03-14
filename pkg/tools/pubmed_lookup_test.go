package tools

import (
	"context"
	"strings"
	"testing"
)

func TestPubMedSearchTool_Success(t *testing.T) {
	workspace := t.TempDir()
	var gotArgs []string
	var gotEnv map[string]string

	tool := newPubMedSearchToolWithRunner(workspace, func(ctx context.Context, binary string, args []string, cwd string, env map[string]string) (string, string, error) {
		_ = ctx
		if binary != "/usr/bin/pubmed" {
			t.Fatalf("unexpected binary: %s", binary)
		}
		if cwd != workspace {
			t.Fatalf("expected cwd %q, got %q", workspace, cwd)
		}
		gotArgs = append([]string{}, args...)
		gotEnv = env
		return `{"count":1,"ids":["24001701"]}`, "", nil
	})
	tool.base.findBin = func() (string, error) { return "/usr/bin/pubmed", nil }
	tool.SetExtraEnv(map[string]string{"NCBI_API_KEY": "test-key"})

	res := tool.Execute(context.Background(), map[string]interface{}{
		"query": "Jeon 2013 detecting",
		"limit": 5,
		"sort":  "date",
		"year":  "2013-2014",
	})

	if res.IsError {
		t.Fatalf("expected success, got error: %s", res.ForLLM)
	}
	if !strings.Contains(res.ForLLM, `"ids":["24001701"]`) {
		t.Fatalf("expected JSON output, got: %s", res.ForLLM)
	}
	wantArgs := []string{"search", "Jeon 2013 detecting", "--json", "--limit", "5", "--sort", "date", "--year", "2013-2014"}
	if strings.Join(gotArgs, "|") != strings.Join(wantArgs, "|") {
		t.Fatalf("unexpected args: %v", gotArgs)
	}
	if gotEnv["NCBI_API_KEY"] != "test-key" {
		t.Fatalf("expected API key env, got: %v", gotEnv)
	}
}

func TestPubMedSearchTool_Validation(t *testing.T) {
	workspace := t.TempDir()
	tool := newPubMedSearchToolWithRunner(workspace, func(ctx context.Context, binary string, args []string, cwd string, env map[string]string) (string, string, error) {
		t.Fatalf("runner should not be called on validation error")
		return "", "", nil
	})
	tool.base.findBin = func() (string, error) { return "/usr/bin/pubmed", nil }

	cases := []map[string]interface{}{
		{"query": "", "limit": 5},
		{"query": "test", "limit": 0},
		{"query": "test", "sort": "bad"},
		{"query": "test", "year": "2025-2024"},
	}
	for _, args := range cases {
		res := tool.Execute(context.Background(), args)
		if !res.IsError {
			t.Fatalf("expected validation error for args %#v", args)
		}
	}
}

func TestPubMedFetchTool_Success(t *testing.T) {
	workspace := t.TempDir()
	var gotArgs []string

	tool := newPubMedFetchToolWithRunner(workspace, func(ctx context.Context, binary string, args []string, cwd string, env map[string]string) (string, string, error) {
		_ = ctx
		gotArgs = append([]string{}, args...)
		return `[{"pmid":"24001701","title":"Detecting..."}]`, "", nil
	})
	tool.base.findBin = func() (string, error) { return "/usr/bin/pubmed", nil }

	res := tool.Execute(context.Background(), map[string]interface{}{
		"pmids": []interface{}{"24001701", "24229314"},
	})

	if res.IsError {
		t.Fatalf("expected success, got error: %s", res.ForLLM)
	}
	if !strings.Contains(res.ForLLM, `"pmid":"24001701"`) {
		t.Fatalf("expected JSON output, got: %s", res.ForLLM)
	}
	wantArgs := []string{"fetch", "24001701", "24229314", "--json"}
	if strings.Join(gotArgs, "|") != strings.Join(wantArgs, "|") {
		t.Fatalf("unexpected args: %v", gotArgs)
	}
}

func TestPubMedFetchTool_RejectsNonDigitPMIDs(t *testing.T) {
	workspace := t.TempDir()
	tool := newPubMedFetchToolWithRunner(workspace, func(ctx context.Context, binary string, args []string, cwd string, env map[string]string) (string, string, error) {
		t.Fatalf("runner should not be called on invalid PMID")
		return "", "", nil
	})
	tool.base.findBin = func() (string, error) { return "/usr/bin/pubmed", nil }

	res := tool.Execute(context.Background(), map[string]interface{}{
		"pmids": []interface{}{"24001701", "bad"},
	})
	if !res.IsError {
		t.Fatal("expected PMID validation error")
	}
}
