package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const pythonBootstrapMarker = ".sciclaw-python-packages-v1"

var workspacePythonPackages = []string{
	"requests",
	"beautifulsoup4",
	"python-docx",
	"lxml",
	"PyYAML",
}

func workspaceVenvDir(workspace string) string {
	return filepath.Join(workspace, ".venv")
}

func workspaceVenvBinDir(workspace string) string {
	return filepath.Join(workspaceVenvDir(workspace), "bin")
}

func workspaceVenvPythonPath(workspace string) string {
	return filepath.Join(workspaceVenvBinDir(workspace), "python")
}

func workspacePythonMarkerPath(workspace string) string {
	return filepath.Join(workspaceVenvDir(workspace), pythonBootstrapMarker)
}

func ensureWorkspacePythonEnvironment(workspace string) (string, error) {
	workspace = filepath.Clean(strings.TrimSpace(workspace))
	if workspace == "" {
		return "", fmt.Errorf("workspace path is empty")
	}

	// Linux is the critical path for PEP 668 restrictions and missing base packages.
	if runtime.GOOS != "linux" {
		return workspaceVenvBinDir(workspace), nil
	}

	venvPython := workspaceVenvPythonPath(workspace)

	if err := os.MkdirAll(workspace, 0755); err != nil {
		return "", fmt.Errorf("create workspace dir: %w", err)
	}

	if !fileExists(venvPython) {
		if err := createWorkspaceVenv(workspace); err != nil {
			return "", err
		}
	}

	if fileExists(workspacePythonMarkerPath(workspace)) {
		return workspaceVenvBinDir(workspace), nil
	}

	if err := installWorkspacePythonPackages(workspace); err != nil {
		return "", err
	}

	if err := os.WriteFile(workspacePythonMarkerPath(workspace), []byte(time.Now().UTC().Format(time.RFC3339)+"\n"), 0644); err != nil {
		return "", fmt.Errorf("write python bootstrap marker: %w", err)
	}

	return workspaceVenvBinDir(workspace), nil
}

func createWorkspaceVenv(workspace string) error {
	venvDir := workspaceVenvDir(workspace)
	venvPython := workspaceVenvPythonPath(workspace)

	if uvPath, err := exec.LookPath("uv"); err == nil {
		if _, err := runCommandWithOutput(2*time.Minute, uvPath, "venv", venvDir); err == nil && fileExists(venvPython) {
			return nil
		}
	}

	python3, err := exec.LookPath("python3")
	if err != nil {
		return fmt.Errorf("python3 not found and uv venv unavailable (install `uv` or python3)")
	}

	out, err := runCommandWithOutput(2*time.Minute, python3, "-m", "venv", venvDir)
	if err != nil {
		return fmt.Errorf("create venv failed: %s%s", singleLine(out), pythonSetupHint(out))
	}
	if !fileExists(venvPython) {
		return fmt.Errorf("venv creation finished but %s is missing", venvPython)
	}
	return nil
}

func installWorkspacePythonPackages(workspace string) error {
	venvPython := workspaceVenvPythonPath(workspace)
	if !fileExists(venvPython) {
		return fmt.Errorf("venv python not found: %s", venvPython)
	}

	// Prefer uv's pip frontend if available; it handles modern Python packaging smoothly.
	if uvPath, err := exec.LookPath("uv"); err == nil {
		args := append([]string{"pip", "install", "--python", venvPython}, workspacePythonPackages...)
		if out, err := runCommandWithOutput(3*time.Minute, uvPath, args...); err == nil {
			_ = out
			return nil
		}
	}

	// Fallback for environments without uv.
	args := append([]string{"-m", "pip", "install", "--disable-pip-version-check"}, workspacePythonPackages...)
	out, err := runCommandWithOutput(3*time.Minute, venvPython, args...)
	if err != nil {
		return fmt.Errorf("install python packages failed: %s%s", singleLine(out), pythonSetupHint(out))
	}
	return nil
}

func runCommandWithOutput(timeout time.Duration, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, name, args...)
	out, err := cmd.CombinedOutput()
	text := strings.TrimSpace(string(out))
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		if text == "" {
			text = "command timed out"
		}
		return text, ctx.Err()
	}
	if err != nil {
		if text == "" {
			text = err.Error()
		}
		return text, err
	}
	return text, nil
}

func pythonSetupHint(output string) string {
	lower := strings.ToLower(output)
	if strings.Contains(lower, "no module named venv") || strings.Contains(lower, "ensurepip is not available") {
		return " (hint: sudo apt-get update && sudo apt-get install -y python3-venv python3-pip python-is-python3)"
	}
	return ""
}

func singleLine(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.TrimSpace(s)
	if len(s) > 220 {
		return s[:220] + "..."
	}
	return s
}
