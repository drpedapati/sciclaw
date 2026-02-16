---
name: phi-cleaner
description: "De-identify clinical text for drafts, notes, and manuscript excerpts using phi-cleaner before sharing with external tools or collaborators. Use when: (1) Removing patient names/dates/identifiers from free-text notes, (2) Producing sanitized excerpts for LLM prompts, (3) Running a PHI detection pass to audit sensitive text, (4) Preparing de-identified appendices or examples for manuscripts and presentations."
metadata: {"nanobot":{"requires":{"bins":["phi-clean"]},"install":[{"id":"brew","kind":"brew","formula":"drpedapati/tools/phi-cleaner","bins":["phi-clean"],"label":"Install phi-cleaner (brew)"}]}}
---

# phi-cleaner

`phi-cleaner` is an optional companion CLI for text de-identification.

Use it before sending potentially sensitive clinical text into downstream tooling.

## Install

```bash
brew tap drpedapati/tools
brew install drpedapati/tools/phi-cleaner
```

Verify:

```bash
phi-clean --version
```

## Common commands

```bash
# Clean direct text input
phi-clean "Patient John Smith was seen on 03/15/2024."

# Detect-only mode
phi-clean --detect "Dr. Chen at Mayo Clinic"

# File input/output
phi-clean -f note.txt -o note.cleaned.txt

# Show available models
phi-clean --models
```

## Workflow guidance

1. Run `phi-clean` before sharing clinical notes in chat workflows.
2. Keep original and de-identified files separate.
3. Validate cleaned output for context loss before publication.

## Caveat

This tool helps reduce accidental PHI leakage, but it is not a legal/compliance determination on its own.
