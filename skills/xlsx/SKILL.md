---
name: xlsx
description: Use this skill whenever spreadsheet files (`.xlsx`, `.xlsm`, `.csv`, `.tsv`) are created, edited, cleaned, analyzed, or converted.
---

# XLSX Skill (Anthropic Official Source)

Source: `https://github.com/anthropics/skills/tree/main/skills/xlsx`

Use this skill for spreadsheet-centric workflows.

When running inside sciClaw, prefer the built-in typed spreadsheet tools over ad-hoc shell calls:
- `xlsx_review_read`
- `xlsx_review_diff`
- `xlsx_review_apply`

Use raw `xlsx-review` CLI only for advanced modes not covered by those typed tools, such as `--create`, `--textconv`, or `--git-setup`.

## Typical Triggers

- "update this Excel file"
- "create a spreadsheet model"
- "clean this CSV/TSV and output xlsx"
- "add formulas/charts/formatting to workbook"

## Working Rules

1. Use formulas in the sheet instead of hardcoded computed values.
2. Preserve existing workbook conventions when editing templates.
3. Validate output for formula errors (`#REF!`, `#DIV/0!`, `#VALUE!`, etc.).
4. Keep transformation provenance for publication-quality reproducibility.

## Quick Commands

```python
import pandas as pd
df = pd.read_excel("input.xlsx")
df.to_excel("output.xlsx", index=False)
```
