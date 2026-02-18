package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const toolsCLIFirstPolicyHeading = "## Critical CLI-First Rules"

const toolsCLIFirstPolicySection = `
## Critical CLI-First Rules

- For PubMed literature tasks, use the installed ` + "`pubmed`/`pubmed-cli`" + ` directly.
- Do not scrape ` + "`pubmed.ncbi.nlm.nih.gov`" + ` with ` + "`web_fetch`" + ` when ` + "`pubmed`" + ` CLI is available.
- Do not wrap CLI tools in Python subprocess calls when direct CLI calls are sufficient.
- For Word edits, use ` + "`docx-review`" + ` directly for read/edit/diff workflows.

### PubMed Examples (Preferred)

` + "```bash" + `
pubmed search "schizophrenia treatment" --json --limit 20
pubmed fetch 41705278 41704932 41704822 --json
` + "```" + `

### Anti-Pattern (Avoid)

` + "```python" + `
# Avoid Python subprocess wrappers for installed CLIs
subprocess.check_output(["pubmed", "search", "query", "--json"])
` + "```" + `
`

func ensureToolsCLIFirstPolicy(workspace string) error {
	toolsPath := filepath.Join(workspace, "TOOLS.md")
	if !fileExists(toolsPath) {
		return nil
	}

	contentBytes, err := os.ReadFile(toolsPath)
	if err != nil {
		return fmt.Errorf("read TOOLS.md: %w", err)
	}
	content := string(contentBytes)
	if strings.Contains(content, toolsCLIFirstPolicyHeading) {
		return nil
	}

	addition := strings.TrimSpace(toolsCLIFirstPolicySection)
	updated := strings.TrimRight(content, "\n")
	if updated != "" {
		updated += "\n\n"
	}
	updated += addition + "\n"

	if err := os.WriteFile(toolsPath, []byte(updated), 0644); err != nil {
		return fmt.Errorf("write TOOLS.md: %w", err)
	}
	return nil
}
