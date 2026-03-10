package docxreview

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

type RunResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

type RunFunc func(ctx context.Context, binary string, args []string) (RunResult, error)

type ClientOptions struct {
	LookPathFn       func(string) (string, error)
	BinaryCandidates []string
	RunFn            RunFunc
	Timeout          time.Duration
}

type Client struct {
	lookPathFn       func(string) (string, error)
	binaryCandidates []string
	run              RunFunc
	timeout          time.Duration
}

type CLIError struct {
	Binary   string
	Args     []string
	ExitCode int
	Stdout   string
	Stderr   string
}

func (e *CLIError) Error() string {
	if e == nil {
		return "docx-review command failed"
	}
	source := strings.TrimSpace(e.Stderr)
	if source == "" {
		source = strings.TrimSpace(e.Stdout)
	}
	if source == "" {
		return fmt.Sprintf("docx-review failed with exit code %d", e.ExitCode)
	}
	if len(source) > 400 {
		source = source[:400] + "... (truncated)"
	}
	return fmt.Sprintf("docx-review failed with exit code %d: %s", e.ExitCode, source)
}

func NewClient() *Client {
	return NewClientWithOptions(ClientOptions{})
}

func NewClientWithOptions(opts ClientOptions) *Client {
	lookPathFn := opts.LookPathFn
	if lookPathFn == nil {
		lookPathFn = exec.LookPath
	}
	binaryCandidates := opts.BinaryCandidates
	if len(binaryCandidates) == 0 {
		binaryCandidates = defaultBinaryCandidates()
	}
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = 90 * time.Second
	}
	runFn := opts.RunFn
	if runFn == nil {
		runFn = defaultRun
	}
	return &Client{
		lookPathFn:       lookPathFn,
		binaryCandidates: binaryCandidates,
		run:              runFn,
		timeout:          timeout,
	}
}

func newClientWithRunner(opts ClientOptions, runner RunFunc) *Client {
	c := NewClientWithOptions(opts)
	if runner != nil {
		c.run = runner
	}
	return c
}

func (c *Client) ResolveBinaryPath() (string, error) {
	if explicit := strings.TrimSpace(os.Getenv("PICOCLAW_DOCX_REVIEW_BINARY")); explicit != "" {
		if isExecutableBinary(explicit) {
			return explicit, nil
		}
	}

	if c.lookPathFn != nil {
		if binaryPath, err := c.lookPathFn("docx-review"); err == nil && strings.TrimSpace(binaryPath) != "" {
			return binaryPath, nil
		}
	}

	checked := make([]string, 0, len(c.binaryCandidates))
	for _, candidate := range c.binaryCandidates {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		checked = append(checked, candidate)
		if isExecutableBinary(candidate) {
			return candidate, nil
		}
	}

	if len(checked) == 0 {
		return "", errors.New("docx-review binary not found in PATH (no fallback candidates configured)")
	}
	return "", fmt.Errorf("docx-review binary not found in PATH or fallback paths: %s (install: brew tap drpedapati/tap && brew install sciclaw-docx-review)", strings.Join(checked, ", "))
}

func (c *Client) Read(ctx context.Context, inputPath string) (*ReadResult, error) {
	inputPath = strings.TrimSpace(inputPath)
	if inputPath == "" {
		return nil, fmt.Errorf("input path is required")
	}
	if err := ensureRegularFile(inputPath, "input"); err != nil {
		return nil, err
	}
	var result ReadResult
	if _, err := c.runJSON(ctx, []string{inputPath, "--read", "--json"}, &result, false); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *Client) Diff(ctx context.Context, oldPath, newPath string) (*DiffResult, error) {
	oldPath = strings.TrimSpace(oldPath)
	newPath = strings.TrimSpace(newPath)
	if oldPath == "" {
		return nil, fmt.Errorf("old path is required")
	}
	if newPath == "" {
		return nil, fmt.Errorf("new path is required")
	}
	if err := ensureRegularFile(oldPath, "old"); err != nil {
		return nil, err
	}
	if err := ensureRegularFile(newPath, "new"); err != nil {
		return nil, err
	}
	var result DiffResult
	if _, err := c.runJSON(ctx, []string{"--diff", oldPath, newPath, "--json"}, &result, false); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *Client) Apply(ctx context.Context, req ApplyRequest) (*ApplyResult, error) {
	req.InputPath = strings.TrimSpace(req.InputPath)
	req.ManifestPath = strings.TrimSpace(req.ManifestPath)
	req.OutputPath = strings.TrimSpace(req.OutputPath)
	req.Author = strings.TrimSpace(req.Author)
	if req.InputPath == "" {
		return nil, fmt.Errorf("input path is required")
	}
	if req.ManifestPath == "" {
		return nil, fmt.Errorf("manifest path is required")
	}
	if !req.DryRun && req.OutputPath == "" {
		return nil, fmt.Errorf("output path is required unless dry_run is true")
	}
	if req.OutputPath != "" && pathsReferToSameFile(req.InputPath, req.OutputPath) {
		return nil, fmt.Errorf("output path must differ from source document")
	}
	if err := ensureRegularFile(req.InputPath, "input"); err != nil {
		return nil, err
	}
	if err := ensureRegularFile(req.ManifestPath, "manifest"); err != nil {
		return nil, err
	}

	args := []string{req.InputPath, req.ManifestPath, "--json"}
	if req.OutputPath != "" {
		args = append(args, "--output", req.OutputPath)
	}
	if req.Author != "" {
		args = append(args, "--author", req.Author)
	}
	if req.DryRun {
		args = append(args, "--dry-run")
	}
	if req.AcceptExisting {
		args = append(args, "--accept-existing")
	} else {
		args = append(args, "--no-accept-existing")
	}

	beforeInfo, beforeErr := os.Stat(req.OutputPath)
	var parsed ProcessingResult
	res, err := c.runJSON(ctx, args, &parsed, true)
	if err != nil {
		return nil, err
	}

	result := &ApplyResult{
		ProcessingResult: parsed,
		Status:           statusFromExitCode(res.ExitCode),
		ExitCode:         res.ExitCode,
		DryRun:           req.DryRun,
		AcceptExisting:   req.AcceptExisting,
	}

	if req.DryRun || req.OutputPath == "" {
		return result, nil
	}

	afterInfo, statErr := os.Stat(req.OutputPath)
	if statErr == nil {
		result.OutputWritten = beforeErr != nil || !sameFileSnapshot(beforeInfo, afterInfo)
	}

	if expectsOutputFile(result) {
		if statErr != nil {
			return nil, fmt.Errorf("docx-review completed without producing output file %q: %w", req.OutputPath, statErr)
		}
		if beforeErr == nil && sameFileSnapshot(beforeInfo, afterInfo) {
			return nil, fmt.Errorf("docx-review reported changes but did not update output file %q", req.OutputPath)
		}
		result.OutputWritten = true
	}

	return result, nil
}

func (c *Client) runJSON(ctx context.Context, args []string, dst interface{}, allowJSONOnExitOne bool) (RunResult, error) {
	binaryPath, err := c.ResolveBinaryPath()
	if err != nil {
		return RunResult{}, err
	}

	runCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	res, runErr := c.run(runCtx, binaryPath, args)
	if runErr != nil {
		if allowJSONOnExitOne && res.ExitCode == 1 {
			if jsonErr := json.Unmarshal([]byte(res.Stdout), dst); jsonErr == nil {
				return res, nil
			}
		}
		if res.ExitCode > 0 {
			return res, &CLIError{Binary: binaryPath, Args: append([]string{}, args...), ExitCode: res.ExitCode, Stdout: res.Stdout, Stderr: res.Stderr}
		}
		return res, fmt.Errorf("docx-review invocation failed: %w", runErr)
	}
	if res.ExitCode != 0 {
		if allowJSONOnExitOne && res.ExitCode == 1 {
			if jsonErr := json.Unmarshal([]byte(res.Stdout), dst); jsonErr == nil {
				return res, nil
			}
		}
		return res, &CLIError{Binary: binaryPath, Args: append([]string{}, args...), ExitCode: res.ExitCode, Stdout: res.Stdout, Stderr: res.Stderr}
	}
	if err := json.Unmarshal([]byte(res.Stdout), dst); err != nil {
		return res, fmt.Errorf("docx-review returned invalid JSON: %w", err)
	}
	return res, nil
}

func defaultRun(ctx context.Context, binary string, args []string) (RunResult, error) {
	cmd := exec.CommandContext(ctx, binary, args...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	result := RunResult{Stdout: stdout.String(), Stderr: stderr.String(), ExitCode: 0}
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			result.ExitCode = exitErr.ExitCode()
		} else {
			result.ExitCode = -1
		}
		return result, err
	}
	return result, nil
}

func defaultBinaryCandidates() []string {
	seen := map[string]struct{}{}
	add := func(out []string, v string) []string {
		v = strings.TrimSpace(v)
		if v == "" {
			return out
		}
		if _, ok := seen[v]; ok {
			return out
		}
		seen[v] = struct{}{}
		return append(out, v)
	}

	binName := "docx-review"
	if runtime.GOOS == "windows" {
		binName = "docx-review.exe"
	}

	candidates := make([]string, 0, 6)
	candidates = add(candidates, os.Getenv("PICOCLAW_DOCX_REVIEW_BINARY"))
	if prefix := strings.TrimSpace(os.Getenv("HOMEBREW_PREFIX")); prefix != "" {
		candidates = add(candidates, filepath.Join(prefix, "bin", binName))
	}

	switch runtime.GOOS {
	case "darwin":
		candidates = add(candidates, filepath.Join("/opt/homebrew/bin", binName))
		candidates = add(candidates, filepath.Join("/usr/local/bin", binName))
	case "linux":
		candidates = add(candidates, filepath.Join("/home/linuxbrew/.linuxbrew/bin", binName))
		candidates = add(candidates, filepath.Join("/usr/local/bin", binName))
		candidates = add(candidates, filepath.Join("/usr/bin", binName))
	case "windows":
		if local := strings.TrimSpace(os.Getenv("LOCALAPPDATA")); local != "" {
			candidates = add(candidates, filepath.Join(local, "Programs", "docx-review", binName))
		}
		if pf := strings.TrimSpace(os.Getenv("ProgramFiles")); pf != "" {
			candidates = add(candidates, filepath.Join(pf, "docx-review", binName))
		}
		if pfx86 := strings.TrimSpace(os.Getenv("ProgramFiles(x86)")); pfx86 != "" {
			candidates = add(candidates, filepath.Join(pfx86, "docx-review", binName))
		}
	}
	return candidates
}

func statusFromExitCode(code int) string {
	if code == 1 {
		return "partial"
	}
	return "ok"
}

func expectsOutputFile(result *ApplyResult) bool {
	if result == nil || result.DryRun {
		return false
	}
	if result.Success {
		return true
	}
	return result.ChangesSucceeded > 0 || result.CommentsSucceeded > 0
}

func isExecutableBinary(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	if info.IsDir() {
		return false
	}
	if runtime.GOOS == "windows" {
		switch strings.ToLower(filepath.Ext(path)) {
		case ".exe", ".bat", ".cmd":
			return true
		}
	}
	return info.Mode().Perm()&0o111 != 0
}

func sameFileSnapshot(before, after os.FileInfo) bool {
	if before == nil || after == nil {
		return false
	}
	return before.Size() == after.Size() && before.ModTime().Equal(after.ModTime())
}

func ensureRegularFile(path string, label string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("%s file not found: %s", label, path)
	}
	if info.IsDir() {
		return fmt.Errorf("%s path is a directory, expected file: %s", label, path)
	}
	return nil
}

func pathsReferToSameFile(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	absA, errA := filepath.Abs(a)
	absB, errB := filepath.Abs(b)
	if errA != nil || errB != nil {
		return filepath.Clean(a) == filepath.Clean(b)
	}
	return filepath.Clean(absA) == filepath.Clean(absB)
}
