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
- For new Word documents, write Markdown and convert with ` + "`pandoc ... -o file.docx`" + `.
- For ` + "`pandoc`" + ` DOCX generation, sciClaw auto-applies its bundled NIH reference template unless you explicitly pass ` + "`--reference-doc`" + `.
- Use ` + "`docx-review`" + ` only for tracked-change edits/comments/diff on existing documents.
- Do not use ` + "`docx-review`" + ` manifest workflows to create first-draft manuscripts unless the user explicitly requests tracked changes.
- For common Word review workflows, prefer ` + "`docx_review_read`" + `, ` + "`docx_review_diff`" + `, and ` + "`docx_review_apply`" + ` over shelling out to ` + "`docx-review`" + ` directly.
- Use raw ` + "`docx-review`" + ` CLI only as fallback or for advanced modes not covered by typed tools (for example ` + "`--textconv`" + `, ` + "`--git-setup`" + `, ` + "`--create`" + `, or custom flag combinations).
- For spreadsheet review workflows, prefer ` + "`xlsx_review_read`" + `, ` + "`xlsx_review_diff`" + `, and ` + "`xlsx_review_apply`" + ` over shelling out to ` + "`xlsx-review`" + ` directly.
- Use raw ` + "`xlsx-review`" + ` CLI only as fallback or for advanced modes not covered by typed tools (for example ` + "`--textconv`" + `, ` + "`--git-setup`" + `, ` + "`--create`" + `, or custom flag combinations).
- For presentation review workflows, prefer ` + "`pptx_review_read`" + `, ` + "`pptx_review_diff`" + `, and ` + "`pptx_review_apply`" + ` over shelling out to ` + "`pptx-review`" + ` directly.
- Use raw ` + "`pptx-review`" + ` CLI only as fallback or for advanced modes not covered by typed tools (for example ` + "`--textconv`" + `, ` + "`--git-setup`" + `, or custom flag combinations).
- For fillable AcroForm PDFs, prefer ` + "`pdf_form_inspect`" + `, ` + "`pdf_form_schema`" + `, and ` + "`pdf_form_fill`" + ` over shelling out to ` + "`pdf-form-filler`" + ` directly.
- Do not wrap ` + "`pdf-form-filler`" + ` in Python subprocess calls when dedicated sciClaw PDF form tools are available.

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
