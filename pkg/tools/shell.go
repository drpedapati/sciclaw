package tools

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

type ExecTool struct {
	workingDir          string
	timeout             time.Duration
	denyPatterns        []*regexp.Regexp
	allowPatterns       []*regexp.Regexp
	restrictToWorkspace bool
	extraEnv            map[string]string
}

func NewExecTool(workingDir string, restrict bool) *ExecTool {
	denyPatterns := []*regexp.Regexp{
		regexp.MustCompile(`\brm\s+-[rf]{1,2}\b`),
		regexp.MustCompile(`\bdel\s+/[fq]\b`),
		regexp.MustCompile(`\brmdir\s+/s\b`),
		regexp.MustCompile(`\b(format|mkfs|diskpart)\b\s`), // Match disk wiping commands (must be followed by space/args)
		regexp.MustCompile(`\bdd\s+if=`),
		regexp.MustCompile(`>\s*/dev/sd[a-z]\b`), // Block writes to disk devices (but allow /dev/null)
		regexp.MustCompile(`\b(shutdown|reboot|poweroff)\b`),
		regexp.MustCompile(`:\(\)\s*\{.*\};\s*:`),
	}

	return &ExecTool{
		workingDir:          workingDir,
		timeout:             60 * time.Second,
		denyPatterns:        denyPatterns,
		allowPatterns:       nil,
		restrictToWorkspace: restrict,
		extraEnv:            nil,
	}
}

// SetExtraEnv provides environment variables that will be injected into all shell commands.
// Values override any existing variables with the same name.
func (t *ExecTool) SetExtraEnv(env map[string]string) {
	if len(env) == 0 {
		return
	}
	if t.extraEnv == nil {
		t.extraEnv = map[string]string{}
	}
	for k, v := range env {
		if strings.TrimSpace(k) == "" {
			continue
		}
		t.extraEnv[k] = v
	}
}

func (t *ExecTool) Name() string {
	return "exec"
}

func (t *ExecTool) Description() string {
	return "Execute a shell command and return its output. Use with caution."
}

func (t *ExecTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"command": map[string]interface{}{
				"type":        "string",
				"description": "The shell command to execute",
			},
			"working_dir": map[string]interface{}{
				"type":        "string",
				"description": "Optional working directory for the command",
			},
		},
		"required": []string{"command"},
	}
}

func (t *ExecTool) Execute(ctx context.Context, args map[string]interface{}) *ToolResult {
	command, ok := args["command"].(string)
	if !ok {
		return ErrorResult("command is required")
	}

	cwd := t.workingDir
	if wd, ok := args["working_dir"].(string); ok && wd != "" {
		cwd = wd
	}

	if cwd == "" {
		wd, err := os.Getwd()
		if err == nil {
			cwd = wd
		}
	}

	if guardError := t.guardCommand(command, cwd); guardError != "" {
		return ErrorResult(guardError)
	}

	cmdCtx, cancel := context.WithTimeout(ctx, t.timeout)
	defer cancel()

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(cmdCtx, "powershell", "-NoProfile", "-NonInteractive", "-Command", command)
	} else {
		cmd = exec.CommandContext(cmdCtx, "sh", "-c", command)
	}
	if cwd != "" {
		cmd.Dir = cwd
	}
	if len(t.extraEnv) > 0 {
		cmd.Env = mergeEnv(os.Environ(), t.extraEnv)
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	output := stdout.String()
	if stderr.Len() > 0 {
		output += "\nSTDERR:\n" + stderr.String()
	}

	if err != nil {
		if cmdCtx.Err() == context.DeadlineExceeded {
			msg := fmt.Sprintf("Command timed out after %v", t.timeout)
			return &ToolResult{
				ForLLM:  msg,
				ForUser: msg,
				IsError: true,
			}
		}
		output += fmt.Sprintf("\nExit code: %v", err)
	}

	if output == "" {
		output = "(no output)"
	}

	maxLen := 10000
	if len(output) > maxLen {
		output = output[:maxLen] + fmt.Sprintf("\n... (truncated, %d more chars)", len(output)-maxLen)
	}

	if err != nil {
		return &ToolResult{
			ForLLM:  output,
			ForUser: output,
			IsError: true,
		}
	}

	return &ToolResult{
		ForLLM:  output,
		ForUser: output,
		IsError: false,
	}
}

func mergeEnv(base []string, overrides map[string]string) []string {
	if len(overrides) == 0 {
		return base
	}

	// Remove keys we override (case-sensitive; matches typical UNIX semantics).
	out := make([]string, 0, len(base)+len(overrides))
	for _, kv := range base {
		keep := true
		for k := range overrides {
			prefix := k + "="
			if strings.HasPrefix(kv, prefix) {
				keep = false
				break
			}
		}
		if keep {
			out = append(out, kv)
		}
	}
	for k, v := range overrides {
		out = append(out, fmt.Sprintf("%s=%s", k, v))
	}
	return out
}

func (t *ExecTool) guardCommand(command, cwd string) string {
	cmd := strings.TrimSpace(command)
	lower := strings.ToLower(cmd)

	for _, pattern := range t.denyPatterns {
		if pattern.MatchString(lower) {
			return "Command blocked by safety guard (dangerous pattern detected)"
		}
	}

	if len(t.allowPatterns) > 0 {
		allowed := false
		for _, pattern := range t.allowPatterns {
			if pattern.MatchString(lower) {
				allowed = true
				break
			}
		}
		if !allowed {
			return "Command blocked by safety guard (not in allowlist)"
		}
	}

	if isPythonSubprocessWrapperForInstalledCLI(cmd) {
		return "Command blocked by safety guard (avoid Python subprocess wrappers for pubmed/docx-review; call the CLI directly)"
	}

	if t.restrictToWorkspace {
		if strings.Contains(cmd, "..\\") || strings.Contains(cmd, "../") {
			return "Command blocked by safety guard (path traversal detected)"
		}

		cwdPath, err := filepath.Abs(cwd)
		if err != nil {
			return ""
		}

		pathPattern := regexp.MustCompile(`[A-Za-z]:\\[^\\\"']+|/[^\s\"']+`)
		pathScanInput := cmd
		// PubMed search syntax uses field tags like [Title/Abstract], which can be
		// misread as absolute paths by the generic scanner. Strip bracketed tags
		// for path scanning only, while still keeping other safety checks active.
		if isPubMedCommand(cmd) {
			pathScanInput = stripBracketSegments(cmd)
		}
		// URL literals are not filesystem paths and should not trigger
		// workspace path checks.
		pathScanInput = stripURLSegments(pathScanInput)
		matches := pathPattern.FindAllString(pathScanInput, -1)

		for _, raw := range matches {
			p, err := filepath.Abs(raw)
			if err != nil {
				continue
			}

			rel, err := filepath.Rel(cwdPath, p)
			if err != nil {
				continue
			}

			if strings.HasPrefix(rel, "..") {
				return "Command blocked by safety guard (path outside working dir)"
			}
		}
	}

	return ""
}

func isPubMedCommand(command string) bool {
	fields := strings.Fields(strings.TrimSpace(command))
	if len(fields) == 0 {
		return false
	}
	switch strings.ToLower(fields[0]) {
	case "pubmed", "pubmed-cli":
		return true
	default:
		return false
	}
}

func isPythonSubprocessWrapperForInstalledCLI(command string) bool {
	lower := strings.ToLower(strings.TrimSpace(command))
	if lower == "" {
		return false
	}
	if !strings.Contains(lower, "python") {
		return false
	}
	if !strings.Contains(lower, "subprocess") {
		return false
	}
	if !(strings.Contains(lower, "check_output") || strings.Contains(lower, "subprocess.run") || strings.Contains(lower, "popen(")) {
		return false
	}
	if strings.Contains(lower, "pubmed") || strings.Contains(lower, "docx-review") {
		return true
	}
	return false
}

func stripBracketSegments(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	depth := 0
	for _, r := range s {
		switch r {
		case '[':
			depth++
			b.WriteRune(' ')
		case ']':
			if depth > 0 {
				depth--
				b.WriteRune(' ')
			} else {
				b.WriteRune(r)
			}
		default:
			if depth > 0 {
				b.WriteRune(' ')
			} else {
				b.WriteRune(r)
			}
		}
	}
	return b.String()
}

func stripURLSegments(s string) string {
	urlPattern := regexp.MustCompile(`https?://[^\s"'` + "`" + `]+`)
	return urlPattern.ReplaceAllString(s, " ")
}

func (t *ExecTool) SetTimeout(timeout time.Duration) {
	t.timeout = timeout
}

func (t *ExecTool) SetRestrictToWorkspace(restrict bool) {
	t.restrictToWorkspace = restrict
}

func (t *ExecTool) SetAllowPatterns(patterns []string) error {
	t.allowPatterns = make([]*regexp.Regexp, 0, len(patterns))
	for _, p := range patterns {
		re, err := regexp.Compile(p)
		if err != nil {
			return fmt.Errorf("invalid allow pattern %q: %w", p, err)
		}
		t.allowPatterns = append(t.allowPatterns, re)
	}
	return nil
}
