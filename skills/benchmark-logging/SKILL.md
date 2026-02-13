---
name: benchmark-logging
description: Define benchmark runs and log outcomes with consistent metrics, acceptance criteria, and reproducible artifact references.
---

# Benchmark Logging

Use this skill to run and document benchmark comparisons between sciClaw and baseline workflows.

## When to use

- "run benchmark"
- "compare baseline vs sciclaw"
- "log benchmark outcomes"
- "add acceptance criteria"

## Minimum benchmark record

1. Benchmark ID and date.
2. Task category and scenario definition.
3. Baseline command sequence.
4. sciClaw command sequence.
5. Metrics: task success, reproducibility, latency, and resource usage.
6. Acceptance decision (pass/fail) with rationale.

## Workflow

1. Freeze scenario definitions before running.
2. Execute baseline and sciClaw runs with the same inputs.
3. Record metric values and artifact paths.
4. Log failures with root-cause notes and retry policy.
5. Add manuscript-ready summary sentences only after data is logged.
