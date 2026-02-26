package tools

import (
	"bytes"
	"context"
	_ "embed"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

type ExecTool struct {
	workingDir              string
	timeout                 time.Duration
	denyPatterns            []*regexp.Regexp
	allowPatterns           []*regexp.Regexp
	restrictToWorkspace     bool
	sharedWorkspace         string
	sharedWorkspaceReadOnly bool
	extraEnv                map[string]string
}

var (
	shellPathPattern = regexp.MustCompile(`[A-Za-z]:\\[^\\\"']+|/[^\s\"']+`)
	shellURLPattern              = regexp.MustCompile("https?://[^\\s\"'`]+")
	shellMutatingCommandPattern  = regexp.MustCompile(`(?i)(^|[;&|()\s])(touch|mkdir|rmdir|rm|mv|cp|install|chmod|chown|truncate|tee|sed\s+-i|perl\s+-i|pandoc)([;&|()\s]|$)`)
	shellWriteRedirectPattern    = regexp.MustCompile(`(^|[^0-9])>>?`)
	pandocBinaryPattern          = regexp.MustCompile(`(?i)(^|[;&|()\s])pandoc([;&|()\s]|$)`)
	pandocDefaultsPattern        = regexp.MustCompile(`(?i)(^|[\s;|&])(--defaults|-d)(=|\s+)[^;\n|&]+`)
	pandocOutputDocxPattern      = regexp.MustCompile(`(?i)(^|[\s;|&])(--output|-o)(=|\s+)[^;\n|&]*\.docx(\s|$)`)
	pandocToDocxPattern          = regexp.MustCompile(`(?i)(^|[\s;|&])(--to|-t)(=|\s*)docx(\s|$)`)
	sciclawNIHTemplateCandidates = []string{
		"/opt/homebrew/opt/sciclaw/share/sciclaw/templates/nih-standard.docx",
		"/home/linuxbrew/.linuxbrew/opt/sciclaw/share/sciclaw/templates/nih-standard.docx",
		"/usr/local/opt/sciclaw/share/sciclaw/templates/nih-standard.docx",
	}
)

//go:embed assets/nih-standard.docx
var embeddedNIHTemplate []byte

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

	if t.restrictToWorkspace && strings.TrimSpace(cwd) != "" {
		resolvedCWD, err := validatePathWithPolicy(cwd, t.workingDir, true, AccessRead, t.sharedWorkspace, t.sharedWorkspaceReadOnly)
		if err != nil {
			return UserErrorResult("Command blocked by safety guard (" + err.Error() + ")")
		}
		cwd = resolvedCWD
	}

	if guardError := t.guardCommand(command, cwd); guardError != "" {
		return UserErrorResult(guardError)
	}
	withPandocDefaults, pandocErr := t.commandWithPandocDefaults(command)
	if pandocErr != nil {
		return ErrorResult(pandocErr.Error())
	}
	command = withPandocDefaults

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
	envOverrides := make(map[string]string)
	for k, v := range t.extraEnv {
		envOverrides[k] = v
	}
	pathBase := envOverrides["PATH"]
	if strings.TrimSpace(pathBase) == "" {
		pathBase = os.Getenv("PATH")
	}
	envOverrides["PATH"] = mergedExecPATH(pathBase)
	cmd.Env = mergeEnv(os.Environ(), envOverrides)

	prepareCommandForTermination(cmd)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		return ErrorResult(fmt.Sprintf("failed to start command: %v", err))
	}

	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	var err error
	select {
	case err = <-done:
	case <-cmdCtx.Done():
		_ = terminateProcessTree(cmd)
		select {
		case err = <-done:
		case <-time.After(2 * time.Second):
			if cmd.Process != nil {
				_ = cmd.Process.Kill()
			}
			err = <-done
		}
	}

	output := stdout.String()
	if stderr.Len() > 0 {
		output += "\nSTDERR:\n" + stderr.String()
	}

	if err != nil {
		if errors.Is(cmdCtx.Err(), context.DeadlineExceeded) {
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

func mergedExecPATH(current string) string {
	entries := splitAndCleanPath(current)
	home, _ := os.UserHomeDir()
	extras := []string{
		filepath.Join(home, ".local", "bin"),
		"/opt/homebrew/bin",
		"/opt/homebrew/sbin",
		"/usr/local/bin",
		"/usr/local/sbin",
		"/usr/bin",
		"/bin",
		"/usr/sbin",
		"/sbin",
	}
	return mergePathEntries(entries, extras)
}

func splitAndCleanPath(pathValue string) []string {
	var out []string
	for _, p := range strings.Split(pathValue, string(os.PathListSeparator)) {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		out = append(out, p)
	}
	return out
}

func mergePathEntries(base []string, extras []string) string {
	seen := map[string]struct{}{}
	var ordered []string
	appendIfNew := func(path string) {
		path = strings.TrimSpace(path)
		if path == "" {
			return
		}
		if _, exists := seen[path]; exists {
			return
		}
		seen[path] = struct{}{}
		ordered = append(ordered, path)
	}

	for _, p := range base {
		appendIfNew(p)
	}
	for _, p := range extras {
		appendIfNew(p)
	}

	return strings.Join(ordered, string(os.PathListSeparator))
}

func (t *ExecTool) commandWithPandocDefaults(command string) (string, error) {
	if !shouldApplyNIHPandocTemplate(command) {
		return command, nil
	}

	templatePath := resolveNIHTemplatePath()
	if templatePath == "" {
		return "", fmt.Errorf("pandoc DOCX generation requires sciClaw's NIH template; run `sciclaw onboard` or set SCICLAW_NIH_REFERENCE_DOC")
	}

	defaultsPath, err := ensurePandocDefaultsFile(templatePath)
	if err != nil {
		return "", fmt.Errorf("prepare pandoc defaults: %w", err)
	}
	quotedDefaults := strconv.Quote(defaultsPath)
	replacement := "${1}pandoc --defaults " + quotedDefaults + "${2}"
	return pandocBinaryPattern.ReplaceAllString(command, replacement), nil
}

func shouldApplyNIHPandocTemplate(command string) bool {
	cmd := strings.TrimSpace(command)
	if cmd == "" {
		return false
	}
	lower := strings.ToLower(cmd)
	if !pandocBinaryPattern.MatchString(lower) {
		return false
	}
	if pandocDefaultsPattern.MatchString(cmd) {
		return false
	}
	if strings.Contains(lower, "--reference-doc") {
		return false
	}
	if pandocOutputDocxPattern.MatchString(cmd) {
		return true
	}
	if pandocToDocxPattern.MatchString(cmd) {
		return true
	}
	return false
}

func resolveNIHTemplatePath() string {
	if p := strings.TrimSpace(os.Getenv("SCICLAW_NIH_REFERENCE_DOC")); p != "" {
		if isRegularFile(p) {
			return absCleanPath(p)
		}
	}

	canonicalPath := defaultNIHTemplatePath()
	if canonicalPath != "" && isRegularFile(canonicalPath) {
		return absCleanPath(canonicalPath)
	}

	candidates := sciclawTemplateCandidatePaths()
	for _, p := range candidates {
		if isRegularFile(p) {
			if canonicalPath == "" {
				return absCleanPath(p)
			}
			if err := copyFileContents(p, canonicalPath, 0o644); err == nil {
				return absCleanPath(canonicalPath)
			}
			return absCleanPath(p)
		}
	}

	if canonicalPath != "" && ensureEmbeddedNIHTemplate(canonicalPath) == nil && isRegularFile(canonicalPath) {
		return absCleanPath(canonicalPath)
	}
	return ""
}

// ResolvePandocNIHTemplatePath returns the effective NIH reference-doc path
// that sciClaw will use for pandoc DOCX generation.
func ResolvePandocNIHTemplatePath() string {
	return resolveNIHTemplatePath()
}

func sciclawTemplateCandidatePaths() []string {
	candidates := make([]string, 0, len(sciclawNIHTemplateCandidates)+2)
	if exePath, err := os.Executable(); err == nil {
		resolved := exePath
		if symlinkResolved, err := filepath.EvalSymlinks(exePath); err == nil {
			resolved = symlinkResolved
		}
		binDir := filepath.Dir(resolved)
		candidates = append(candidates,
			filepath.Join(binDir, "..", "share", "sciclaw", "templates", "nih-standard.docx"),
			filepath.Join(binDir, "..", "..", "share", "sciclaw", "templates", "nih-standard.docx"),
		)
	}
	candidates = append(candidates, sciclawNIHTemplateCandidates...)
	return candidates
}

func defaultNIHTemplatePath() string {
	home := strings.TrimSpace(os.Getenv("PICOCLAW_HOME"))
	if home == "" {
		userHome, err := os.UserHomeDir()
		if err != nil || strings.TrimSpace(userHome) == "" {
			return ""
		}
		home = filepath.Join(userHome, ".picoclaw")
	}
	return filepath.Join(home, "templates", "nih-standard.docx")
}

func ensureEmbeddedNIHTemplate(destPath string) error {
	if strings.TrimSpace(destPath) == "" {
		return fmt.Errorf("empty NIH template destination")
	}
	if len(embeddedNIHTemplate) == 0 {
		return fmt.Errorf("embedded NIH template is missing")
	}
	if isRegularFile(destPath) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return err
	}
	return os.WriteFile(destPath, embeddedNIHTemplate, 0o644)
}

func copyFileContents(srcPath, dstPath string, mode os.FileMode) error {
	content, err := os.ReadFile(srcPath)
	if err != nil {
		return err
	}
	if len(content) == 0 {
		return fmt.Errorf("source file is empty: %s", srcPath)
	}
	if err := os.MkdirAll(filepath.Dir(dstPath), 0o755); err != nil {
		return err
	}
	return os.WriteFile(dstPath, content, mode)
}

func ensurePandocDefaultsFile(templatePath string) (string, error) {
	defaultsPath := strings.TrimSpace(os.Getenv("SCICLAW_PANDOC_DEFAULTS_PATH"))
	if defaultsPath == "" {
		defaultsPath = filepath.Join(os.TempDir(), "sciclaw-pandoc-defaults.yaml")
	}
	if err := os.MkdirAll(filepath.Dir(defaultsPath), 0o755); err != nil {
		return "", err
	}

	content := []byte("reference-doc: " + strconv.Quote(templatePath) + "\n")
	if existing, err := os.ReadFile(defaultsPath); err == nil {
		if bytes.Equal(existing, content) {
			return defaultsPath, nil
		}
	}

	if err := os.WriteFile(defaultsPath, content, 0o600); err != nil {
		return "", err
	}
	return defaultsPath, nil
}

func isRegularFile(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir()
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
		pathGuardInput := stripHeredocSegments(cmd)

		if strings.Contains(pathGuardInput, "..\\") || strings.Contains(pathGuardInput, "../") {
			return "Command blocked by safety guard (path traversal detected)"
		}

		cwdPath, err := filepath.Abs(cwd)
		if err != nil {
			return ""
		}
		sharedRoot := absCleanPath(t.sharedWorkspace)
		cwdInSharedRoot := sharedRoot != "" && isWithinWorkspace(cwdPath, sharedRoot)
		if cwdInSharedRoot && t.sharedWorkspaceReadOnly && looksMutatingCommand(cmd) {
			return "Command blocked by safety guard (shared workspace is read-only)"
		}

		pathScanInput := pathGuardInput
		// PubMed search syntax uses field tags like [Title/Abstract], which can be
		// misread as absolute paths by the generic scanner. Strip bracketed tags
		// for path scanning only, while still keeping other safety checks active.
		if isPubMedCommand(cmd) {
			pathScanInput = stripBracketSegments(cmd)
		}
		// URL literals are not filesystem paths and should not trigger
		// workspace path checks.
		pathScanInput = stripURLSegments(pathScanInput)
		// Also strip relative path patterns like ./ and ../ which can cause
		// false positives (e.g., './backups/*' would extract '/backups/*')
		pathScanInput = stripRelativePathPrefixes(pathScanInput)
		matches := shellPathPattern.FindAllString(pathScanInput, -1)
		type guardRoot struct {
			path     string
			readOnly bool
		}
		allowedRoots := []guardRoot{{path: cwdPath, readOnly: cwdInSharedRoot && t.sharedWorkspaceReadOnly}}
		if sharedRoot != "" && !isWithinWorkspace(cwdPath, sharedRoot) {
			allowedRoots = append(allowedRoots, guardRoot{path: sharedRoot, readOnly: t.sharedWorkspaceReadOnly})
		}

		for _, raw := range matches {
			p, err := filepath.Abs(raw)
			if err != nil {
				continue
			}
			var matchedRoot *guardRoot
			for i := range allowedRoots {
				root := allowedRoots[i]
				if isWithinWorkspace(p, root.path) {
					matchedRoot = &allowedRoots[i]
					break
				}
			}

			if matchedRoot == nil {
				if !isAllowedOutsideWorkspacePath(p) {
					return "Command blocked by safety guard (path outside working dir)"
				}
				continue
			}
			if matchedRoot.readOnly && looksMutatingCommand(cmd) {
				return "Command blocked by safety guard (shared workspace is read-only)"
			}
		}
	}

	return ""
}

func looksMutatingCommand(command string) bool {
	cmd := strings.TrimSpace(strings.ToLower(command))
	if cmd == "" {
		return false
	}
	if shellMutatingCommandPattern.MatchString(cmd) {
		return true
	}
	// Treat non-fd redirections as potentially mutating.
	if shellWriteRedirectPattern.MatchString(cmd) {
		return true
	}
	// Common write-style flags used by CLI tools.
	if strings.Contains(cmd, "--output") || strings.Contains(cmd, " -o ") || strings.Contains(cmd, "--ris ") || strings.HasSuffix(cmd, "--ris") {
		return true
	}
	return false
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
	return shellURLPattern.ReplaceAllString(s, " ")
}

// stripRelativePathPrefixes replaces relative path patterns like ./ and ../
// with spaces to prevent false positive path detection. For example,
// './backups/*' would otherwise match '/backups/*' as an absolute path.
func stripRelativePathPrefixes(s string) string {
	// Replace ./ and ../ patterns (with their following path) with spaces
	// This handles cases like './foo', '../bar', './*', etc.
	result := regexp.MustCompile(`\.\.?/[^\s\"']*`).ReplaceAllStringFunc(s, func(match string) string {
		return strings.Repeat(" ", len(match))
	})
	return result
}

func isAllowedOutsideWorkspacePath(path string) bool {
	p := absCleanPath(path)
	if p == "" {
		return false
	}

	// Standard null/std streams are safe redirection targets.
	switch p {
	case "/dev/null", "/dev/stdout", "/dev/stderr", "/dev/stdin":
		return true
	}

	for _, root := range allowedOutsideWorkspaceRoots() {
		if pathWithinRoot(p, root) {
			return true
		}
	}
	return false
}

func allowedOutsideWorkspaceRoots() []string {
	roots := []string{os.TempDir(), "/tmp", "/var/tmp", "/private/tmp"}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(roots))
	for _, root := range roots {
		clean := absCleanPath(root)
		if clean == "" {
			continue
		}
		if _, ok := seen[clean]; ok {
			continue
		}
		seen[clean] = struct{}{}
		out = append(out, clean)
	}
	return out
}

func pathWithinRoot(path, root string) bool {
	if path == root {
		return true
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func absCleanPath(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	if abs, err := filepath.Abs(p); err == nil {
		p = abs
	}
	return filepath.Clean(p)
}

func stripHeredocSegments(s string) string {
	lines := strings.Split(s, "\n")
	if len(lines) == 1 {
		return s
	}

	out := make([]string, 0, len(lines))
	inHeredoc := false
	endMarker := ""

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		if inHeredoc {
			if trimmed == endMarker {
				inHeredoc = false
				endMarker = ""
				out = append(out, line)
			} else {
				out = append(out, "")
			}
			continue
		}

		out = append(out, line)
		if marker, ok := heredocMarkerFromLine(line); ok {
			inHeredoc = true
			endMarker = marker
		}
	}

	return strings.Join(out, "\n")
}

func heredocMarkerFromLine(line string) (string, bool) {
	idx := strings.Index(line, "<<")
	if idx < 0 {
		return "", false
	}
	rest := strings.TrimSpace(line[idx+2:])
	if strings.HasPrefix(rest, "-") {
		rest = strings.TrimSpace(rest[1:])
	}
	if rest == "" {
		return "", false
	}

	if rest[0] == '\'' || rest[0] == '"' {
		quote := rest[0]
		rest = rest[1:]
		end := strings.IndexByte(rest, quote)
		if end <= 0 {
			return "", false
		}
		return rest[:end], true
	}

	fields := strings.Fields(rest)
	if len(fields) == 0 {
		return "", false
	}
	marker := strings.Trim(fields[0], `"'`)
	if marker == "" {
		return "", false
	}
	return marker, true
}

func (t *ExecTool) SetTimeout(timeout time.Duration) {
	t.timeout = timeout
}

func (t *ExecTool) SetRestrictToWorkspace(restrict bool) {
	t.restrictToWorkspace = restrict
}

func (t *ExecTool) SetSharedWorkspacePolicy(sharedWorkspace string, sharedWorkspaceReadOnly bool) {
	t.sharedWorkspace = strings.TrimSpace(sharedWorkspace)
	t.sharedWorkspaceReadOnly = sharedWorkspaceReadOnly
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
