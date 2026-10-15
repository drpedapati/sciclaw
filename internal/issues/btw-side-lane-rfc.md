# RFC: `/btw` as a First-Class Read-Only Task

## Status

Implemented on `main` as the current design direction for `/btw`.

What shipped:
- explicit `/btw` task routing
- same-workspace task model
- read-only capability filtering instead of hidden downgrade from ordinary prompts
- queue-aware cards and controls
- typed read-only tools like `weather_forecast`, `pubmed_search`, and `pubmed_fetch`

Remaining gap:
- broader soak/UX validation under real Discord traffic

## Summary

`/btw` should stop being treated as a semi-separate side-lane product and instead become a first-class task in the same workspace with a hard read-only contract.

The intended model is simple:

- same workspace
- same session and prompt context
- same scheduler and job model
- same cards, queueing, status, and cancel semantics
- one difference: `/btw` is enforced read-only

This RFC supersedes the earlier framing that treated `/btw` as a distinct side-lane product with its own capability surface. That framing overfit the current implementation. The simpler model is better.

## Why This RFC Exists

The queue-first scheduler fixed the original problem:

- normal user turns are no longer silently downgraded into a reduced runtime
- normal work now stays full-capability and queues per workspace

That part was correct.

The next issue surfaced with the Cincinnati weather example:

- `/btw` ran correctly as a queued/overlapping task
- mention handling worked
- Discord response flow worked
- but the answer quality was poor because `/btw` had only a narrow tool subset and fell back to generic web search plus a weak fetched page

The wrong conclusion would be:

- "weather is broken"
- or "the queue system is broken"

The real conclusion is:

- `/btw` is currently implemented as a separate reduced runtime
- that makes it easy for capability drift and odd gaps to accumulate

The clean fix is not to keep inventing a second product surface. It is to make `/btw` a normal task with one clear property: `read_only=true`.

## Problem Statement

Right now `/btw` is conceptually muddled.

Users think:

- quick side question
- same assistant
- same context
- just don't let it change things

The implementation currently behaves more like:

- separate tool profile
- separate runtime prompt
- separate persistence behavior
- partially separate capability model

That is more complexity than the user intent requires.

The core mistake is that `/btw` is being modeled as a mini runtime instead of a normal task with a strict mutation boundary.

## Design Goal

`/btw` should mean exactly this:

- "Run this as a normal task in the current workspace and with the current context, but do not allow any mutation or side effects."

That is the whole concept.

Everything else should follow from that.

## Proposed Model

### 1. `/btw` is a first-class task type

`/btw` is not a different scheduler world.

It is a normal task with:

- the same workspace
- the same session key
- the same relevant history/context
- the same queue/status model
- a hard read-only execution contract

### 2. `/btw` remains explicit

Normal user turns remain full-capability main tasks.

`/btw` is still opt-in. It is not inferred from wording like:

- search
- find
- research
- quick question

That inference model already caused too much damage.

### 3. Read-only is enforced by the runtime

This is the critical point.

`/btw` must not mean:

- "please try to behave read-only"

It must mean:

- the runtime will prevent mutation and side effects

### 4. Same context, no mutation

The correct mental model is:

- read the same world
- do not change the world

That means `/btw` should be allowed to:

- read session/history
- read workspace files
- use safe lookup tools
- answer questions from current context

And `/btw` should be blocked from:

- writing files
- editing files
- appending files
- deleting or renaming files
- shell execution
- attachments or document delivery
- outbound side effects beyond the reply itself
- persistent session mutation if we choose to keep `/btw` ephemeral

## Gap Analysis

### 1. Conceptual gap

Current `/btw` is over-modeled.

It behaves like a distinct side product when the real user intent is much simpler:

- same task system
- same context
- read-only contract

This is the largest design gap.

### 2. Runtime/profile gap

Today `/btw` is implemented as a separate tool profile with an explicit narrowed registry.

That creates two problems:

- missing capability surprises
- long-term drift between main-lane and `/btw` behavior

This is why a trivial weather question could end up on generic web tools in a brittle way.

### 3. Tooling gap

The real issue is not only missing weather.

The bigger issue is that `/btw` currently gets a hand-picked partial tool set instead of a principled read-only capability mask.

That means every time the main system grows, `/btw` risks falling behind unless we remember to hand-curate parity.

### 4. Mutation-boundary gap

The important distinction is not:

- main lane vs weird side lane

It is:

- mutating vs non-mutating operations

The codebase still encodes too much of `/btw` around the old side-lane concept instead of around a formal read-only contract.

### 5. State semantics gap

Today `/btw` is ephemeral:

- it can read history
- it does not persist new session state

This may still be the right decision, but it is a separate policy choice from read-only.

We should not conflate:

- read-only
- non-persistent

Those are related but distinct properties.

### 6. Escalation gap

If `/btw` needs mutation, export, or delivery, the system should refuse immediately and tell the user to submit a normal task.

That boundary should be enforced centrally, not improvised in prompts.

### 7. Testing gap

We have tested:

- queue behavior
- `/btw` command routing
- cards and controls

We have not yet fully tested the more important contract:

- can `/btw` see the same context as main tasks?
- does it reliably refuse mutation?
- does it avoid falling into degraded tool paths for ordinary side questions?

### 8. Naming gap

The code still carries legacy naming baggage from `external_readonly` and from the interim side-lane framing.

If the target design is "normal task + read-only contract," the naming should eventually reflect that.

## Non-Goals

- `/btw` should not become full write-capability overlap.
- `/btw` should not be inferred automatically from natural language.
- `/btw` should not require skills to carry scheduler metadata.
- `/btw` should not become a separate mini-agent product.

## Design Principles

1. Preserve a single mental model for tasks.
2. Keep normal work full-capability and queued.
3. Make `/btw` explicit.
4. Enforce read-only at runtime, not just in prompt text.
5. Prefer capability masks over hand-maintained alternate tool worlds.
6. Escalate to a normal queued task when mutation is required.

## Proposed Execution Contract

### Allowed in `/btw`

- reading current session and relevant history
- reading workspace files
- reading summaries and recall context
- using approved non-mutating lookup tools
- replying in-channel with plain text

### Disallowed in `/btw`

- file creation
- file modification
- file deletion
- shell execution
- document generation
- export workflows
- attachment delivery
- background sub-workflows that mutate state
- persistent state mutation, if ephemeral policy is retained

## Architectural Direction

### A. Keep one scheduler model

The scheduler should think in terms of:

- main tasks
- `/btw` tasks

But both are first-class tasks in the same workspace and same job system.

The difference is not queue semantics. The difference is the read-only contract.

### B. Replace partial tool-profile thinking with capability masking

Instead of hand-curating `/btw` as a small alternate tool universe, the system should derive `/btw` behavior from one question:

- is this operation mutating or non-mutating?

That leads to a cleaner architecture:

- tools advertise capability classes
- `/btw` gets only non-mutating capabilities
- main tasks get both mutating and non-mutating capabilities

This is much easier to reason about than maintaining an arbitrary short list forever.

### C. Preserve context parity where safe

`/btw` should keep the same workspace and read context as the main task.

That avoids the current risk that `/btw` feels like a detached assistant with different awareness.

### D. Centralize refusal and escalation

If a `/btw` task attempts a mutating action, the system should fail fast with a clean response like:

- "That needs a normal task. Send it without `/btw` and I’ll queue it."

This should be enforced centrally, not left to prompt wording.

## Alternatives Considered

### 1. Keep `/btw` as a separate side-lane product

Pros:

- explicit separation
- narrow concurrency model

Cons:

- unnecessary conceptual overhead
- capability drift
- more permanent parity problems
- users have to learn a second assistant personality

Rejected.

### 2. Remove `/btw` entirely

Pros:

- simplest model

Cons:

- loses a useful way to ask small side questions during a long-running job

Rejected for now.

### 3. Give `/btw` main-lane parity

Pros:

- fewer missing-tool surprises

Cons:

- breaks the safety purpose
- invites concurrent mutation in the same workspace

Rejected.

## Recommended Plan

### Phase 1: Rewrite the design contract

Update docs and internal language so `/btw` is defined as:

- first-class task
- same workspace/context
- hard read-only contract

### Phase 2: Introduce capability-based gating

Move from hand-picked alternate tool sets toward tool capability classes such as:

- read-only lookup
- read-only workspace inspection
- mutating workspace actions
- outbound delivery actions

Then gate `/btw` against those classes.

### Phase 3: Preserve or refine ephemeral policy

Decide explicitly whether `/btw` should remain:

- read-only and ephemeral
nor
- read-only but session-visible

Current recommendation:

- keep ephemeral until there is a strong product reason not to

### Phase 4: Add fast escalation behavior

When `/btw` encounters a blocked mutating action, it should immediately redirect the user into the normal queue path.

### Phase 5: Fill obvious read-only gaps

Once the contract is right, add missing high-value read-only tools as needed.

Weather is the clearest first example.

The important order is:

1. fix the contract
2. then fill capability gaps

Not the other way around.

## Acceptance Criteria

1. `/btw` is understandable as a normal task with read-only enforcement.
2. `/btw` sees the same relevant context as the main lane.
3. `/btw` cannot mutate workspace or side-effect state.
4. Unsupported `/btw` requests escalate cleanly to a normal queued task.
5. `/btw` no longer feels like a detached half-runtime.

## Open Questions

1. Should `/btw` keep non-persistent session behavior, or should replies become visible to future task context?
2. Should tool capability classes be explicit metadata on tools, or inferred from tool type?
3. Which high-value read-only tools belong in the first pass after contract cleanup?
4. Should the Discord UI explain `/btw` as "read-only task" instead of "side lane"?

## Bottom Line

The simplest correct model is:

- `/btw` is a first-class task
- in the same workspace
- with the same context
- under a hard read-only contract

That is cleaner than maintaining `/btw` as a separate mini product.
