# RFC: Curated Workspace Memory Boundary

Status: Draft
Date: 2026-05-05

## Problem

`memory/MEMORY.md` is being used as an execution log in routed scientific workspaces.

Observed EEG workflow failure:
- a Python analysis runs successfully
- the agent appends a dated success record to `memory/MEMORY.md`
- the record includes command completion, workbook path, byte size, sheet names, validation checks, row counts, and remaining mismatches
- later turns inject that entire record as long-term memory

This is the wrong persistence layer. Routine execution state already has better records: output files, session JSON, artifact tracking, job records, hook audit JSONL, gateway/tool logs, and Discord archives. Copying the same facts into `MEMORY.md` creates prompt bloat and stale context without improving reproducibility.

The current prompt-only fix reduces the behavior but does not enforce the boundary. Generic file tools can still write directly to `memory/MEMORY.md`, and daily notes can become a temporary version of the same log sink.

## Root Cause

SciClaw has deterministic memory readers, not deterministic memory writers.

`pkg/agent/memory.go` reads:
- long-term memory from `memory/MEMORY.md`
- recent daily notes from `memory/YYYYMM/YYYYMMDD.md`

Those files are injected into the prompt. The write methods in `MemoryStore` are not the production path for the observed pollution.

The observed write path is model-driven:
1. The model sees instructions that memory should preserve useful facts.
2. The model decides a successful execution summary is worth remembering.
3. The model calls generic file tools such as `append_file`.
4. The file tool appends prose to `memory/MEMORY.md`.

This means the fix must create a write boundary, not just better prose instructions.

## Existing Ledgers

SciClaw already has several persistence layers that should carry execution activity.

| Layer | Current path / code | Should carry |
| --- | --- | --- |
| Session history | `sessions/*.json`, `pkg/session/manager.go` | conversation messages, tool calls, compacted tool results |
| Session artifacts | `Session.Artifacts`, `BuildArtifactContext` | canonical input/output file paths for future turns |
| Tool provenance | `pkg/agent/artifacts.go` | output files produced by write/review/message/export tools |
| Routed jobs | `pkg/routing/jobs.go` | queued/running/done/failed state, phase, detail, errors |
| Hook audit | `hooks/hook-events.jsonl`, `pkg/hooks/audit_jsonl.go` | lifecycle event audit metadata |
| Gateway/tool logs | `pkg/logger`, `pkg/tools/registry.go` | operational debugging, durations, failures |
| Discord archive | `memory/archive/discord` | old conversation text for recall |
| Workspace memory | `memory/MEMORY.md` | curated durable context injected every turn |

Only the final layer is paid for on every turn. Therefore it must be small, durable, and intentionally curated.

## Goals

1. Preserve smart automatic memory updates.
2. Prevent routine execution logs from entering `memory/MEMORY.md`.
3. Keep `MEMORY.md` compact enough to be worth injecting on every turn.
4. Route execution activity to existing ledgers instead of duplicating it.
5. Give existing users clear cleanup guidance for polluted memory files.

## Non-Goals

- Build the full verified execution ledger from `verified-execution-memory-rfc.md`.
- Remove hooks or session history.
- Prevent users from manually editing `MEMORY.md` in the System tab.
- Make `MEMORY.md` a database, search index, or complete lab notebook.
- Solve all prompt-size issues in one pass.

## Design

### 1. Add a dedicated `remember` tool

Add a narrow memory-writing tool, likely `pkg/tools/memory.go`.

Inputs:
- `category`
- `content`
- `reason`
- optional `source_artifacts`

Allowed categories:
- `user_preference`
- `project_convention`
- `canonical_artifact`
- `method_decision`
- `known_issue`
- `open_question`
- `data_provenance`

Rejected categories:
- `execution_log`
- `routine_success`
- `temporary_artifact`

The tool appends only accepted curated entries to `memory/MEMORY.md`, under stable sections. It should return a clear rejection reason when content belongs in session/artifact/job/audit records instead.

### 2. Block generic writes to long-term memory

Add a shared path guard used by:
- `WriteFileTool`
- `EditFileTool`
- `AppendFileTool`

If the resolved target is `<workspace>/memory/MEMORY.md`, reject the write with:

> Use the `remember` tool for long-term memory updates. Routine execution logs belong in session history, artifacts, jobs, hooks, or output files.

This should not block explicit user edits through the web System page, because those requests bypass the agent file tools and are user-directed.

### 3. Keep hooks audit-only

Hooks should not write to `MEMORY.md`.

Current hooks already write audit entries to `hooks/hook-events.jsonl`. That is the right destination for lifecycle events and tool outcomes.

Update `HOOKS.md` wording so "record" means audit logs, plans, output artifacts, or reports, not long-term memory. The hook policy template should explicitly say routine hook/tool outcomes must not be copied into `memory/MEMORY.md`.

### 4. Keep daily notes narrow

Daily notes are useful for short-term continuity, but they must not become the new execution log.

Allowed daily-note use:
- unresolved work that should survive a restart
- a currently running or interrupted job
- short recovery context for the next turn

Rejected daily-note use:
- routine successful commands
- file sizes and validation summaries
- smoke-test outputs
- facts already present in artifacts, jobs, sessions, or hook audit

### 5. Add context-budget protection

`MemoryStore.GetMemoryContext()` should enforce a budget.

Initial policy:
- warn or truncate when `MEMORY.md` exceeds a conservative character budget
- separately cap recent daily notes
- include a visible notice that memory was truncated and should be cleaned

This is a safety valve, not the main fix. The main fix is preventing low-value writes.

## Acceptance Rules For `remember`

Accept if the proposed entry answers: "Will this still matter in a future session when the original output files and session history are not being inspected?"

Good examples:
- "Use `data/PreSMART_R61_03/SSRT/..._log_summary_consistency.xlsx` as the canonical consistency workbook for PreSMART R61 03 Post1 fixed 03a FIF."
- "The lab convention is to analyze successful and failed stop trials separately using `--stop-outcome`."
- "Known issue: subject IDs in raw log filenames may be `03` while EEG files may be `03a` or `03b`; validate against EEG filename when present."

Bad examples:
- "Command completed successfully."
- "Workbook exists, 36,309 bytes."
- "Smoke run produced shape `(6, 876, 26)` before temporary outputs were deleted."
- "Sheets are `EEG vs. log`, `txt vs. log`, and `unmatched`."
- "Ran `python -m py_compile ...` successfully."

Borderline examples should be compressed into durable meaning. For the EEG example, row counts and remaining mismatches should not be saved by default unless they change the scientific interpretation or become a canonical benchmark.

## Existing User Cleanup Guidance

Users with polluted `memory/MEMORY.md` can clean it up. The new system does not need old execution logs to work.

Before editing:
- make a backup copy of `memory/MEMORY.md`
- keep entries that encode durable project knowledge
- remove routine execution records

Keep:
- stable dataset/script/output paths that are reused
- scientific or analysis decisions
- inclusion/exclusion rules
- canonical methods and thresholds
- known bugs and workarounds
- user or collaborator preferences
- unresolved open questions

Delete:
- "ran script" entries
- command success confirmations
- file sizes and byte counts
- smoke-test details
- temporary output paths
- routine validation summaries
- row counts that are already in output workbooks

Future follow-on: add `sciclaw memory audit` to classify existing sections as `keep`, `archive`, or `delete`, but that command is not required for the first implementation.

## Surgical Implementation Plan

Phase 1:
1. Add `RememberTool`.
2. Register it next to file tools in the agent loop.
3. Add a memory path guard to generic file mutation tools.
4. Update `context.go`, `AGENTS.md`, and `HOOKS.md` language.
5. Add tests for accepted and rejected memory writes.

Phase 2:
1. Add memory context budget enforcement.
2. Add `sciclaw doctor` warning for oversized `MEMORY.md`.
3. Add optional memory audit/cleanup command.

Phase 3:
1. Revisit `verified-execution-memory-rfc.md`.
2. Decide whether to extend `Session` with structured verified facts once the immediate `MEMORY.md` boundary is enforced.

## Test Plan

Unit tests:
- `append_file` to `memory/MEMORY.md` is rejected.
- `write_file` to `memory/MEMORY.md` is rejected.
- `edit_file` to `memory/MEMORY.md` is rejected.
- `remember` accepts a method decision.
- `remember` accepts a canonical artifact entry.
- `remember` rejects the EEG success-log example.
- `remember` rejects file-size and smoke-test-only content.
- `remember` writes under the expected section.
- memory context truncation emits a visible notice when enabled.

Integration tests:
- run a simulated successful Python workflow and verify no `MEMORY.md` write occurs through generic tools.
- verify output paths still appear in session artifact context.
- verify hook audit entries still appear in `hooks/hook-events.jsonl`.
- verify a model can still record a durable project convention through `remember`.

Regression tests:
- existing System page edits of `memory/MEMORY.md` still work.
- workspace template creation still creates `memory/MEMORY.md`.
- routed workspaces still get independent memory files.

## Open Questions

1. Should `remember` append markdown directly, or maintain a small structured sidecar such as `memory/memory.json` and render `MEMORY.md` from it?
2. Should `remember` deduplicate similar entries by category and normalized content?
3. Should daily notes be writable only through a separate `note_today` tool?
4. What default character budget should `MEMORY.md` have before `doctor` warns?
5. Should memory cleanup archive deleted entries under `memory/archive/curated/` or leave backup creation to the user?

## Decision Bias

Prefer the smallest enforceable boundary:
- one dedicated memory write tool
- one generic file-tool guard
- prompt/template wording that routes logs to existing ledgers
- no new general execution ledger until the memory abuse is stopped
