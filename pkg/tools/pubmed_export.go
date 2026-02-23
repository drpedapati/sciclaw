package tools

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type pubmedRunFunc func(ctx context.Context, binary string, args []string, cwd string, env map[string]string) (string, string, error)

type PubMedExportTool struct {
	workspace               string
	restrict                bool
	sharedWorkspace         string
	sharedWorkspaceReadOnly bool
	extraEnv                map[string]string
	run                     pubmedRunFunc
	findBin                 func() (string, error)
	timeout                 time.Duration
}

func NewPubMedExportTool(workspace string, restrict bool) *PubMedExportTool {
	return &PubMedExportTool{
		workspace: workspace,
		restrict:  restrict,
		extraEnv:  map[string]string{},
		run:       defaultPubMedRun,
		findBin:   findPubMedBinary,
		timeout:   90 * time.Second,
	}
}

func newPubMedExportToolWithRunner(workspace string, restrict bool, runner pubmedRunFunc) *PubMedExportTool {
	t := NewPubMedExportTool(workspace, restrict)
	if runner != nil {
		t.run = runner
	}
	return t
}

func (t *PubMedExportTool) SetSharedWorkspacePolicy(sharedWorkspace string, sharedWorkspaceReadOnly bool) {
	t.sharedWorkspace = strings.TrimSpace(sharedWorkspace)
	t.sharedWorkspaceReadOnly = sharedWorkspaceReadOnly
}

func (t *PubMedExportTool) SetExtraEnv(env map[string]string) {
	if len(env) == 0 {
		return
	}
	for k, v := range env {
		if strings.TrimSpace(k) == "" {
			continue
		}
		t.extraEnv[k] = v
	}
}

func (t *PubMedExportTool) Name() string {
	return "pubmed_export_ris"
}

func (t *PubMedExportTool) Description() string {
	return "Export PubMed citations to a RIS file without shell pipes. Use this for citation manager export workflows."
}

func (t *PubMedExportTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"pmids": map[string]interface{}{
				"type":        "array",
				"description": "List of PMID values to export",
				"items": map[string]interface{}{
					"type": "string",
				},
			},
			"output_file": map[string]interface{}{
				"type":        "string",
				"description": "Path for the RIS output file (workspace-relative by default)",
			},
			"overwrite": map[string]interface{}{
				"type":        "boolean",
				"description": "Overwrite output file if it already exists (default false)",
			},
		},
		"required": []string{"pmids", "output_file"},
	}
}

func (t *PubMedExportTool) Execute(ctx context.Context, args map[string]interface{}) *ToolResult {
	pmids, err := parsePMIDList(args["pmids"])
	if err != nil || len(pmids) == 0 {
		return ErrorResult("pmids is required and must include at least one PMID")
	}

	outputFile := getString(args, "output_file")
	if strings.TrimSpace(outputFile) == "" {
		return ErrorResult("output_file is required")
	}

	outputPath, err := validatePathWithPolicy(outputFile, t.workspace, t.restrict, AccessWrite, t.sharedWorkspace, t.sharedWorkspaceReadOnly)
	if err != nil {
		return ErrorResult(err.Error())
	}

	overwrite := getBool(args, "overwrite")
	if _, statErr := os.Stat(outputPath); statErr == nil && !overwrite {
		return ErrorResult("output_file already exists; set overwrite=true to replace it")
	}

	if mkErr := os.MkdirAll(filepath.Dir(outputPath), 0o755); mkErr != nil {
		return ErrorResult(fmt.Sprintf("failed to create output directory: %v", mkErr))
	}

	binary, err := t.findBin()
	if err != nil {
		return ErrorResult(err.Error())
	}

	cmdArgs := make([]string, 0, len(pmids)+3)
	cmdArgs = append(cmdArgs, "fetch")
	cmdArgs = append(cmdArgs, pmids...)
	cmdArgs = append(cmdArgs, "--ris", outputPath)

	runCtx, cancel := context.WithTimeout(ctx, t.timeout)
	defer cancel()

	stdout, stderr, runErr := t.run(runCtx, binary, cmdArgs, t.workspace, t.extraEnv)
	if runErr != nil {
		msg := fmt.Sprintf("pubmed export failed: %v", runErr)
		detail := strings.TrimSpace(stderr)
		if detail == "" {
			detail = strings.TrimSpace(stdout)
		}
		if detail != "" {
			msg += "\n" + truncateToolOutput(detail, 1200)
		}
		return ErrorResult(msg)
	}

	info, statErr := os.Stat(outputPath)
	if statErr != nil {
		return ErrorResult(fmt.Sprintf("pubmed export command succeeded but output file missing: %v", statErr))
	}

	return NewToolResult(mustJSON(map[string]interface{}{
		"status":      "ok",
		"output_file": outputPath,
		"pmid_count":  len(pmids),
		"bytes":       info.Size(),
		"command":     fmt.Sprintf("%s %s", filepath.Base(binary), strings.Join(cmdArgs, " ")),
	}))
}

func defaultPubMedRun(ctx context.Context, binary string, args []string, cwd string, env map[string]string) (string, string, error) {
	cmd := exec.CommandContext(ctx, binary, args...)
	if strings.TrimSpace(cwd) != "" {
		cmd.Dir = cwd
	}
	if len(env) > 0 {
		cmd.Env = mergeEnv(os.Environ(), env)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}

func findPubMedBinary() (string, error) {
	for _, name := range []string{"pubmed-cli", "pubmed"} {
		if path, err := exec.LookPath(name); err == nil {
			return path, nil
		}
	}
	return "", fmt.Errorf("pubmed binary not found in PATH (install: brew tap drpedapati/tap && brew install sciclaw-pubmed-cli)")
}

func parsePMIDList(raw interface{}) ([]string, error) {
	switch v := raw.(type) {
	case []string:
		out := make([]string, 0, len(v))
		for _, s := range v {
			s = strings.TrimSpace(s)
			if s != "" {
				out = append(out, s)
			}
		}
		return out, nil
	case []interface{}:
		out := make([]string, 0, len(v))
		for _, item := range v {
			s, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("pmids must be strings")
			}
			s = strings.TrimSpace(s)
			if s != "" {
				out = append(out, s)
			}
		}
		return out, nil
	case string:
		parts := strings.FieldsFunc(v, func(r rune) bool {
			return r == ',' || r == ' ' || r == '\n' || r == '\t'
		})
		out := make([]string, 0, len(parts))
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p != "" {
				out = append(out, p)
			}
		}
		return out, nil
	default:
		return nil, fmt.Errorf("invalid pmids type")
	}
}

func truncateToolOutput(s string, max int) string {
	if max <= 0 || len(s) <= max {
		return s
	}
	return s[:max] + "... (truncated)"
}
