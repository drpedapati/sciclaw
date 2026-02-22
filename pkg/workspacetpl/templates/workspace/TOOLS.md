# Tools

Use tools as part of reproducible workflows, not ad-hoc actions.

## Discovery

- Search and summarize relevant sources.
- Record key evidence and references in logs/notes.

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

- Keep baseline skills installed and available in `workspace/skills/`.
- Prefer using these skills before ad-hoc prompt-only behavior.
- If your use case is not research-heavy, trim or replace skill folders to match your workflow.

## Critical CLI-First Rules

- For PubMed literature tasks, use the installed `pubmed`/`pubmed-cli` directly.
- Do not scrape `pubmed.ncbi.nlm.nih.gov` with `web_fetch` when `pubmed` CLI is available.
- Do not wrap CLI tools in Python subprocess calls when direct CLI calls are sufficient.
- For new Word documents, write Markdown and convert with `pandoc ... -o file.docx`.
- For `pandoc` DOCX generation, sciClaw auto-applies its bundled NIH reference template unless you explicitly pass `--reference-doc`.
- Use `docx-review` only for tracked-change edits/comments/diff on existing documents.
- Do not use `docx-review` manifest workflows to create first-draft manuscripts unless the user explicitly requests tracked changes.

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
