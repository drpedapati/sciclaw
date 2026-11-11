# RFC: Add a Real Discord `/skill` Slash Command

## Status

Implemented on `main`.

What shipped:
- real Discord `/skill name:<skill> prompt:<task>` command
- workspace-aware autocomplete
- submit-time validation against the routed workspace skill catalog
- reuse of the normal task pipeline instead of a separate skill runtime

Remaining gap:
- more live Discord soak on autocomplete/interaction UX

## Summary

Add a single Discord slash command:

- `/skill name:<skill> prompt:<task>`

The command should:

- autocomplete skills available in the current workspace
- validate the selected skill server-side on submit
- defer immediately in Discord
- re-enter the existing sciClaw task pipeline as a normal task with an explicit instruction to use the selected skill

This is intentionally narrow. It does **not** introduce a new skill runtime, a new scheduler lane, or one slash command per skill.

## Why This RFC Exists

We now have a real `/btw` slash command, but explicit skill invocation in Discord is still weak:

- users have to remember skill names and type them in normal messages
- Discord provides no discovery or autocomplete for available skills
- skills are workspace-scoped, so a static global command list would be wrong

The right product surface is one generic `/skill` command that is workspace-aware.

## Current State

### Skills already have a catalog primitive

The codebase already exposes a workspace-aware skill catalog in [loader.go](/Users/ernie/Documents/irl_projects/260212-sciclaw/pkg/skills/loader.go):

- `SkillsLoader.ListSkills()`
- workspace skills override global skills
- global skills override builtin skills

### Discord already has slash-command plumbing

The Discord adapter in [discord.go](/Users/ernie/Documents/irl_projects/260212-sciclaw/pkg/channels/discord.go) already supports:

- command registration
- interaction handling
- deferred ephemeral ack
- mapping `/btw` into the normal inbound bus

So `/skill` does not require a second command framework.

### Important constraint

Skills are still used through the normal agent path. There is no structured internal `RunSkill(name, prompt)` API.

That means the lean implementation should:

- validate the skill name in the channel adapter
- synthesize an explicit user message that tells the agent to use that skill
- let the existing scheduler, routing, and tooling handle execution

## Gap Analysis

### 1. No Discord-native skill discovery

Current state:
- skill use in Discord is text-only
- Discord cannot suggest available skills

Gap:
- users cannot discover workspace-available skills from the UI

Required change:
- add `/skill`
- add autocomplete for the `name` option

### 2. Skill availability is workspace-dependent

Current state:
- skill availability depends on routing and workspace resolution

Gap:
- a static global skill list would leak the wrong skills into the wrong channel

Required change:
- resolve the current workspace from the Discord channel before serving autocomplete or validating submit

### 3. No server-side validation path for explicit skill invocation

Current state:
- the agent can see available skills, but Discord has no submit-time validation for a chosen skill

Gap:
- autocomplete must not be treated as authoritative
- a stale client choice or mismatched workspace could submit an unavailable skill

Required change:
- re-list skills on submit and reject unavailable names cleanly

### 4. No structured argument model for skill invocation

Current state:
- users must manually express both the skill name and the task in free-form text

Gap:
- that is weak UX and brittle to parse mentally

Required change:
- formal slash-command schema:
  - `name` string, required, autocomplete enabled
  - `prompt` string, required

### 5. No clean boundary between channel UX and routing/skills logic

Current state:
- the Discord adapter should not import routing decisions or skill precedence rules directly

Gap:
- `/skill` autocomplete needs workspace-aware data
- but pushing routing logic into `pkg/channels` would couple the wrong layers

Required change:
- provide a callback from `cmd/picoclaw/main.go` into the Discord channel for workspace-aware skill lookup

### 6. No skill-specific slash execution path is needed

Current state:
- the existing task pipeline already handles normal jobs, `/btw`, routing, queueing, and progress cards

Gap:
- none at the execution layer, if we are willing to synthesize an explicit skill-use instruction

Required change:
- convert the slash command into a normal inbound task message
- do not build a second skill execution stack

## Proposal

### Command

`/skill`

Options:
- `name` — required string with autocomplete
- `prompt` — required string

### Autocomplete behavior

- workspace-aware
- case-insensitive match against skill name and description
- return up to 25 suggestions
- sort exact/prefix matches above fuzzy matches

### Submit behavior

1. validate allowlist
2. validate `name`
3. validate `prompt`
4. resolve the available skills for the current channel/workspace
5. reject if the skill is unavailable there
6. defer ephemerally
7. publish an inbound task into the existing pipeline with content equivalent to:

`Use the skill "<name>" for this task. Read its SKILL.md first and follow it.\n\nTask:\n<prompt>`

### Routing model

- use the same routing resolution model as normal Discord traffic
- treat slash invocation as a direct bot invocation
- if routing is disabled, fall back to the default workspace

### Backward compatibility

- keep normal text-based skill invocation working
- `/skill` is additive

## Alternatives Considered

### One slash command per skill

Rejected.

Why:
- too many commands
- workspace-dependent skills do not fit a static command surface
- operationally messy to register/unregister dynamically

### A new structured internal skill runtime

Rejected for the first slice.

Why:
- too much change
- not required to deliver Discord-native UX
- the existing task pipeline already works if we provide a clear instruction

## Acceptance Criteria

1. `/skill` appears in Discord as a real slash command
2. `name` autocompletes workspace-available skills
3. submit rejects unavailable skills cleanly
4. submit defers immediately and creates a normal task
5. the resulting task uses the selected skill without introducing a second execution path
6. plain-text skill invocation continues to work

## Open Questions

1. Should `/skill` be global or guild-scoped during rollout?
2. Should autocomplete include descriptions in ranking only, or expose source labels too?
3. Should the final task remain a main-lane task by default, or allow a later `/skill-btw` variant?
