package irl

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestClientDiscoverProjectsAndWorkspaceContext(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script fixture is unix-only")
	}

	workspace := t.TempDir()
	binDir := t.TempDir()
	if err := writeFakeIRLBinary(filepath.Join(binDir, "irl"), false); err != nil {
		t.Fatalf("write fake irl binary: %v", err)
	}

	origPath := os.Getenv("PATH")
	t.Cleanup(func() { _ = os.Setenv("PATH", origPath) })
	if err := os.Setenv("PATH", binDir+string(os.PathListSeparator)+origPath); err != nil {
		t.Fatalf("set PATH: %v", err)
	}

	client := NewClientWithOptions(workspace, ClientOptions{
		NowFn: func() time.Time {
			return time.Date(2026, time.February, 15, 9, 0, 0, 0, time.UTC)
		},
	})

	ctx := context.Background()
	projectsResult, err := client.DiscoverProjects(ctx)
	if err != nil {
		t.Fatalf("DiscoverProjects returned error: %v", err)
	}
	if projectsResult.Status != StatusSuccess {
		t.Fatalf("expected discover status success, got %s", projectsResult.Status)
	}
	if len(projectsResult.Commands) != 1 {
		t.Fatalf("expected one discover command record, got %d", len(projectsResult.Commands))
	}
	if projectsResult.Commands[0].StorePath == "" {
		t.Fatalf("expected discover command store path")
	}
	if _, err := os.Stat(projectsResult.Commands[0].StorePath); err != nil {
		t.Fatalf("discover store path missing: %v", err)
	}

	ctxResult, err := client.GetWorkspaceContext(ctx)
	if err != nil {
		t.Fatalf("GetWorkspaceContext returned error: %v", err)
	}
	if ctxResult.Status != StatusSuccess {
		t.Fatalf("expected workspace context success, got %s", ctxResult.Status)
	}
	if len(ctxResult.Commands) != 2 {
		t.Fatalf("expected two context command records, got %d", len(ctxResult.Commands))
	}
}

func TestClientJSONRetryOnParseFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script fixture is unix-only")
	}

	workspace := t.TempDir()
	binDir := t.TempDir()
	if err := writeFakeIRLBinary(filepath.Join(binDir, "irl"), true); err != nil {
		t.Fatalf("write fake irl binary: %v", err)
	}

	origPath := os.Getenv("PATH")
	t.Cleanup(func() { _ = os.Setenv("PATH", origPath) })
	if err := os.Setenv("PATH", binDir+string(os.PathListSeparator)+origPath); err != nil {
		t.Fatalf("set PATH: %v", err)
	}

	client := NewClientWithOptions(workspace, ClientOptions{
		NowFn: func() time.Time {
			return time.Date(2026, time.February, 15, 10, 0, 0, 0, time.UTC)
		},
	})

	res, err := client.DiscoverProjects(context.Background())
	if err != nil {
		t.Fatalf("DiscoverProjects returned error after retry: %v", err)
	}
	if res.Status != StatusPartial {
		t.Fatalf("expected partial status (first parse failure recorded), got %s", res.Status)
	}
	if len(res.Commands) != 2 {
		t.Fatalf("expected 2 attempts, got %d", len(res.Commands))
	}
	if res.Commands[1].Status != StatusSuccess {
		t.Fatalf("expected second attempt success, got %s", res.Commands[1].Status)
	}
}

func TestCreateProjectRequiresPurposeOrName(t *testing.T) {
	client := NewClient(t.TempDir())
	_, err := client.CreateProject(context.Background(), CreateProjectRequest{})
	if err == nil || !strings.Contains(err.Error(), "purpose or name is required") {
		t.Fatalf("expected validation error, got %v", err)
	}
}

func TestDiscoverProjectsFallbackWhenListCommandUnsupported(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script fixture is unix-only")
	}

	workspace := t.TempDir()
	projectDir := filepath.Join(workspace, "sciclaw-manuscript")
	if err := os.MkdirAll(filepath.Join(projectDir, "plans"), 0755); err != nil {
		t.Fatalf("mkdir project plans: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "plans", "main-plan.md"), []byte("# plan\n"), 0644); err != nil {
		t.Fatalf("write plan file: %v", err)
	}

	binDir := t.TempDir()
	binaryPath := filepath.Join(binDir, "irl")
	if err := writeFakeIRLBinaryWithoutList(binaryPath); err != nil {
		t.Fatalf("write fake irl binary: %v", err)
	}

	client := NewClientWithOptions(workspace, ClientOptions{
		NowFn: func() time.Time {
			return time.Date(2026, time.February, 15, 12, 0, 0, 0, time.UTC)
		},
		LookPathFn: func(file string) (string, error) {
			return binaryPath, nil
		},
		BinaryCandidates: []string{binaryPath},
	})

	res, err := client.DiscoverProjects(context.Background())
	if err != nil {
		t.Fatalf("DiscoverProjects returned error: %v", err)
	}
	if res.Status != StatusPartial {
		t.Fatalf("expected partial status (fallback mode), got %s", res.Status)
	}

	projectsRaw, ok := res.Data["projects"]
	if !ok {
		t.Fatalf("expected projects in fallback result")
	}
	projects, ok := projectsRaw.([]map[string]interface{})
	if !ok {
		t.Fatalf("expected []map[string]interface{} projects, got %T", projectsRaw)
	}
	if len(projects) == 0 {
		t.Fatalf("expected at least one discovered project in fallback mode")
	}
	found := false
	for _, project := range projects {
		if project["name"] == "sciclaw-manuscript" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected sciclaw-manuscript in fallback project list, got %v", projects)
	}

	if source, ok := res.Data["source"]; !ok || source != "filesystem_fallback" {
		t.Fatalf("expected source=filesystem_fallback, got %v", source)
	}
	if len(res.Commands) == 0 {
		t.Fatalf("expected original failed command to be recorded")
	}
}

func TestClientResolvesBinaryFromFallbackCandidates(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script fixture is unix-only")
	}

	workspace := t.TempDir()
	binDir := t.TempDir()
	binaryPath := filepath.Join(binDir, "irl")
	if err := writeFakeIRLBinary(binaryPath, false); err != nil {
		t.Fatalf("write fake irl binary: %v", err)
	}

	client := NewClientWithOptions(workspace, ClientOptions{
		NowFn: func() time.Time {
			return time.Date(2026, time.February, 15, 11, 0, 0, 0, time.UTC)
		},
		LookPathFn: func(file string) (string, error) {
			return "", errors.New("not in PATH")
		},
		BinaryCandidates: []string{binaryPath},
	})

	res, err := client.DiscoverProjects(context.Background())
	if err != nil {
		t.Fatalf("DiscoverProjects returned error: %v", err)
	}
	if res.Status != StatusSuccess {
		t.Fatalf("expected discover status success, got %s", res.Status)
	}
	if len(res.Commands) != 1 {
		t.Fatalf("expected one command record, got %d", len(res.Commands))
	}
	storePath := res.Commands[0].StorePath
	if storePath == "" {
		t.Fatalf("expected store path")
	}
	raw, readErr := os.ReadFile(storePath)
	if readErr != nil {
		t.Fatalf("read store record: %v", readErr)
	}
	if !strings.Contains(string(raw), binaryPath) {
		t.Fatalf("expected record to include resolved binary path %q", binaryPath)
	}
}

func writeFakeIRLBinary(path string, firstListMalformed bool) error {
	markerPath := filepath.Join(filepath.Dir(path), "irl-list-ok.marker")
	malformedLogic := ""
	if firstListMalformed {
		malformedLogic = `
    marker="` + markerPath + `"
    if [ -f "$marker" ]; then
      echo '[{"name":"proj-1"}]'
    else
      echo '{bad-json'
      touch "$marker"
    fi
    `
	} else {
		malformedLogic = `echo '[{"name":"proj-1"}]'`
	}

	content := `#!/bin/sh
set -eu
cmd="${1:-}"
case "$cmd" in
  --version)
    echo "irl 0.9.0"
    ;;
  list)
    if [ "${2:-}" = "--json" ]; then
` + malformedLogic + `
    else
      echo "list requires --json" >&2
      exit 2
    fi
    ;;
  config)
    echo '{"workspace":"demo-workspace"}'
    ;;
  profile)
    echo '{"user":"scientist"}'
    ;;
  init)
    echo 'created project'
    ;;
  adopt)
    echo 'adopted project'
    ;;
  *)
    echo "unknown command: $cmd" >&2
    exit 2
    ;;
esac
`
	if err := os.WriteFile(path, []byte(content), 0755); err != nil {
		return err
	}
	return nil
}

func writeFakeIRLBinaryWithoutList(path string) error {
	content := `#!/bin/sh
set -eu
cmd="${1:-}"
case "$cmd" in
  --version)
    echo "irl 0.5.16"
    ;;
  list)
    echo 'Error: unknown command "list" for "irl"' >&2
    echo "Run 'irl --help' for usage." >&2
    exit 1
    ;;
  config)
    echo "Configuration"
    ;;
  *)
    echo "unknown command: $cmd" >&2
    exit 2
    ;;
esac
`
	return os.WriteFile(path, []byte(content), 0755)
}
