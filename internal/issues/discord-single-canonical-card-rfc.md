# RFC: Single Canonical Discord Job Card

## Status

Draft.

Related issue:
- [#101](https://github.com/drpedapati/sciclaw/issues/101) Discord job cards make elapsed time look static after embed migration

This RFC addresses the broader Discord card confusion behind that issue:
- static-looking elapsed time
- multiple cards for one job
- control cards competing with the main progress card
- replacement progress cards when edit fallback fires

## Summary

Discord job UX should converge on one rule:

- one job
- one canonical card
- one mutable `StatusMessageID`

Everything else is where the confusion comes from.

Today the system can emit multiple full cards for the same job because:
1. progress edit fallback can create a replacement progress message
2. `status`, `cancel`, and `force` create separate control cards instead of updating the canonical card
3. the canonical card timer looks static because the embed payload does not visibly change in the way users expect

The minimal engineered fix is not a card-system rewrite.

It is:
1. keep exactly one canonical progress card per job
2. update that card for control actions whenever possible
3. only replace the progress card when Discord definitively says the old message no longer exists
4. render elapsed time ourselves so the card visibly changes on heartbeat

## Problem Statement

The current Discord card behavior violates the user mental model.

Users expect:
- one job card that stays current
- reply on that card to control the job
- the card to keep showing that the job is alive

Current behavior can instead produce:
- original progress card
- separate `status` card
- separate `cancel` or `force` acknowledgment card
- replacement progress card if edit fallback sends a new message

That creates three visible problems:

### 1. Duplicate-card confusion

The channel can show multiple `sciClaw · <job-id>` cards for one logical job.

Root causes:
- [pkg/channels/discord.go]( /Users/ernie/Documents/irl_projects/260212-sciclaw/pkg/channels/discord.go ) `SendOrEditProgress()` falls back to `sendProgressMessage()` on any edit error
- [pkg/routing/jobs.go]( /Users/ernie/Documents/irl_projects/260212-sciclaw/pkg/routing/jobs.go ) `handleControl()` always sends standalone control cards

### 2. Ambiguous source of truth

Control replies do not reinforce which card is canonical.

The user sees:
- one card saying queued/running/done
- another card saying treated as `status` / `cancel` / `force`

That makes it unclear which card should be replied to next.

### 3. Static-looking liveness

The progress reporter already heartbeats.

Code:
- [pkg/routing/jobs.go]( /Users/ernie/Documents/irl_projects/260212-sciclaw/pkg/routing/jobs.go ) `progressReporter.runHeartbeat()`

But the visible timer is still weak because:
- the embed uses `Started = <t:...:R>` in a field
- Discord embed rendering does not give the same perceived live count-up effect
- the payload may be edited without visibly changing the timer presentation

## Current Behavior Trace

### A. Progress update path

- `progressReporter.sendLocked()` in [jobs.go]( /Users/ernie/Documents/irl_projects/260212-sciclaw/pkg/routing/jobs.go ) builds `formatProgressOutbound(record)`
- calls `SendOrEditProgress(...)`
- if a new message ID comes back, it rewrites `StatusMessageID`

### B. Discord edit fallback path

In [pkg/channels/discord.go]( /Users/ernie/Documents/irl_projects/260212-sciclaw/pkg/channels/discord.go ):
- if `editProgressMessage()` fails, sciClaw sends a brand new progress message
- that means one job can leave behind stale older cards

Current code is effectively:
- edit existing progress message if possible
- otherwise create a new one regardless of why edit failed

That is too aggressive.

### C. Control-card path

In [pkg/routing/jobs.go]( /Users/ernie/Documents/irl_projects/260212-sciclaw/pkg/routing/jobs.go ) `handleControl()`:
- `status` emits `formatControlRecordOutbound(...)`
- `cancel` emits `formatControlRecordOutbound(...)`
- `force` emits `formatControlRecordOutbound(...)`

These are separate outbound messages, not edits to the canonical progress card.

## Gap Analysis

### 1. There is no explicit canonical-card policy

The code has `StatusMessageID`, but behavior does not consistently respect it as the one card the user should track.

Gap:
- storage model implies one canonical message
- runtime behavior still emits auxiliary full cards

### 2. Edit fallback is too broad

Current fallback treats all edit errors as if the card is gone.

That is not defensible.

There is a meaningful difference between:
- unknown/deleted message
- permission loss
- transient API/network error
- malformed edit payload

Only the first category justifies creating a replacement canonical card.

### 3. Control actions are implemented as separate message products

This is the main UX drift.

`status`, `cancel`, and `force` are currently designed like mini command responses rather than job-card mutations.

That adds card count without increasing clarity.

### 4. Heartbeat exists but does not guarantee visible liveness

This is not a scheduler gap.

It is a rendering/output gap:
- the job system is heartbeating
- the card does not visibly communicate elapsed time in a reliable way

### 5. Tests currently enshrine some undesirable behavior

There is an explicit test for broad edit fallback:
- [pkg/channels/discord_test.go]( /Users/ernie/Documents/irl_projects/260212-sciclaw/pkg/channels/discord_test.go ) `TestDiscordSendOrEditProgressFallsBackToNewMessageOnEditFailure`

There are also tests that expect separate control embeds:
- [pkg/routing/jobs_test.go]( /Users/ernie/Documents/irl_projects/260212-sciclaw/pkg/routing/jobs_test.go ) reply/cancel/force status assertions

Those tests are useful, but they currently lock in the noisy behavior.

## Design Goal

Minimal product rule:

- each job has one canonical card
- replying to that card updates the canonical card
- channel noise is reduced to final answer plus exceptional errors

This should be achieved with the smallest code change set possible.

## Non-Goals

This RFC does **not** propose:
- replacing embeds with a new UI system
- adding Discord buttons or components
- redesigning queue semantics
- redesigning `JobRecord`
- changing non-Discord channels
- removing all text fallback behavior

## Proposed Minimal Solution

### 1. Treat `StatusMessageID` as the canonical card always

Policy:
- all normal progress updates target `StatusMessageID`
- control actions for an existing job should also target `StatusMessageID`
- separate full cards should be exceptional, not routine

### 2. Narrow edit fallback in the Discord adapter

Change [pkg/channels/discord.go]( /Users/ernie/Documents/irl_projects/260212-sciclaw/pkg/channels/discord.go ) `SendOrEditProgress()` so it only creates a replacement card when the edit error is definitively permanent.

Allowed replacement cases:
- unknown message / deleted message
- possibly missing access if we decide there is no recoverable edit path

Do **not** fallback-send on:
- transient network errors
- generic REST failures
- timeouts
- unknown edit errors

In those cases:
- return the edit error
- keep the old `StatusMessageID`
- let the next heartbeat retry the same canonical card

This is the single most important engineering correction.

### 3. Change `status` to refresh the canonical card instead of sending a new card

Minimal behavior:
- if `status` selects a concrete job, call the existing progress-sync/edit path on that job’s canonical card
- optionally send no extra message at all
- if an extra reply is necessary, keep it to a one-line acknowledgement, not a full duplicate card

This cuts the most common unnecessary duplicate card immediately.

### 4. Change `cancel` and `force` acknowledgements to prefer canonical-card mutation

For selected jobs:
- mutate state
- update canonical progress card
- avoid standalone full control card by default

If user feedback is still needed, use a tiny textual acknowledgment such as:
- `Cancelled 0000Q.`
- `Moved 0000R to the front of the queue.`

Do not emit a second full embed that looks like another job card.

### 5. Add server-rendered `Elapsed` to the progress embed

In [pkg/routing/jobs.go]( /Users/ernie/Documents/irl_projects/260212-sciclaw/pkg/routing/jobs.go ) `formatProgressEmbed()`:
- keep `Started`
- add `Elapsed`
- compute from `record.StartedAt` to `time.Now()` when formatting

This makes the heartbeat produce a visibly changing card even if Discord relative timestamps in embed fields remain underwhelming.

## Exact Surgical Change Set

### A. Discord adapter

File:
- [pkg/channels/discord.go]( /Users/ernie/Documents/irl_projects/260212-sciclaw/pkg/channels/discord.go )

Changes:
- add helper that classifies edit errors into:
  - replace-card
  - retry-same-card
- narrow `SendOrEditProgress()` fallback-send behavior accordingly

### B. Job manager control handling

File:
- [pkg/routing/jobs.go]( /Users/ernie/Documents/irl_projects/260212-sciclaw/pkg/routing/jobs.go )

Changes:
- `status` for a concrete selected job should sync/update canonical progress card instead of emitting a standalone control card
- `cancel` and `force` should mutate and refresh canonical card first
- retain list/error/no-job responses as standalone messages because there is no single selected canonical card in those cases

### C. Progress card rendering

File:
- [pkg/routing/jobs.go]( /Users/ernie/Documents/irl_projects/260212-sciclaw/pkg/routing/jobs.go )

Changes:
- add `Elapsed` field to `formatProgressEmbed()`
- compute server-side on each render

## Why This Is the Minimal Safe Fix

Because it leaves the current architecture intact.

We are **not** changing:
- queue model
- job store structure
- progress messenger interface
- message bus model

We are only changing:
- when a new card is allowed to exist
- where control acknowledgements go
- how elapsed time is rendered

That is the highest-leverage, lowest-risk slice.

## Regression Risks

### 1. Over-narrowing edit fallback could hide a truly dead card

Mitigation:
- only suppress fallback-send for retryable errors
- preserve replacement for definitive message-loss errors
- log classification so we can see which path fired

### 2. Users may miss explicit control acknowledgements

Mitigation:
- canonical card state change is the primary acknowledgment
- keep a tiny plain-text acknowledgment if needed
- do not send a second full card

### 3. Tests need to move with the new policy

Expected changes:
- replace the “fallback on any edit failure” Discord test with explicit permanent-vs-transient cases
- adjust job-control tests to assert canonical-card mutation rather than standalone control embeds for selected jobs

## Success Criteria

### Functional
- one selected job control action does not create a second full job card
- transient progress-edit failures do not create replacement cards
- definitive deleted-message errors still recover by creating one replacement canonical card

### UX
- a single job has one obvious card to reply to
- `status` does not clutter the channel with duplicate full cards
- the progress card visibly shows elapsed runtime advancing

### Logging / observability
- progress edit fallback logs whether the error was classified as permanent or retryable
- when a replacement card is created, that event is explicit in logs

## Test Plan

### Discord adapter tests
- edit failure classified as transient -> no replacement send
- edit failure classified as unknown-message/not-found -> replacement send occurs

### Job manager tests
- `status` on a selected job refreshes canonical card and does not emit a full control embed
- `cancel` on a selected queued job updates canonical progress card to cancelled and avoids a second full card
- `force` on a selected queued job updates canonical progress card and queue ordering without a duplicate full card
- no-job and multi-job control cases still emit standalone helper cards

### Card rendering tests
- `formatProgressEmbed()` includes a server-rendered `Elapsed` field
- repeated heartbeat renders change the `Elapsed` value over time

## Recommended Rollout

1. land the adapter fallback narrowing first
2. land selected-job control-card suppression second
3. land `Elapsed` rendering third
4. canary on `data3` in a noisy real Discord channel

## Bottom Line

The duplicate-card problem is not a reason to redesign the jobs system.

It comes from two narrow implementation choices:
- edit fallback is too eager to create a replacement card
- selected-job control commands emit separate full cards instead of updating the canonical one

The minimal fix is to correct those two behaviors and add a server-rendered elapsed field.
