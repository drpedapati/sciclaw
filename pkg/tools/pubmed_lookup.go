package tools

import (
	"context"
	"fmt"
	"strings"
	"time"
)

type pubmedLookupToolBase struct {
	workspace string
	extraEnv  map[string]string
	run       pubmedRunFunc
	findBin   func() (string, error)
	timeout   time.Duration
}

func newPubmedLookupToolBase(workspace string) pubmedLookupToolBase {
	return pubmedLookupToolBase{
		workspace: workspace,
		extraEnv:  map[string]string{},
		run:       defaultPubMedRun,
		findBin:   findPubMedBinary,
		timeout:   90 * time.Second,
	}
}

func (b *pubmedLookupToolBase) SetExtraEnv(env map[string]string) {
	if len(env) == 0 {
		return
	}
	for k, v := range env {
		if strings.TrimSpace(k) == "" {
			continue
		}
		b.extraEnv[k] = v
	}
}

func (b *pubmedLookupToolBase) execute(ctx context.Context, args []string) *ToolResult {
	binary, err := b.findBin()
	if err != nil {
		return ErrorResult(err.Error())
	}
	runCtx, cancel := context.WithTimeout(ctx, b.timeout)
	defer cancel()

	stdout, stderr, runErr := b.run(runCtx, binary, args, b.workspace, b.extraEnv)
	if runErr != nil {
		msg := fmt.Sprintf("pubmed %s failed: %v", args[0], runErr)
		detail := strings.TrimSpace(stderr)
		if detail == "" {
			detail = strings.TrimSpace(stdout)
		}
		if detail != "" {
			msg += "\n" + truncateToolOutput(detail, 1200)
		}
		return ErrorResult(msg)
	}

	content := strings.TrimSpace(stdout)
	if content == "" {
		content = strings.TrimSpace(stderr)
	}
	if content == "" {
		content = mustJSON(map[string]interface{}{
			"status":  "ok",
			"command": strings.Join(args, " "),
		})
	}
	return NewToolResult(content)
}

type PubMedSearchTool struct {
	base pubmedLookupToolBase
}

func NewPubMedSearchTool(workspace string) *PubMedSearchTool {
	return &PubMedSearchTool{base: newPubmedLookupToolBase(workspace)}
}

func newPubMedSearchToolWithRunner(workspace string, runner pubmedRunFunc) *PubMedSearchTool {
	t := NewPubMedSearchTool(workspace)
	if runner != nil {
		t.base.run = runner
	}
	return t
}

func (t *PubMedSearchTool) SetExtraEnv(env map[string]string) {
	t.base.SetExtraEnv(env)
}

func (t *PubMedSearchTool) Name() string {
	return "pubmed_search"
}

func (t *PubMedSearchTool) Description() string {
	return "Search PubMed directly and return structured results. Prefer this over web_search for citation verification, PMID lookup, and biomedical literature retrieval."
}

func (t *PubMedSearchTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"query": map[string]interface{}{
				"type":        "string",
				"description": "PubMed search query",
			},
			"limit": map[string]interface{}{
				"type":        "integer",
				"description": "Maximum number of results to return (default 10)",
			},
			"sort": map[string]interface{}{
				"type":        "string",
				"description": "Optional sort order: relevance, date, or cited",
				"enum":        []string{"relevance", "date", "cited"},
			},
			"year": map[string]interface{}{
				"type":        "string",
				"description": "Optional year filter: YYYY or YYYY-YYYY",
			},
		},
		"required": []string{"query"},
	}
}

func (t *PubMedSearchTool) Execute(ctx context.Context, args map[string]interface{}) *ToolResult {
	query := strings.TrimSpace(getString(args, "query"))
	if query == "" {
		return ErrorResult("query is required")
	}

	limit, err := getOptionalPositiveInt(args, "limit", 10)
	if err != nil {
		return ErrorResult(err.Error())
	}

	cmdArgs := []string{"search", query, "--json", "--limit", fmt.Sprintf("%d", limit)}

	if sort := strings.TrimSpace(getString(args, "sort")); sort != "" {
		if !isAllowedPubMedSort(sort) {
			return ErrorResult("sort must be one of: relevance, date, cited")
		}
		cmdArgs = append(cmdArgs, "--sort", sort)
	}

	if year := strings.TrimSpace(getString(args, "year")); year != "" {
		if !isValidPubMedYear(year) {
			return ErrorResult("year must be YYYY or YYYY-YYYY")
		}
		cmdArgs = append(cmdArgs, "--year", year)
	}

	return t.base.execute(ctx, cmdArgs)
}

type PubMedFetchTool struct {
	base pubmedLookupToolBase
}

func NewPubMedFetchTool(workspace string) *PubMedFetchTool {
	return &PubMedFetchTool{base: newPubmedLookupToolBase(workspace)}
}

func newPubMedFetchToolWithRunner(workspace string, runner pubmedRunFunc) *PubMedFetchTool {
	t := NewPubMedFetchTool(workspace)
	if runner != nil {
		t.base.run = runner
	}
	return t
}

func (t *PubMedFetchTool) SetExtraEnv(env map[string]string) {
	t.base.SetExtraEnv(env)
}

func (t *PubMedFetchTool) Name() string {
	return "pubmed_fetch"
}

func (t *PubMedFetchTool) Description() string {
	return "Fetch PubMed records by PMID and return structured JSON metadata and abstracts. Prefer this over web_fetch for PubMed article pages."
}

func (t *PubMedFetchTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"pmids": map[string]interface{}{
				"type":        "array",
				"description": "List of PMID values to fetch",
				"items": map[string]interface{}{
					"type": "string",
				},
			},
		},
		"required": []string{"pmids"},
	}
}

func (t *PubMedFetchTool) Execute(ctx context.Context, args map[string]interface{}) *ToolResult {
	pmids, err := parsePMIDList(args["pmids"])
	if err != nil || len(pmids) == 0 {
		return ErrorResult("pmids is required and must include at least one PMID")
	}
	for _, pmid := range pmids {
		if !isDigitsOnly(pmid) {
			return ErrorResult("pmids must contain digits only")
		}
	}
	cmdArgs := append([]string{"fetch"}, pmids...)
	cmdArgs = append(cmdArgs, "--json")
	return t.base.execute(ctx, cmdArgs)
}

func getOptionalPositiveInt(args map[string]interface{}, key string, fallback int) (int, error) {
	raw, ok := args[key]
	if !ok || raw == nil {
		return fallback, nil
	}
	switch v := raw.(type) {
	case int:
		if v <= 0 {
			return 0, fmt.Errorf("%s must be greater than 0", key)
		}
		return v, nil
	case float64:
		if v <= 0 || v != float64(int(v)) {
			return 0, fmt.Errorf("%s must be a positive integer", key)
		}
		return int(v), nil
	default:
		return 0, fmt.Errorf("%s must be an integer", key)
	}
}

func isAllowedPubMedSort(sort string) bool {
	switch sort {
	case "relevance", "date", "cited":
		return true
	default:
		return false
	}
}

func isValidPubMedYear(year string) bool {
	parts := strings.Split(year, "-")
	if len(parts) > 2 {
		return false
	}
	for _, part := range parts {
		if len(part) != 4 || !isDigitsOnly(part) {
			return false
		}
	}
	if len(parts) == 2 && parts[0] > parts[1] {
		return false
	}
	return true
}

func isDigitsOnly(s string) bool {
	if strings.TrimSpace(s) == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
