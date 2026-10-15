# RFC: Make `/btw` an Official Discord Slash Command

## Status

Implemented on `main`.

What shipped:
- real Discord `/btw` application command registration
- interaction handling with deferred ephemeral ack
- routing into the same internal `/btw` task path
- message-form `/btw` retained for compatibility during transition

Remaining gap:
- broader live-validation and any future decision about deprecating plain-text `/btw`

## Summary

`/btw` should become a real Discord application command instead of a plain-text message prefix.

Today `/btw` is implemented as message content parsing inside the normal inbound message path. That works, but it has three user-facing problems:

- it does not appear in Discord's slash-command picker
- it depends on normal message routing rules like mention handling
- it looks official to users while behaving like a private text convention

This RFC proposes a narrow change:

- keep the internal `/btw` task model
- add a real Discord slash-command entrypoint on top of it
- route slash invocations into the same scheduler and read-only `/btw` execution path already used today

The goal is UI/entrypoint correctness, not a redesign of `/btw` semantics.

## Why This RFC Exists

The current `/btw` backend is now in a better place:

- `/btw` is explicit
- it runs in the same workspace/context
- it is enforced read-only
- it has queue/status/cancel semantics

But the Discord entrypoint is still fake.

Typing `/btw ...` in Discord today does **not** invoke a Discord application command. It sends a normal message. sciClaw then notices the literal text prefix and interprets it later.

That mismatch is bad product design because the UI implies one thing while the transport does another.

## Current Implementation

### Entry path today

1. Discord sends a normal `MessageCreate` or `MessageUpdate` event.
2. sciClaw processes the message in [discord.go](/Users/ernie/Documents/irl_projects/260212-sciclaw/pkg/channels/discord.go).
3. The message is published into the normal inbound bus in [base.go](/Users/ernie/Documents/irl_projects/260212-sciclaw/pkg/channels/base.go).
4. The scheduler inspects message content in [jobs.go](/Users/ernie/Documents/irl_projects/260212-sciclaw/pkg/routing/jobs.go).
5. If content begins with `/btw`, the scheduler strips the prefix and marks the job as `JobClassBTW`.

### Important fact

There is currently **no** Discord application-command machinery in the codebase.

I checked for:

- `InteractionCreate`
- command registration
- interaction response handling
- application command setup

None of that exists.

So `/btw` is currently a text convention, not an official command.

## Problem Statement

The backend intent and the Discord UX do not match.

Users reasonably expect `/btw` to:

- appear in the slash-command picker
- autocomplete
- behave consistently without needing a mention workaround
- feel like a first-class bot feature

Instead it currently behaves like:

- a normal message that happens to start with `/btw`
- something still filtered by mention/routing rules unless the channel mapping already allows it
- something that can be edited/rementioned like any other message-based path

That creates unnecessary confusion.

## Gap Analysis

### 1. No command registration

Current state:
- sciClaw never registers `/btw` as a Discord application command.

Gap:
- Discord cannot advertise or autocomplete `/btw`.
- There is no canonical schema for arguments.

Required change:
- add command registration for `/btw`, likely guild-scoped first, then optionally global.

### 2. No interaction event handling

Current state:
- Discord adapter only handles `MessageCreate` and `MessageUpdate`.

Gap:
- slash commands arrive as interactions, not normal messages.
- sciClaw has no code to receive them.

Required change:
- add an `InteractionCreate` handler in the Discord channel.

### 3. No interaction response lifecycle

Current state:
- sciClaw sends normal outbound messages through the bus.

Gap:
- slash commands must be acknowledged quickly.
- long-running work needs deferred responses and follow-up/edit behavior.

Required change:
- add Discord interaction ack/defer/follow-up handling.
- likely bridge interaction responses onto existing progress-card + final-reply behavior.

### 4. `/btw` still depends on message routing rules

Current state:
- message-form `/btw` is still a normal inbound message.
- whether it runs depends on the same routing gates as any message.

Gap:
- a first-class side-question command should not depend on text-message mention semantics.

Required change:
- slash-command invocations should bypass normal message-prefix ambiguity and route directly into `/btw` submission.

### 5. Argument model is implicit and weak

Current state:
- `/btw` is parsed by stripping a prefix from raw content.

Gap:
- no structured argument validation
- no discoverable help text
- no room for future options like:
  - `context: current-channel`
  - `mode: read-only`
  - `reply_style: terse`

Required change:
- define a formal slash-command option schema.
- Minimum viable option set:
  - `prompt` string, required

### 6. Progress/final reply mapping is message-oriented

Current state:
- progress cards and final replies assume a chat/channel message context.

Gap:
- slash commands need a coherent mapping from:
  - interaction
  - deferred response
  - progress edits
  - final reply/follow-up

Required change:
- decide whether progress embeds edit the deferred interaction response or send follow-up messages.

Recommended path:
- defer the interaction immediately
- use the deferred response message as the initial progress card when possible
- keep later final answer as edit or follow-up depending on size/attachments

### 7. Permission/install path is incomplete

Current state:
- bot startup opens the Discord gateway and handles messages.

Gap:
- official slash commands require proper Discord application command scope and lifecycle.

Required change:
- ensure bot invite/install includes `applications.commands`
- document guild-scoped vs global rollout behavior

### 8. Testing gap

Current state:
- tests cover message-form `/btw`
- tests do not cover slash-command invocation or interaction response flow

Required change:
- add adapter tests for:
  - command registration payloads
  - `InteractionCreate` handling
  - defer/follow-up behavior
  - mapping to `/btw` job submission

## Proposal

### Design principle

Keep the internal `/btw` scheduler/runtime exactly as the source of truth.

Add a new Discord-specific entrypoint:
- real slash command in
- same internal `/btw` job out

### Proposed command

`/btw`

Arguments:
- `prompt` (string, required)

Possible future options:
- `brief` (boolean)
- `cite` (boolean)
- `channel_context` (boolean)

Do not add those yet.

### Proposed flow

1. Discord interaction arrives for `/btw`.
2. sciClaw validates the `prompt` option.
3. sciClaw sends an immediate deferred interaction response.
4. sciClaw converts the interaction into an internal `InboundMessage` equivalent tagged as `/btw`.
5. Existing `JobManager` handles it as `JobClassBTW`.
6. Progress cards and final response are attached to the interaction response/follow-up channel context.

### Backward compatibility

Keep message-form `/btw` temporarily.

Reason:
- avoids breaking users immediately
- allows staged migration

But mark the message-prefix form as legacy in docs/help once slash commands are live.

## Alternatives Considered

### A. Keep `/btw` as plain text only

Rejected.

Reason:
- poor UX
- mismatch with user expectation
- unnecessary dependence on message-routing semantics

### B. Remove `/btw` entirely and force queue-only workflow

Rejected.

Reason:
- `/btw` has clear product value for lightweight side questions
- the backend model is now sound enough to preserve

### C. Build a generic slash-command framework first

Not for the first slice.

Reason:
- over-scopes the problem
- `/btw` can be the pilot command
- we can generalize afterward if the pattern is good

## Implementation Phases

### Phase 1: Minimal official `/btw`

- register `/btw` command
- handle `InteractionCreate`
- support required `prompt` option
- defer immediately
- map into existing `/btw` scheduler path
- return progress + final response through the interaction lifecycle

### Phase 2: Polish and migration

- update help text and onboarding
- steer users away from plain-text `/btw`
- decide whether to keep or deprecate message-form `/btw`

### Phase 3: Generalization

- if good, extract a reusable Discord slash-command bridge for future commands

## Success Criteria

1. `/btw` appears in Discord slash-command UI.
2. Invoking `/btw` does not depend on mention-only text routing.
3. Slash `/btw` reaches the existing `JobClassBTW` execution path.
4. Progress and final reply work correctly for long-running `/btw` tasks.
5. Existing queue/status/cancel behavior still works.
6. Message-form `/btw` continues to work during migration unless explicitly removed.

## Regression Tests

### Discord adapter
- command registration test
- interaction handling test
- deferred response test
- follow-up/progress edit test

### Routing/scheduler
- slash `/btw` maps to `JobClassBTW`
- slash `/btw` still respects read-only contract
- slash `/btw` does not enter main write lane

### Backward compatibility
- plain-text `/btw` still works during transition

## Open Questions

1. Should `/btw` be guild-scoped first or globally registered from day one?
2. Should progress reuse the deferred interaction response message or send a separate progress embed?
3. Once slash `/btw` is live, do we keep plain-text `/btw` indefinitely or deprecate it?
4. Do we want a second official command later for queue introspection, or keep queue controls as replies/messages only?

## Recommendation

Implement Phase 1 only.

That gives us the product fix we actually need:
- official Discord UX
- minimal backend change
- reuse of the existing `/btw` runtime and scheduler

The core point is simple:

- do not redesign `/btw` again
- just make the Discord entrypoint honest and first-class
