# Tools

Use tools as part of reproducible workflows, not ad-hoc actions.

## Discovery

- Search and summarize relevant sources.
- Record key evidence and references in logs/notes.

## Execution

- Run code and shell commands in idempotent, reversible ways.
- Prefer explicit inputs/outputs and deterministic scripts where possible.

## Python

Always use `uv run python` instead of bare `python` for all Python scripts.
The `uv` tool manages the local virtual environment in the workspace directory.
Do NOT use the system Python or pip directly.

## Validation

- Re-run critical steps before claiming completion.
- Note assumptions, failure modes, and confidence level.

## Reporting

- Link major outputs to file paths and commands.
- Update plan/activity logs for all materially relevant tool use.

## Baseline Skill Policy

- Keep baseline skills installed and available in `workspace/skills/`.
- Prefer using these skills before ad-hoc prompt-only behavior.
- If your use case is not research-heavy, trim or replace skill folders to match your workflow.

## Critical CLI-First Rules

- For PubMed literature tasks, prefer `pubmed_search` and `pubmed_fetch` first.
- Use the installed `pubmed`/`pubmed-cli` directly only for advanced flags or workflows not covered by the typed tools.
- Do not scrape `pubmed.ncbi.nlm.nih.gov` with `web_fetch` when `pubmed` CLI is available.
- Do not wrap CLI tools in Python subprocess calls when direct CLI calls are sufficient.
- For new Word documents, write Markdown and convert with `pandoc ... -o file.docx`.
- For `pandoc` DOCX generation, sciClaw auto-applies its bundled NIH reference template unless you explicitly pass `--reference-doc`.
- Use `docx-review` only for tracked-change edits/comments/diff on existing documents.
- Do not use `docx-review` manifest workflows to create first-draft manuscripts unless the user explicitly requests tracked changes.
- For common Word review workflows, prefer `docx_review_read`, `docx_review_diff`, and `docx_review_apply` over shelling out to `docx-review` directly.
- Use raw `docx-review` CLI only as fallback or for advanced modes not covered by typed tools (for example `--textconv`, `--git-setup`, `--create`, or custom flag combinations).
- For spreadsheet review workflows, prefer `xlsx_review_read`, `xlsx_review_diff`, and `xlsx_review_apply` over shelling out to `xlsx-review` directly.
- Use raw `xlsx-review` CLI only as fallback or for advanced modes not covered by typed tools (for example `--textconv`, `--git-setup`, `--create`, or custom flag combinations).
- For presentation review workflows, prefer `pptx_review_read`, `pptx_review_diff`, and `pptx_review_apply` over shelling out to `pptx-review` directly.
- Use raw `pptx-review` CLI only as fallback or for advanced modes not covered by typed tools (for example `--textconv`, `--git-setup`, or custom flag combinations).
- For fillable AcroForm PDFs, prefer `pdf_form_inspect`, `pdf_form_schema`, and `pdf_form_fill` over shelling out to `pdf-form-filler` directly.
- Do not wrap `pdf-form-filler` in Python subprocess calls when dedicated sciClaw PDF form tools are available.

### PubMed Examples (Preferred)

```bash
pubmed search "schizophrenia treatment" --json --limit 20
pubmed fetch 41705278 41704932 41704822 --json

# Typed tools are preferred when available
pubmed_search(query="schizophrenia treatment", limit=20)
pubmed_fetch(pmids=["41705278","41704932","41704822"])
```

### Anti-Pattern (Avoid)

```python
# Avoid Python subprocess wrappers for installed CLIs
subprocess.check_output(["pubmed", "search", "query", "--json"])
```
