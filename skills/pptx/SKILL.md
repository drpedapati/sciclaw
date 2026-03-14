---
name: pptx
description: Use this skill whenever `.pptx` slide decks are created, edited, summarized, templated, merged, or analyzed.
---

# PPTX Skill (Anthropic Official Source)

Source: `https://github.com/anthropics/skills/tree/main/skills/pptx`

Use this skill when presentations are in scope.

When running inside sciClaw, prefer the built-in typed presentation tools over ad-hoc shell calls:
- `pptx_review_read`
- `pptx_review_diff`
- `pptx_review_apply`

Use raw `pptx-review` CLI only for advanced modes not covered by those typed tools, such as `--textconv` or `--git-setup`.

## Typical Triggers

- "make a slide deck"
- "update this presentation"
- "extract content from .pptx"
- "prepare a poster/meeting deck from manuscript results"

## Working Rules

1. Start from content goals and audience, then map slide structure.
2. Keep slide layouts intentional, not generic bullet dumps.
3. Preserve template styles when editing existing decks.
4. Validate by reviewing rendered slides before final delivery.

## Quick Commands

```bash
# Extract markdown-like text from deck
python -m markitdown presentation.pptx
```
