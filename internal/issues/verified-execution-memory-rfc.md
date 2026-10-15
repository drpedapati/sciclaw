# RFC: Verified Execution Memory for Discord Sessions

Status: Draft
Date: 2026-03-17

## Problem

Recent `tms-people` turns show that sciClaw can promote intended or partial work into durable session truth.

Observed failure pattern:
- a turn narrates work as if it completed
- the session summary stores that narration as fact
- later turns inherit the false state and reason from it
- follow-up turns then burn large token budgets correcting or reconciling contradictions

Concrete examples from `tms-people`:
- session summary says the CONSORT skill file was written and the page-count patch was committed/pushed/PR-ready
- the live workspace at the time only had partial local edits
- a later turn had to explicitly admit that the claimed rerun had not happened

This is not primarily a routing or archive-recall problem.
- `discord auto-recall` had no hits in the bad turn
- injected artifact context was small
- the main corruption channel is the free-form summary generated in `pkg/agent/loop.go`

## Root Cause

Current summarization in `pkg/agent/loop.go` is free-form.

`maybeSummarize` and `summarizeSession`:
- summarize old user/assistant text only
- do not distinguish verified execution facts from assistant intent
- do not ground summary claims in successful tool results
- can therefore encode "planned" or "attempted" work as if it were completed

This is especially bad for repo and workflow tasks where future turns depend on objective state:
- file written or not
- branch created or not
- commit created or not
- push succeeded or not
- PR created or not

## Goals

1. Prevent unverified execution claims from entering durable session memory.
2. Preserve useful conversational context without requiring a major memory-system rewrite.
3. Improve follow-up task quality by giving later turns reliable execution state.
4. Make the fix measurable with targeted tests and live canaries.

## Non-Goals

- redesign the full archive/recall system
- replace the existing natural-language summary entirely
- solve all runaway-token behavior in one change
- build a general workflow engine

## Proposed Design

Add a small structured "verified execution ledger" per session and constrain summarization to use it.

### 1. Add verified execution facts to session state

Extend session persistence with a small append-only fact list, for example:

- `file_written`
- `file_edited`
- `attachment_staged`
- `branch_created`
- `git_commit`
- `git_push`
- `pr_created`
- `tool_failed`

Each fact should carry:
- `type`
- `timestamp`
- `turn_id`
- `tool`
- `path` or other stable identifier when relevant
- minimal metadata needed for later turns

Examples:
- `file_written path=/workspace/skills/consort-evaluation/SKILL.md`
- `branch_created branch=fix/page-count-estimation`
- `git_commit sha=abc123 subject="feat: add Page estimation..."`
- `tool_failed tool=write_file error="path is required"`

### 2. Only record facts from verified tool outcomes

Facts should be emitted from real tool success paths, not assistant prose.

Examples:
- `write_file` success -> `file_written`
- `edit_file` success -> `file_edited`
- `message` with attachments -> `message_sent` / `attachment_delivered`
- `exec` with git success patterns -> `branch_created`, `git_commit`, `git_push`
- `gh pr create` success -> `pr_created`

For the initial surgical version, pattern-match only the high-value execution states that are causing confusion.

### 3. Separate soft summary from hard execution state

Keep the existing free-form conversation summary, but change its contract.

The free-form summary may contain:
- user goals
- high-level reasoning context
- open questions
- unresolved risks

It may not assert completed execution unless that completion is present in the verified ledger.

Practical rule:
- "planned", "attempted", or "in progress" stays in free text
- "written", "committed", "pushed", "opened" requires a verified fact

### 4. Inject verified state explicitly into future turns

Before each turn, add a short "Verified Execution State" block to the prompt.

Example:
- verified files written
- verified branch name
- verified commit hash
- verified PR URL
- recent tool failures still unresolved

This block should be short, deterministic, and higher-trust than the conversational summary.

### 5. Gate summarization after bad turns

Do not update the free-form summary from turns that ended in obviously unstable states, for example:
- repeated tool failures above threshold
- no final user-facing response and no message-tool delivery
- still-running job / interrupted job
- excessive iteration count without convergence

Initial simple gate:
- skip summary update when the turn has 3+ tool failures or no stable terminal outcome

## Why this is the minimal fix

This does not require:
- replacing session summaries
- redesigning archive recall
- building a planner
- changing all tools

It only adds:
- a small structured fact ledger
- a narrow fact-emission path from verified tool results
- a stricter summary contract
- a deterministic prompt injection block

This is the smallest change that directly addresses the observed `tms-people` corruption pattern.

## File Touch Points

Likely implementation files:
- `pkg/session/manager.go`
  - persist and load verified execution facts
- `pkg/agent/loop.go`
  - inject verified state into prompt
  - gate summary updates on unstable turns
  - update summarization prompt/contract
- `pkg/tools/registry.go`
  - central place to observe tool success/failure
- `pkg/hooks/types.go`
  - optional: reuse hook metadata shape for fact emission

Optional follow-on:
- specific tools may emit richer facts directly if needed

## Strategic Test Plan

The fix should be judged on behavior, not just code coverage.

### A. Unit tests

1. Summary truthfulness
- simulate a turn where assistant says "I wrote the file" but no write tool succeeded
- expected: summary does not store "file written" as fact

2. Verified fact persistence
- successful `write_file` stores `file_written`
- successful `edit_file` stores `file_edited`
- facts survive session save/load

3. Git fact extraction
- successful exec output for branch/commit/push/PR produces the correct verified facts
- failed exec does not produce them

4. Summary gating
- turn with multiple tool failures does not overwrite summary with a confident completion narrative

5. Prompt injection
- future turn receives the deterministic verified-state block
- verified state is present even if the free-form summary is sparse

### B. Integration tests

1. Repo-task correction flow
- turn 1: request file creation but force tool failure after assistant claims success
- turn 2: ask what happened
- expected:
  - no false "written" fact
  - answer reflects attempted/unverified status

2. Partial repo progress
- branch creation succeeds, commit fails
- next turn should know branch exists but commit does not

3. Message-tool split delivery
- if a file is delivered by `message`, verified state should include delivered attachment/file path
- later turn should refer to that file as real output, not inferred output

### C. Live canaries on `data3`

Use a dedicated mapped Discord workspace and run three scripted scenarios.

1. False-success guard
- ask sciClaw to modify a repo while blocking one critical tool step
- verify the follow-up answer says attempted/partial, not done

2. Verified continuity
- ask sciClaw to create a skill file and branch
- follow with "what happened"
- verify the answer references exact verified artifacts and branch names

3. TMS-style stress case
- repo task with code edits + web research temptation
- verify that later turns reflect verified state and do not invent commits/pushes/PRs

### D. Success metrics

Track before/after on a canary channel:
- count of turns where later messages contradict prior claimed execution state
- count of follow-up turns needing to "correct" a prior completion claim
- token usage for second-turn clarification after a failed or partial repo task
- proportion of follow-up answers that cite verified local artifacts correctly

Primary success criterion:
- a failed or partial execution must no longer be summarized as completed state

Secondary success criterion:
- follow-up clarification turns should become shorter and more direct because the prompt contains verified state

## Rollout Plan

Phase 1
- add verified fact ledger for file and repo execution states
- inject verified state into prompt
- harden summarizer prompt

Phase 2
- add summary gating on unstable turns
- add more fact types if needed

Phase 3
- revisit runaway-token behavior separately once false-state carryover is reduced

## Open Questions

1. Should verified facts live inside `SessionData` or adjacent to artifacts?
- recommendation: start inside `SessionData` for minimal plumbing

2. Should `exec` facts be pattern-matched centrally or emitted by a higher-level git tool?
- recommendation: start with narrow central pattern matching for existing flows

3. Should free-form summary be truncated further once verified state exists?
- probably yes, but not required for the first fix
