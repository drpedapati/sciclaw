package main

import (
	"path/filepath"
	"testing"
)

func TestWorkspaceVenvPaths(t *testing.T) {
	workspace := "/tmp/sciclaw-workspace"
	if got := workspaceVenvDir(workspace); got != filepath.Join(workspace, ".venv") {
		t.Fatalf("unexpected venv dir: %q", got)
	}
	if got := workspaceVenvBinDir(workspace); got != filepath.Join(workspace, ".venv", "bin") {
		t.Fatalf("unexpected venv bin dir: %q", got)
	}
	if got := workspaceVenvPythonPath(workspace); got != filepath.Join(workspace, ".venv", "bin", "python") {
		t.Fatalf("unexpected venv python path: %q", got)
	}
}

func TestPythonSetupHint(t *testing.T) {
	msg := pythonSetupHint("No module named venv")
	if msg == "" {
		t.Fatalf("expected apt hint for missing venv module")
	}
	if msg2 := pythonSetupHint("some unrelated error"); msg2 != "" {
		t.Fatalf("unexpected hint for unrelated error: %q", msg2)
	}
}
