---
name: pandoc-docx
description: Create clean first-draft Word documents from Markdown using pandoc, with sciClaw auto-applying the bundled NIH reference template unless a custom --reference-doc is provided.
---

# Pandoc DOCX

Use this skill when the user asks to create a new Word document from fresh content.

## When to use

- "create a Word document from this outline"
- "convert this markdown to docx"
- "generate a clean manuscript draft in .docx"

## Default workflow

1. Draft content in Markdown (`.md`).
2. Convert with pandoc:

```bash
pandoc manuscript.md -o manuscript.docx
```

3. Keep output clean (no tracked changes) unless explicitly requested.

## Critical routing rule

- For NEW clean documents: use `pandoc`.
- For tracked review edits/comments on existing docs: use `docx-review`.

## Notes

- sciClaw injects `PANDOC_DEFAULTS` at runtime for DOCX commands and resolves a bundled NIH reference template automatically.
- If user passes `--reference-doc`, respect that override.
