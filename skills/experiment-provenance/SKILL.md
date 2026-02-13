---
name: experiment-provenance
description: Capture experiment provenance with reproducible run metadata, artifact pointers, and decision logs for scientific claims.
---

# Experiment Provenance

Use this skill when a task produces evidence that may become a manuscript claim.

## When to use

- "track provenance"
- "log this experiment"
- "record reproducible run details"
- "capture evidence for manuscript claim"

## Required provenance fields

1. Objective and hypothesis.
2. Exact command(s) executed.
3. Input files, config, and environment assumptions.
4. Output artifact paths.
5. Validation status (tests/build/render).
6. Claim boundary and uncertainty notes.

## Workflow

1. Record run intent before execution.
2. Execute with deterministic commands where possible.
3. Store output artifact paths, not just summaries.
4. Link provenance notes to `plans/main-plan-activity.md` and `plans/main-plan-log.csv`.
5. Mark evidence quality: strong, partial, or insufficient.
