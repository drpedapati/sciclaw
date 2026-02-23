package tools

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestShellTool_Success verifies successful command execution
func TestShellTool_Success(t *testing.T) {
	tool := NewExecTool("", false)

	ctx := context.Background()
	args := map[string]interface{}{
		"command": "echo 'hello world'",
	}

	result := tool.Execute(ctx, args)

	// Success should not be an error
	if result.IsError {
		t.Errorf("Expected success, got IsError=true: %s", result.ForLLM)
	}

	// ForUser should contain command output
	if !strings.Contains(result.ForUser, "hello world") {
		t.Errorf("Expected ForUser to contain 'hello world', got: %s", result.ForUser)
	}

	// ForLLM should contain full output
	if !strings.Contains(result.ForLLM, "hello world") {
		t.Errorf("Expected ForLLM to contain 'hello world', got: %s", result.ForLLM)
	}
}

// TestShellTool_Failure verifies failed command execution
func TestShellTool_Failure(t *testing.T) {
	tool := NewExecTool("", false)

	ctx := context.Background()
	args := map[string]interface{}{
		"command": "ls /nonexistent_directory_12345",
	}

	result := tool.Execute(ctx, args)

	// Failure should be marked as error
	if !result.IsError {
		t.Errorf("Expected error for failed command, got IsError=false")
	}

	// ForUser should contain error information
	if result.ForUser == "" {
		t.Errorf("Expected ForUser to contain error info, got empty string")
	}

	// ForLLM should contain exit code or error
	if !strings.Contains(result.ForLLM, "Exit code") && result.ForUser == "" {
		t.Errorf("Expected ForLLM to contain exit code or error, got: %s", result.ForLLM)
	}
}

// TestShellTool_Timeout verifies command timeout handling
func TestShellTool_Timeout(t *testing.T) {
	tool := NewExecTool("", false)
	tool.SetTimeout(100 * time.Millisecond)

	ctx := context.Background()
	args := map[string]interface{}{
		"command": "sleep 10",
	}

	result := tool.Execute(ctx, args)

	// Timeout should be marked as error
	if !result.IsError {
		t.Errorf("Expected error for timeout, got IsError=false")
	}

	// Should mention timeout
	if !strings.Contains(result.ForLLM, "timed out") && !strings.Contains(result.ForUser, "timed out") {
		t.Errorf("Expected timeout message, got ForLLM: %s, ForUser: %s", result.ForLLM, result.ForUser)
	}
}

// TestShellTool_WorkingDir verifies custom working directory
func TestShellTool_WorkingDir(t *testing.T) {
	// Create temp directory
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.txt")
	os.WriteFile(testFile, []byte("test content"), 0644)

	tool := NewExecTool("", false)

	ctx := context.Background()
	args := map[string]interface{}{
		"command":     "cat test.txt",
		"working_dir": tmpDir,
	}

	result := tool.Execute(ctx, args)

	if result.IsError {
		t.Errorf("Expected success in custom working dir, got error: %s", result.ForLLM)
	}

	if !strings.Contains(result.ForUser, "test content") {
		t.Errorf("Expected output from custom dir, got: %s", result.ForUser)
	}
}

// TestShellTool_DangerousCommand verifies safety guard blocks dangerous commands
func TestShellTool_DangerousCommand(t *testing.T) {
	tool := NewExecTool("", false)

	ctx := context.Background()
	args := map[string]interface{}{
		"command": "rm -rf /",
	}

	result := tool.Execute(ctx, args)

	// Dangerous command should be blocked
	if !result.IsError {
		t.Errorf("Expected dangerous command to be blocked (IsError=true)")
	}

	if !strings.Contains(result.ForLLM, "blocked") && !strings.Contains(result.ForUser, "blocked") {
		t.Errorf("Expected 'blocked' message, got ForLLM: %s, ForUser: %s", result.ForLLM, result.ForUser)
	}
}

// TestShellTool_MissingCommand verifies error handling for missing command
func TestShellTool_MissingCommand(t *testing.T) {
	tool := NewExecTool("", false)

	ctx := context.Background()
	args := map[string]interface{}{}

	result := tool.Execute(ctx, args)

	// Should return error result
	if !result.IsError {
		t.Errorf("Expected error when command is missing")
	}
}

// TestShellTool_StderrCapture verifies stderr is captured and included
func TestShellTool_StderrCapture(t *testing.T) {
	tool := NewExecTool("", false)

	ctx := context.Background()
	args := map[string]interface{}{
		"command": "sh -c 'echo stdout; echo stderr >&2'",
	}

	result := tool.Execute(ctx, args)

	// Both stdout and stderr should be in output
	if !strings.Contains(result.ForLLM, "stdout") {
		t.Errorf("Expected stdout in output, got: %s", result.ForLLM)
	}
	if !strings.Contains(result.ForLLM, "stderr") {
		t.Errorf("Expected stderr in output, got: %s", result.ForLLM)
	}
}

// TestShellTool_OutputTruncation verifies long output is truncated
func TestShellTool_OutputTruncation(t *testing.T) {
	tool := NewExecTool("", false)

	ctx := context.Background()
	// Generate long output (>10000 chars)
	args := map[string]interface{}{
		"command": "python3 -c \"print('x' * 20000)\" || echo " + strings.Repeat("x", 20000),
	}

	result := tool.Execute(ctx, args)

	// Should have truncation message or be truncated
	if len(result.ForLLM) > 15000 {
		t.Errorf("Expected output to be truncated, got length: %d", len(result.ForLLM))
	}
}

// TestShellTool_RestrictToWorkspace verifies workspace restriction
func TestShellTool_RestrictToWorkspace(t *testing.T) {
	tmpDir := t.TempDir()
	tool := NewExecTool(tmpDir, false)
	tool.SetRestrictToWorkspace(true)

	ctx := context.Background()
	args := map[string]interface{}{
		"command": "cat ../../etc/passwd",
	}

	result := tool.Execute(ctx, args)

	// Path traversal should be blocked
	if !result.IsError {
		t.Errorf("Expected path traversal to be blocked with restrictToWorkspace=true")
	}

	if !strings.Contains(result.ForLLM, "blocked") && !strings.Contains(result.ForUser, "blocked") {
		t.Errorf("Expected 'blocked' message for path traversal, got ForLLM: %s, ForUser: %s", result.ForLLM, result.ForUser)
	}
}

func TestShellTool_PubMedFieldTagsNotBlockedByPathGuard(t *testing.T) {
	tmpDir := t.TempDir()
	tool := NewExecTool(tmpDir, false)
	tool.SetRestrictToWorkspace(true)

	cmd := `pubmed search "\"innovations in child depression\"[Title/Abstract] AND depression[MeSH Terms]" --json --limit 20`
	if blocked := tool.guardCommand(cmd, tmpDir); blocked != "" {
		t.Fatalf("expected pubmed query with field tags to pass guard, got: %s", blocked)
	}
}

func TestShellTool_PubMedAllowsTempPathOutput(t *testing.T) {
	tmpDir := t.TempDir()
	tool := NewExecTool(tmpDir, false)
	tool.SetRestrictToWorkspace(true)

	cmd := `pubmed search "depression[Title/Abstract]" --json > /tmp/pubmed.json`
	if blocked := tool.guardCommand(cmd, tmpDir); blocked != "" {
		t.Fatalf("expected temp output path to pass guard, got: %q", blocked)
	}
}

func TestShellTool_HeredocURLNotBlockedByPathGuard(t *testing.T) {
	tmpDir := t.TempDir()
	tool := NewExecTool(tmpDir, false)
	tool.SetRestrictToWorkspace(true)

	cmd := "python3 - <<'PY'\nimport requests\nurl = 'https://pubmed.ncbi.nlm.nih.gov/41694131/'\nprint(requests.get(url, timeout=5).status_code)\nPY"
	if blocked := tool.guardCommand(cmd, tmpDir); blocked != "" {
		t.Fatalf("expected heredoc URL to pass guard, got: %s", blocked)
	}
}

func TestShellTool_URLWithOutsidePathStillBlocked(t *testing.T) {
	tmpDir := t.TempDir()
	tool := NewExecTool(tmpDir, false)
	tool.SetRestrictToWorkspace(true)

	cmd := "python3 - <<'PY'\nurl='https://pubmed.ncbi.nlm.nih.gov/41694131/'\nprint(url)\nPY\ncat /etc/secrets.txt"
	blocked := tool.guardCommand(cmd, tmpDir)
	if !strings.Contains(blocked, "outside working dir") {
		t.Fatalf("expected outside-workspace path to remain blocked, got: %q", blocked)
	}
}

func TestShellTool_HeredocEscapedURLDataNotBlockedByPathGuard(t *testing.T) {
	tmpDir := t.TempDir()
	tool := NewExecTool(tmpDir, false)
	tool.SetRestrictToWorkspace(true)

	cmd := "cat <<'EOF' > report.json\n{\"doi\":\"https:\\/\\/doi.org\\/10.1000\\/xyz123\"}\nEOF"
	if blocked := tool.guardCommand(cmd, tmpDir); blocked != "" {
		t.Fatalf("expected escaped URL in heredoc data to pass guard, got: %s", blocked)
	}
}

func TestShellTool_AllowsDevNullRedirection(t *testing.T) {
	tmpDir := t.TempDir()
	tool := NewExecTool(tmpDir, false)
	tool.SetRestrictToWorkspace(true)

	cmd := "echo ok > /dev/null"
	if blocked := tool.guardCommand(cmd, tmpDir); blocked != "" {
		t.Fatalf("expected /dev/null redirection to pass guard, got: %q", blocked)
	}
}

func TestShellTool_BlocksPythonSubprocessWrapperForPubMed(t *testing.T) {
	tool := NewExecTool("", false)
	cmd := "python3 - <<'PY'\nimport subprocess\nsubprocess.check_output(['pubmed','search','schizophrenia','--json'], text=True)\nPY"
	blocked := tool.guardCommand(cmd, "")
	if !strings.Contains(blocked, "avoid Python subprocess wrappers") {
		t.Fatalf("expected wrapper block message, got: %q", blocked)
	}
}

func TestShellTool_AllowsDirectPubMedCLI(t *testing.T) {
	tool := NewExecTool("", false)
	cmd := `pubmed search "schizophrenia" --json --limit 5`
	if blocked := tool.guardCommand(cmd, ""); blocked != "" {
		t.Fatalf("expected direct pubmed CLI call to pass guard, got: %q", blocked)
	}
}

func TestShouldApplyNIHPandocTemplate(t *testing.T) {
	cases := []struct {
		name string
		cmd  string
		want bool
	}{
		{
			name: "pandoc output docx",
			cmd:  "pandoc manuscript.md -o manuscript.docx",
			want: true,
		},
		{
			name: "pandoc to docx",
			cmd:  "pandoc manuscript.md --to docx --output out.docx",
			want: true,
		},
		{
			name: "explicit reference doc no override",
			cmd:  "pandoc manuscript.md -o out.docx --reference-doc custom.docx",
			want: false,
		},
		{
			name: "explicit defaults file no override",
			cmd:  "pandoc manuscript.md -o out.docx --defaults custom.yaml",
			want: false,
		},
		{
			name: "non-pandoc command",
			cmd:  "echo hello",
			want: false,
		},
		{
			name: "pandoc non-docx output",
			cmd:  "pandoc manuscript.md -o manuscript.pdf",
			want: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := shouldApplyNIHPandocTemplate(tc.cmd); got != tc.want {
				t.Fatalf("shouldApplyNIHPandocTemplate(%q) = %v, want %v", tc.cmd, got, tc.want)
			}
		})
	}
}

func TestCommandWithPandocDefaults_UsesConfiguredTemplate(t *testing.T) {
	tmpDir := t.TempDir()
	templatePath := filepath.Join(tmpDir, "nih-standard.docx")
	if err := os.WriteFile(templatePath, []byte("template"), 0o644); err != nil {
		t.Fatalf("write template: %v", err)
	}
	defaultsPath := filepath.Join(tmpDir, "pandoc-defaults.yaml")

	t.Setenv("SCICLAW_NIH_REFERENCE_DOC", templatePath)
	t.Setenv("SCICLAW_PANDOC_DEFAULTS_PATH", defaultsPath)

	tool := NewExecTool("", false)
	rewritten, err := tool.commandWithPandocDefaults("pandoc input.md -o output.docx")
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if !strings.Contains(rewritten, "--defaults "+strconv.Quote(defaultsPath)) {
		t.Fatalf("expected rewritten command to include defaults path %q, got: %s", defaultsPath, rewritten)
	}

	content, err := os.ReadFile(defaultsPath)
	if err != nil {
		t.Fatalf("read defaults file: %v", err)
	}
	if !strings.Contains(string(content), "reference-doc:") {
		t.Fatalf("defaults file missing reference-doc key: %s", string(content))
	}
	if !strings.Contains(string(content), templatePath) {
		t.Fatalf("defaults file missing template path: %s", string(content))
	}
}

func TestCommandWithPandocDefaults_UsesSciclawCanonicalTemplate(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("PICOCLAW_HOME", tmpDir)
	defaultsPath := filepath.Join(tmpDir, "pandoc-defaults.yaml")
	t.Setenv("SCICLAW_PANDOC_DEFAULTS_PATH", defaultsPath)
	t.Setenv("SCICLAW_NIH_REFERENCE_DOC", "")

	tool := NewExecTool("", false)
	rewritten, err := tool.commandWithPandocDefaults("pandoc input.md -o output.docx")
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if !strings.Contains(rewritten, "--defaults "+strconv.Quote(defaultsPath)) {
		t.Fatalf("expected rewritten command to include defaults path %q, got: %s", defaultsPath, rewritten)
	}

	expectedTemplate := filepath.Join(tmpDir, "templates", "nih-standard.docx")
	if _, err := os.Stat(expectedTemplate); err != nil {
		t.Fatalf("expected canonical NIH template to be materialized at %q: %v", expectedTemplate, err)
	}

	content, err := os.ReadFile(defaultsPath)
	if err != nil {
		t.Fatalf("read defaults file: %v", err)
	}
	if !strings.Contains(string(content), expectedTemplate) {
		t.Fatalf("defaults file missing canonical template path: %s", string(content))
	}
}

func TestShellTool_ExecuteInjectsPandocDefaultsForDocx(t *testing.T) {
	tmpDir := t.TempDir()
	homeDir := filepath.Join(tmpDir, "home")
	binDir := filepath.Join(tmpDir, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir bin dir: %v", err)
	}

	pandocScript := filepath.Join(binDir, "pandoc")
	script := "#!/bin/sh\nprintf '%s' \"$*\"\n"
	if err := os.WriteFile(pandocScript, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake pandoc: %v", err)
	}

	defaultsPath := filepath.Join(tmpDir, "pandoc-defaults.yaml")
	t.Setenv("PICOCLAW_HOME", homeDir)
	t.Setenv("SCICLAW_PANDOC_DEFAULTS_PATH", defaultsPath)
	t.Setenv("SCICLAW_NIH_REFERENCE_DOC", "")

	pathEnv := binDir + string(os.PathListSeparator) + os.Getenv("PATH")
	tool := NewExecTool("", false)
	tool.SetExtraEnv(map[string]string{"PATH": pathEnv})

	result := tool.Execute(context.Background(), map[string]interface{}{
		"command": "pandoc input.md -o output.docx",
	})
	if result.IsError {
		t.Fatalf("expected successful fake pandoc execution, got error: %s", result.ForLLM)
	}

	out := strings.TrimSpace(result.ForLLM)
	if !strings.Contains(out, "--defaults "+defaultsPath) {
		t.Fatalf("expected injected --defaults arg with path %q, got %q", defaultsPath, out)
	}

	expectedTemplate := filepath.Join(homeDir, "templates", "nih-standard.docx")
	if _, err := os.Stat(expectedTemplate); err != nil {
		t.Fatalf("expected NIH template materialized at %q: %v", expectedTemplate, err)
	}
}

func TestShellTool_ExecuteDoesNotInjectPandocDefaultsForNonDocx(t *testing.T) {
	tmpDir := t.TempDir()
	binDir := filepath.Join(tmpDir, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir bin dir: %v", err)
	}

	pandocScript := filepath.Join(binDir, "pandoc")
	script := "#!/bin/sh\nprintf '%s' \"$*\"\n"
	if err := os.WriteFile(pandocScript, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake pandoc: %v", err)
	}

	tool := NewExecTool("", false)
	tool.SetExtraEnv(map[string]string{
		"PATH": binDir + string(os.PathListSeparator) + os.Getenv("PATH"),
	})
	result := tool.Execute(context.Background(), map[string]interface{}{
		"command": "pandoc input.md -o output.pdf",
	})
	if result.IsError {
		t.Fatalf("expected successful fake pandoc execution, got error: %s", result.ForLLM)
	}
	if strings.Contains(strings.TrimSpace(result.ForLLM), "--defaults ") {
		t.Fatalf("expected no --defaults injection for non-docx command, got %q", strings.TrimSpace(result.ForLLM))
	}
}

func TestMergedExecPATHIncludesHomebrewAndSystemBins(t *testing.T) {
	got := mergedExecPATH("/usr/bin:/bin")
	for _, want := range []string{"/opt/homebrew/bin", "/usr/local/bin", "/usr/bin", "/bin"} {
		if !strings.Contains(got, want) {
			t.Fatalf("mergedExecPATH missing %q in %q", want, got)
		}
	}
}

func TestMergePathEntriesDedupesAndKeepsBaseOrder(t *testing.T) {
	got := mergePathEntries(
		[]string{"/alpha/bin", "/beta/bin", "/alpha/bin"},
		[]string{"/beta/bin", "/gamma/bin"},
	)
	if got != "/alpha/bin:/beta/bin:/gamma/bin" {
		t.Fatalf("unexpected merged path: %q", got)
	}
}
