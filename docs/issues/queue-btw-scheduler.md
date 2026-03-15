# Queue-First Scheduler and Explicit `/btw` Side Lane

## Summary

Replace implicit `external_readonly` job classification with a queue-first scheduler:

- Normal user turns always become full-capability main jobs.
- Main jobs are queued per workspace.
- A restricted read-only side lane exists only when the user explicitly opts into it with `/btw`.
- Job references are stable 5-character tokens.
- Queue position is shown separately from the job reference.

This keeps scheduling concerns out of skills and preserves user intent for ordinary requests.

## Why

The current scheduler is making capability decisions too early from shallow text heuristics. That created the `tms-people` failure:

1. A user explicitly invoked `pubmed-literature-brief`.
2. The classifier saw a research-shaped prompt and picked `external_readonly`.
3. The runtime removed write/export tools.
4. The skill contract and runtime no longer matched.
5. The model exposed the mismatch in the user-facing reply.

The problem was not the skill. The problem was hidden reinterpretation by the scheduler.

## Target Behavior

### Main jobs

- Every normal inbound Discord turn becomes a full-capability main job.
- If the workspace is idle, the job starts immediately.
- If a main job is already running, the next main job is queued.
- Main jobs are never silently downgraded into a reduced tool profile.

### `/btw` jobs

- `/btw` is an explicit side-question lane.
- It is read-only and ephemeral.
- It can run while a main job is already running in the same workspace.
- It does not mutate workspace files, session state, or outbound deliverables.
- If the user asks `/btw` to do something write-capable, the agent should refuse and tell them to send it as a normal job.

### Job identity

- Each job gets:
  - a stable 5-character display reference, e.g. `k4m9t`
  - a mutable queue position, e.g. `#2`
- The display reference is used for `status`, `cancel`, and `force`.
- Queue position is informational only.

## Architecture Changes

### 1. Replace `JobClass` with explicit job kind semantics

The scheduler should stop using capability classes as the primary concept.

New model:

- `main`
- `btw`

`main` determines queue behavior.
`btw` determines use of the restricted side profile.

### 2. Real per-workspace queue

Per workspace/runtime target:

- `running_main`: at most one
- `queued_main`: ordered list
- `running_btw`: optional one

Queue state should be persisted and rehydrated on restart.

### 3. Remove implicit readonly inference

Ordinary user turns should no longer be classified as readonly from words like:

- `search`
- `find`
- `research`
- `which`

That heuristic is too brittle and changes capability incorrectly.

### 4. Keep the restricted tool profile, but only for `/btw`

The current restricted registry is still useful, but it should be repurposed:

- old meaning: hidden `external_readonly`
- new meaning: explicit `/btw` side lane

### 5. Queue-aware controls

Add:

- `status`
- `status <ref>`
- `cancel <ref>`
- `force <ref>`

The queue UI should make clear:

- which job is running
- which jobs are queued
- what the user can do next

## Implementation Order

### Phase 1: Scheduler correction

- Remove implicit ordinary-turn routing into `external_readonly`
- Queue extra main jobs instead of sending busy/fallback messages
- Keep one running main job per workspace

### Phase 2: Job references and queue controls

- Replace `J1`-style labels with stable 5-character refs
- Add queue position to job records and cards
- Add `force <ref>`

### Phase 3: Explicit `/btw`

- Add `/btw` command parsing
- Route `/btw` into the restricted side profile
- Make `/btw` ephemeral and read-only

### Phase 4: Cleanup and rename

- Remove `external_readonly` language from config, logs, and UI
- Rename the restricted profile to reflect `/btw`
- Keep fallback guards that suppress internal runtime details in user-facing replies

## Files Expected to Change

- `pkg/routing/jobs.go`
- `pkg/routing/jobs_test.go`
- `pkg/routing/pool.go`
- `pkg/agent/loop.go`
- `pkg/agent/loop_test.go`
- `pkg/config/config.go`
- `cmd/picoclaw/main.go`

## Non-Goals

- Do not make skills carry scheduler metadata just to survive the queue.
- Do not introduce per-file locking.
- Do not make `/btw` an implicit fallback for research-looking messages.

## Open Questions

- Whether `/btw` allows one concurrent side job per workspace or a small side queue.
- Whether queued jobs should survive restart as `queued` or be re-marked for manual replay.
- Whether `force <ref>` should preempt only queued jobs or also stop and replace a running job.

## Current Decision

- Default to preserving capability.
- Queue normal work.
- Make readonly overlap explicit.
