package irl

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
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/sipeed/picoclaw/pkg/logger"
)

const (
	defaultCommandTimeout = 90 * time.Second
	maxStoredOutputChars  = 20000
)

type CreateProjectRequest struct {
	Purpose  string
	Template string
	Name     string
	Dir      string
}

type AdoptProjectRequest struct {
	SourcePath string
	Rename     bool
	Template   string
}

type ClientOptions struct {
	NowFn            func() time.Time
	LookPathFn       func(file string) (string, error)
	BinaryCandidates []string
}

type Client struct {
	workspace        string
	store            *commandStore
	nowFn            func() time.Time
	lookPathFn       func(file string) (string, error)
	binaryCandidates []string
}

func NewClient(workspace string) *Client {
	return NewClientWithOptions(workspace, ClientOptions{})
}

// ResolveBinaryPath returns the resolved path to the `irl` binary, using PATH
// first and then OS-specific fallback candidates (including Homebrew paths).
// This is safe to call from commands like `sciclaw status` that need to detect
// IRL availability even when PATH is minimal (e.g., daemons/nohup).
func (c *Client) ResolveBinaryPath() (string, error) {
	return c.resolveBinaryPath()
}

func NewClientWithOptions(workspace string, opts ClientOptions) *Client {
	nowFn := opts.NowFn
	if nowFn == nil {
		nowFn = time.Now
	}
	lookPathFn := opts.LookPathFn
	if lookPathFn == nil {
		lookPathFn = exec.LookPath
	}
	binaryCandidates := opts.BinaryCandidates
	if len(binaryCandidates) == 0 {
		binaryCandidates = defaultIRLBinaryCandidates()
	}

	return &Client{
		workspace:        workspace,
		store:            newCommandStore(workspace).withNow(nowFn),
		nowFn:            nowFn,
		lookPathFn:       lookPathFn,
		binaryCandidates: binaryCandidates,
	}
}

func (c *Client) CreateProject(ctx context.Context, req CreateProjectRequest) (*OperationResult, error) {
	purpose := strings.TrimSpace(req.Purpose)
	name := strings.TrimSpace(req.Name)
	if purpose == "" && name == "" {
		return nil, fmt.Errorf("purpose or name is required")
	}

	args := []string{"init"}
	if purpose != "" {
		args = append(args, purpose)
	}
	if name != "" {
		args = append(args, "-n", name)
	}
	if template := strings.TrimSpace(req.Template); template != "" {
		args = append(args, "-t", template)
	}
	if dir := strings.TrimSpace(req.Dir); dir != "" {
		args = append(args, "-d", dir)
	}

	rec := c.runCommand(ctx, "create_project", args, 1, false)
	res := &OperationResult{
		Operation: "create_project",
		Status:    rec.Status,
		Data: map[string]interface{}{
			"stdout": trimForResult(rec.Stdout),
			"stderr": trimForResult(rec.Stderr),
		},
		Commands: summarizeRecords([]CommandRecord{rec}),
	}
	if rec.Status != StatusSuccess {
		return res, fmt.Errorf("create_project failed: %s", rec.Error)
	}
	return res, nil
}

func (c *Client) AdoptProject(ctx context.Context, req AdoptProjectRequest) (*OperationResult, error) {
	sourcePath := strings.TrimSpace(req.SourcePath)
	if sourcePath == "" {
		return nil, fmt.Errorf("source_path is required")
	}

	args := []string{"adopt", sourcePath}
	if req.Rename {
		args = append(args, "--rename")
	}
	if template := strings.TrimSpace(req.Template); template != "" {
		args = append(args, "-t", template)
	}

	rec := c.runCommand(ctx, "adopt_project", args, 1, false)
	res := &OperationResult{
		Operation: "adopt_project",
		Status:    rec.Status,
		Data: map[string]interface{}{
			"stdout": trimForResult(rec.Stdout),
			"stderr": trimForResult(rec.Stderr),
		},
		Commands: summarizeRecords([]CommandRecord{rec}),
	}
	if rec.Status != StatusSuccess {
		return res, fmt.Errorf("adopt_project failed: %s", rec.Error)
	}
	return res, nil
}

func (c *Client) DiscoverProjects(ctx context.Context) (*OperationResult, error) {
	parsed, records, err := c.runJSONCommandWithRetry(ctx, "discover_projects", []string{"list", "--json"})
	if err != nil && isDiscoverUnsupported(records) {
		fallbackProjects := c.discoverProjectsFromFilesystem()
		status := StatusPartial
		if len(records) == 0 {
			status = StatusSuccess
		}
		return &OperationResult{
			Operation: "discover_projects",
			Status:    status,
			Data: map[string]interface{}{
				"projects": fallbackProjects,
				"source":   "filesystem_fallback",
			},
			Commands: summarizeRecords(records),
		}, nil
	}

	res := &OperationResult{
		Operation: "discover_projects",
		Status:    aggregateStatus(records),
		Data: map[string]interface{}{
			"projects": parsed,
		},
		Commands: summarizeRecords(records),
	}
	if err != nil {
		return res, err
	}
	return res, nil
}

func (c *Client) GetWorkspaceContext(ctx context.Context) (*OperationResult, error) {
	configPayload, configRecords, configErr := c.runJSONCommandWithRetry(ctx, "get_workspace_context.config", []string{"config", "--json"})
	profilePayload, profileRecords, profileErr := c.runJSONCommandWithRetry(ctx, "get_workspace_context.profile", []string{"profile", "--json"})

	records := append(configRecords, profileRecords...)
	res := &OperationResult{
		Operation: "get_workspace_context",
		Status:    aggregateStatus(records),
		Data: map[string]interface{}{
			"config":  configPayload,
			"profile": profilePayload,
		},
		Commands: summarizeRecords(records),
	}

	if configErr != nil || profileErr != nil {
		errMsg := "get_workspace_context failed"
		if configErr != nil {
			errMsg += ": config error: " + configErr.Error()
		}
		if profileErr != nil {
			if configErr != nil {
				errMsg += ";"
			} else {
				errMsg += ":"
			}
			errMsg += " profile error: " + profileErr.Error()
		}
		return res, errors.New(errMsg)
	}
	return res, nil
}

func (c *Client) runJSONCommandWithRetry(ctx context.Context, operation string, args []string) (interface{}, []CommandRecord, error) {
	first := c.runCommand(ctx, operation, args, 1, true)
	records := []CommandRecord{first}
	if first.Status == StatusSuccess {
		return first.Parsed, records, nil
	}
	if first.Status == StatusFailure {
		return nil, records, errors.New(first.Error)
	}

	// JSON parse was partial (command succeeded but payload parsing failed). Retry once.
	logger.WarnCF("irl", "IRL JSON parse failed, retrying once", map[string]interface{}{
		"event_id":  first.EventID,
		"operation": operation,
		"error":     first.Error,
	})
	second := c.runCommand(ctx, operation, args, 2, true)
	records = append(records, second)
	if second.Status == StatusSuccess {
		return second.Parsed, records, nil
	}
	return nil, records, errors.New(second.Error)
}

func (c *Client) runCommand(ctx context.Context, operation string, args []string, attempt int, parseJSON bool) CommandRecord {
	start := c.nowFn().UTC()
	record := CommandRecord{
		EventType: "irl_command",
		EventID:   uuid.NewString(),
		Timestamp: start.Format(time.RFC3339),
		Operation: operation,
		Attempt:   attempt,
		Command:   append([]string{"irl"}, args...),
		CWD:       c.resolveWorkingDir(),
		ExitCode:  -1,
		Status:    StatusFailure,
	}

	binaryPath, err := c.resolveBinaryPath()
	if err != nil {
		record.Error = err.Error()
		c.persistRecord(&record)
		logger.ErrorCF("irl", "IRL command failed (binary missing)", map[string]interface{}{
			"event_id":  record.EventID,
			"operation": operation,
			"error":     record.Error,
		})
		return record
	}
	record.IRLPath = binaryPath
	record.IRLVersion = c.detectVersion(binaryPath)

	logger.InfoCF("irl", "Executing IRL command", map[string]interface{}{
		"event_id":     record.EventID,
		"operation":    operation,
		"attempt":      attempt,
		"command_argv": record.Command,
		"cwd":          record.CWD,
	})

	cmdCtx, cancel := context.WithTimeout(ctx, defaultCommandTimeout)
	defer cancel()

	cmd := exec.CommandContext(cmdCtx, binaryPath, args...)
	cmd.Dir = record.CWD

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	runErr := cmd.Run()
	record.DurationMs = time.Since(start).Milliseconds()
	record.Stdout = trimForStore(stdout.String())
	record.Stderr = trimForStore(stderr.String())

	if runErr != nil {
		record.ExitCode = exitCodeFromErr(runErr)
		record.Error = fmt.Sprintf("command failed: %v", runErr)
		record.Status = StatusFailure
	} else {
		record.ExitCode = 0
		record.Status = StatusSuccess
	}

	if parseJSON && record.ExitCode == 0 {
		var parsed interface{}
		if err := json.Unmarshal([]byte(strings.TrimSpace(record.Stdout)), &parsed); err != nil {
			record.Status = StatusPartial
			record.Error = fmt.Sprintf("json parse failed: %v", err)
		} else {
			record.Parsed = parsed
		}
	}

	c.persistRecord(&record)
	if record.Status == StatusFailure {
		logger.ErrorCF("irl", "IRL command failed", map[string]interface{}{
			"event_id":    record.EventID,
			"operation":   record.Operation,
			"attempt":     record.Attempt,
			"exit_code":   record.ExitCode,
			"duration_ms": record.DurationMs,
			"store_path":  record.StorePath,
			"error":       record.Error,
		})
	} else {
		logger.InfoCF("irl", "IRL command completed", map[string]interface{}{
			"event_id":    record.EventID,
			"operation":   record.Operation,
			"attempt":     record.Attempt,
			"exit_code":   record.ExitCode,
			"status":      record.Status,
			"duration_ms": record.DurationMs,
			"store_path":  record.StorePath,
		})
	}

	return record
}

func (c *Client) persistRecord(record *CommandRecord) {
	storePath, err := c.store.write(record)
	if err != nil {
		record.StoreError = err.Error()
		logger.ErrorCF("irl", "Failed to write IRL command store record", map[string]interface{}{
			"event_id":  record.EventID,
			"operation": record.Operation,
			"error":     err.Error(),
		})
		return
	}
	record.StorePath = filepath.ToSlash(storePath)
}

func (c *Client) resolveWorkingDir() string {
	if strings.TrimSpace(c.workspace) != "" {
		return c.workspace
	}
	if wd, err := os.Getwd(); err == nil {
		return wd
	}
	return "."
}

func (c *Client) detectVersion(binaryPath string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, binaryPath, "--version")
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		return ""
	}
	return strings.TrimSpace(trimForStore(stdout.String()))
}

func (c *Client) resolveBinaryPath() (string, error) {
	if c.lookPathFn != nil {
		if binaryPath, err := c.lookPathFn("irl"); err == nil && strings.TrimSpace(binaryPath) != "" {
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
		if isExecutableFile(candidate) {
			return candidate, nil
		}
	}

	if len(checked) == 0 {
		return "", errors.New(`irl binary not found in PATH (no fallback candidates configured)`)
	}
	return "", fmt.Errorf(`irl binary not found in PATH or fallback paths: %s`, strings.Join(checked, ", "))
}

func defaultIRLBinaryCandidates() []string {
	seen := map[string]struct{}{}
	add := func(out []string, v string) []string {
		v = strings.TrimSpace(v)
		if v == "" {
			return out
		}
		if _, exists := seen[v]; exists {
			return out
		}
		seen[v] = struct{}{}
		return append(out, v)
	}

	candidates := make([]string, 0, 4)
	candidates = add(candidates, os.Getenv("PICOCLAW_IRL_BINARY"))

	binName := "irl"
	if runtime.GOOS == "windows" {
		binName = "irl.exe"
	}

	if prefix := strings.TrimSpace(os.Getenv("HOMEBREW_PREFIX")); prefix != "" {
		candidates = add(candidates, filepath.Join(prefix, "bin", binName))
	}

	switch runtime.GOOS {
	case "darwin":
		candidates = add(candidates, filepath.Join("/opt/homebrew/bin", binName))
		candidates = add(candidates, filepath.Join("/usr/local/bin", binName))
	case "linux":
		candidates = add(candidates, filepath.Join("/usr/local/bin", binName))
		candidates = add(candidates, filepath.Join("/usr/bin", binName))
	case "windows":
		if local := strings.TrimSpace(os.Getenv("LOCALAPPDATA")); local != "" {
			candidates = add(candidates, filepath.Join(local, "Programs", "irl", binName))
		}
		if pf := strings.TrimSpace(os.Getenv("ProgramFiles")); pf != "" {
			candidates = add(candidates, filepath.Join(pf, "irl", binName))
		}
		if pfx86 := strings.TrimSpace(os.Getenv("ProgramFiles(x86)")); pfx86 != "" {
			candidates = add(candidates, filepath.Join(pfx86, "irl", binName))
		}
	}
	return candidates
}

func isExecutableFile(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	if info.IsDir() {
		return false
	}
	return info.Mode().Perm()&0111 != 0
}

func exitCodeFromErr(err error) int {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return 124
	}
	return -1
}

func trimForStore(s string) string {
	s = strings.TrimSpace(s)
	if len(s) <= maxStoredOutputChars {
		return s
	}
	return s[:maxStoredOutputChars] + fmt.Sprintf("\n... (truncated %d chars)", len(s)-maxStoredOutputChars)
}

func trimForResult(s string) string {
	const max = 4000
	s = strings.TrimSpace(s)
	if len(s) <= max {
		return s
	}
	return s[:max] + fmt.Sprintf("\n... (truncated %d chars)", len(s)-max)
}

func isDiscoverUnsupported(records []CommandRecord) bool {
	if len(records) == 0 {
		return false
	}
	last := records[len(records)-1]
	combined := strings.ToLower(strings.TrimSpace(last.Stderr + "\n" + last.Error))
	if combined == "" {
		return false
	}
	if strings.Contains(combined, `unknown command "list"`) {
		return true
	}
	if strings.Contains(combined, "unknown flag: --json") {
		return true
	}
	if strings.Contains(combined, "/dev/tty") {
		return true
	}
	return false
}

func (c *Client) discoverProjectsFromFilesystem() []map[string]interface{} {
	roots := c.defaultProjectRoots()
	seen := map[string]struct{}{}
	projects := make([]map[string]interface{}, 0, 8)

	for _, root := range roots {
		if root == "" {
			continue
		}
		entries, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			projectPath := filepath.Join(root, entry.Name())
			planPath := filepath.Join(projectPath, "plans", "main-plan.md")
			if _, err := os.Stat(planPath); err != nil {
				continue
			}
			normalizedPath := filepath.Clean(projectPath)
			if _, exists := seen[normalizedPath]; exists {
				continue
			}
			seen[normalizedPath] = struct{}{}
			projects = append(projects, map[string]interface{}{
				"name": entry.Name(),
				"path": filepath.ToSlash(normalizedPath),
			})
		}
	}

	sort.Slice(projects, func(i, j int) bool {
		return fmt.Sprintf("%v", projects[i]["name"]) < fmt.Sprintf("%v", projects[j]["name"])
	})
	return projects
}

func (c *Client) defaultProjectRoots() []string {
	roots := make([]string, 0, 3)
	seen := map[string]struct{}{}
	add := func(path string) {
		path = strings.TrimSpace(path)
		if path == "" {
			return
		}
		path = filepath.Clean(path)
		if _, ok := seen[path]; ok {
			return
		}
		seen[path] = struct{}{}
		roots = append(roots, path)
	}

	add(c.workspace)
	if home, err := os.UserHomeDir(); err == nil {
		add(filepath.Join(home, "Documents", "irl_projects"))
	}
	return roots
}

func summarizeRecords(records []CommandRecord) []CommandSummary {
	out := make([]CommandSummary, 0, len(records))
	for _, r := range records {
		out = append(out, CommandSummary{
			EventID:    r.EventID,
			Operation:  r.Operation,
			Attempt:    r.Attempt,
			Command:    r.Command,
			ExitCode:   r.ExitCode,
			Status:     r.Status,
			StorePath:  r.StorePath,
			Error:      r.Error,
			StoreError: r.StoreError,
		})
	}
	return out
}

func aggregateStatus(records []CommandRecord) CommandStatus {
	if len(records) == 0 {
		return StatusFailure
	}

	overall := StatusSuccess
	for _, record := range records {
		if record.Status == StatusFailure {
			return StatusFailure
		}
		if record.Status == StatusPartial {
			overall = StatusPartial
		}
	}
	return overall
}
