package pdfform

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
		return "pdf-form-filler command failed"
	}
	source := strings.TrimSpace(e.Stderr)
	if source == "" {
		source = strings.TrimSpace(e.Stdout)
	}
	if source == "" {
		return fmt.Sprintf("pdf-form-filler failed with exit code %d", e.ExitCode)
	}
	if len(source) > 400 {
		source = source[:400] + "... (truncated)"
	}
	return fmt.Sprintf("pdf-form-filler failed with exit code %d: %s", e.ExitCode, source)
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
	return &Client{
		lookPathFn:       lookPathFn,
		binaryCandidates: binaryCandidates,
		run:              defaultRun,
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
	if c.lookPathFn != nil {
		if binaryPath, err := c.lookPathFn("pdf-form-filler"); err == nil && strings.TrimSpace(binaryPath) != "" {
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
		return "", errors.New("pdf-form-filler binary not found in PATH (no fallback candidates configured)")
	}
	return "", fmt.Errorf("pdf-form-filler binary not found in PATH or fallback paths: %s (install: brew install pdf-form-filler)", strings.Join(checked, ", "))
}

func (c *Client) Inspect(ctx context.Context, pdfPath string) (*Inspection, error) {
	pdfPath = strings.TrimSpace(pdfPath)
	if pdfPath == "" {
		return nil, fmt.Errorf("pdf path is required")
	}
	var inspection Inspection
	if err := c.runJSON(ctx, []string{"inspect", "--pdf", pdfPath, "--json"}, &inspection, true); err != nil {
		return nil, err
	}
	return &inspection, nil
}

func (c *Client) Schema(ctx context.Context, pdfPath string) (*Schema, error) {
	pdfPath = strings.TrimSpace(pdfPath)
	if pdfPath == "" {
		return nil, fmt.Errorf("pdf path is required")
	}
	var schema Schema
	if err := c.runJSON(ctx, []string{"schema", "--pdf", pdfPath}, &schema, false); err != nil {
		return nil, err
	}
	return &schema, nil
}

func (c *Client) Fill(ctx context.Context, req FillRequest) (*FillResult, error) {
	req.PDFPath = strings.TrimSpace(req.PDFPath)
	req.ValuesPath = strings.TrimSpace(req.ValuesPath)
	req.OutputPath = strings.TrimSpace(req.OutputPath)
	if req.PDFPath == "" {
		return nil, fmt.Errorf("pdf path is required")
	}
	if req.ValuesPath == "" {
		return nil, fmt.Errorf("values path is required")
	}
	if req.OutputPath == "" {
		return nil, fmt.Errorf("output path is required")
	}

	args := []string{"fill", "--pdf", req.PDFPath, "--values", req.ValuesPath, "--out", req.OutputPath, "--json"}
	if req.Flatten {
		args = append(args, "--flatten")
	}

	var result FillResult
	if err := c.runJSON(ctx, args, &result, false); err != nil {
		return nil, err
	}
	if _, err := os.Stat(req.OutputPath); err != nil {
		return nil, fmt.Errorf("pdf-form-filler completed without producing output file %q: %w", req.OutputPath, err)
	}
	return &result, nil
}

func (c *Client) runJSON(ctx context.Context, args []string, dst interface{}, allowJSONOnExitOne bool) error {
	binaryPath, err := c.ResolveBinaryPath()
	if err != nil {
		return err
	}

	runCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	res, runErr := c.run(runCtx, binaryPath, args)
	if runErr != nil {
		if allowJSONOnExitOne && res.ExitCode == 1 {
			if jsonErr := json.Unmarshal([]byte(res.Stdout), dst); jsonErr == nil {
				return nil
			}
		}
		if res.ExitCode > 0 {
			return &CLIError{Binary: binaryPath, Args: append([]string{}, args...), ExitCode: res.ExitCode, Stdout: res.Stdout, Stderr: res.Stderr}
		}
		return fmt.Errorf("pdf-form-filler invocation failed: %w", runErr)
	}
	if res.ExitCode != 0 {
		if allowJSONOnExitOne && res.ExitCode == 1 {
			if jsonErr := json.Unmarshal([]byte(res.Stdout), dst); jsonErr == nil {
				return nil
			}
		}
		return &CLIError{Binary: binaryPath, Args: append([]string{}, args...), ExitCode: res.ExitCode, Stdout: res.Stdout, Stderr: res.Stderr}
	}
	if err := json.Unmarshal([]byte(res.Stdout), dst); err != nil {
		return fmt.Errorf("pdf-form-filler returned invalid JSON: %w", err)
	}
	return nil
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

	binName := "pdf-form-filler"
	if runtime.GOOS == "windows" {
		binName = "pdf-form-filler.exe"
	}

	candidates := make([]string, 0, 6)
	candidates = add(candidates, os.Getenv("PICOCLAW_PDF_FORM_FILLER_BINARY"))
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
			candidates = add(candidates, filepath.Join(local, "Programs", "pdf-form-filler", binName))
		}
		if pf := strings.TrimSpace(os.Getenv("ProgramFiles")); pf != "" {
			candidates = add(candidates, filepath.Join(pf, "pdf-form-filler", binName))
		}
		if pfx86 := strings.TrimSpace(os.Getenv("ProgramFiles(x86)")); pfx86 != "" {
			candidates = add(candidates, filepath.Join(pfx86, "pdf-form-filler", binName))
		}
	}
	return candidates
}

func isExecutableBinary(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	if info.IsDir() {
		return false
	}
	return info.Mode().Perm()&0o111 != 0
}
