# Issue: Exec guard blocks valid scientific workflows

## Summary
Some valid research commands are still blocked by the `exec` safety/path guard, especially when users try to export citations or run simple text metrics in constrained environments.

## User-facing failures
- PubMed citation export flows can fail (for example RIS-focused workflows).
- Basic stdin-style word-count checks can fail under guard constraints.

## Why this matters
These are first-class scientific workflows in sciClaw. When they fail unpredictably, non-technical users lose trust and cannot complete manuscript tasks reliably.

## Decision
Adopt a **tool-first carveout** instead of broad shell allowlisting.

- Add/extend dedicated internal tools for:
  - PubMed export workflow (RIS-safe path in workspace).
  - Word-count/stat checks without shell piping dependency.
- Keep `exec` guard strict for general shell commands.

## Acceptance criteria
- PubMed export works in restricted mode for workspace outputs.
- Word-count checks work without shell stdin/piping tricks.
- Dangerous/path-traversal shell protections remain intact.
- Regression tests cover both successful scientific flows and blocked unsafe flows.

## Suggested implementation notes
- Prefer explicit tool parameters over free-form shell parsing.
- Keep existing `exec` guard defaults unchanged for upstream compatibility.
- Add tests near `pkg/tools` for new scientific-tool behavior and guard non-regression.
