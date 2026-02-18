# Tools

Use tools as part of a scientific workflow, not ad-hoc actions.

## Discovery

- Search and summarize literature or external sources.
- Record key evidence and source references in logs/manuscript notes.

## Execution

- Run code and shell commands in idempotent, reversible ways.
- Prefer explicit inputs/outputs and deterministic scripts where possible.

## Validation

- Re-run critical steps before claiming completion.
- Note assumptions, failure modes, and confidence level.

## Reporting

- Link major outputs to file paths and commands.
- Update plan/activity logs for all materially relevant tool use.

## Baseline Skill Policy

- Keep baseline scientific skills installed and available in `workspace/skills/`.
- Prefer using these skills before ad-hoc prompt-only behavior for literature, provenance, benchmarking, and manuscript operations.

## Critical CLI-First Rules

- For PubMed literature tasks, use the installed `pubmed`/`pubmed-cli` directly.
- Do not scrape `pubmed.ncbi.nlm.nih.gov` with `web_fetch` when `pubmed` CLI is available.
- Do not wrap CLI tools in Python subprocess calls when direct CLI calls are sufficient.
- For Word edits, use `docx-review` directly for read/edit/diff workflows.

### PubMed Examples (Preferred)

```bash
pubmed search "schizophrenia treatment" --json --limit 20
pubmed fetch 41705278 41704932 41704822 --json
```

### Anti-Pattern (Avoid)

```python
# Avoid Python subprocess wrappers for installed CLIs
subprocess.check_output(["pubmed", "search", "query", "--json"])
```
