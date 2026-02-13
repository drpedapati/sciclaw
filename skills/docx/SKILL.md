---
name: docx
description: Use this skill whenever the user needs to create, read, edit, or analyze Microsoft Word `.docx` files, including reports, manuscripts, templates, or tracked-change workflows.
---

# DOCX Skill (Anthropic Official Source)

Source: `https://github.com/anthropics/skills/tree/main/skills/docx`

Use this skill when `.docx` is the target artifact or input.

## Typical Triggers

- "create a Word document"
- "edit this .docx"
- "extract text from a Word file"
- "format a report with headings/table of contents/page numbers"

## Working Rules

1. Prefer deterministic conversions and scripted edits.
2. Preserve document structure and formatting intent.
3. Validate output by reopening/converting and checking content integrity.
4. Keep provenance notes for major transformations.

## Quick Commands

```bash
# Extract readable markdown from .docx
pandoc --track-changes=all input.docx -o output.md

# Convert legacy .doc to .docx (LibreOffice required)
soffice --headless --convert-to docx input.doc
```
