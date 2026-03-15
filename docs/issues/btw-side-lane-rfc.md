# RFC: Make `/btw` an Explicit, Coherent Side-Lane Product

## Status

Draft

## Summary

`/btw` should remain an explicit side lane, but it should stop behaving like "main lane minus some tools." The current implementation is operational, but its capability contract is implicit and incomplete. The weather failure exposed the core issue: `/btw` is a separate runtime profile without a deliberately designed set of supported task classes.

This RFC proposes:

- keeping `/btw` explicit rather than inferred
- treating `/btw` as a distinct product surface with a documented capability matrix
- adding typed, structured tools for the specific low-risk side questions `/btw` is meant to handle
- failing fast when `/btw` is asked to do work that belongs in the main queue
- keeping normal user turns in the main lane and out of reduced runtimes

## Problem Statement

The queue-first scheduler fixed the worst problem: normal user requests are no longer silently downgraded into a reduced read-only runtime. That was the right change.

However, `/btw` is still underdefined.

Today `/btw` is:

- explicit in the scheduler
- concurrent with a running main job
- ephemeral
- restricted to a small tool set

But it is not yet clear what kinds of user requests `/btw` is supposed to handle well.

The current side-lane runtime exposes only:

- `web_search`
- `web_fetch`
- `pubmed_search`
- `pubmed_fetch`

This has two consequences:

1. Some side questions are handled well, especially PubMed lookups.
2. Many ordinary lightweight questions fall back to generic web search and page scraping, which is fragile.

The live weather example made this obvious:

- user asked `/btw what is the weather in cincinnati tomorrow?`
- the side lane had no structured weather tool
- the model used `web_search`, then `web_fetch`
- it chose a low-quality page and returned a degraded answer after spending far too many tokens and too much time

That was not a queue bug. It was a side-lane capability-design bug.

## Current State

### What `/btw` does well now

- explicit user opt-in
- does not silently reinterpret ordinary requests
- can overlap with a running main job
- does not mutate workspace files or session state
- works well for PubMed lookup because it has typed PubMed tools
- has queue-aware Discord cards and controls

### What `/btw` is today in implementation terms

- separate scheduler lane
- separate tool profile
- separate runtime constraints
- ephemeral session behavior
- reduced tool registry

This means `/btw` is not a lightweight alias for the main lane. It is already a distinct runtime surface.

## Gap Analysis

### 1. Capability contract gap

The biggest problem is that `/btw` does not yet have an explicit product contract.

Users reasonably infer:

- "small side question"
- "quick chat while another job runs"
- "same assistant, lighter mode"

But the system actually provides:

- a separate runtime with a hardcoded tool subset
- different persistence semantics
- different failure modes

That mismatch creates user surprise.

### 2. Tool coverage gap

The side lane is too sparse for the kinds of questions users will naturally ask there.

Current coverage is strong for:

- PubMed lookup
- generic web search

Current coverage is weak for:

- weather
- other structured current-info lookups
- quick utility questions that need typed, high-trust sources rather than generic search pages

The important detail is that weather is not just a `/btw` problem. There is no dedicated weather tool in the system today. But `/btw` amplifies the problem because it strips the runtime down to generic web plus PubMed.

### 3. Generic web overuse gap

In `/btw`, generic web tools are doing too much work.

That creates predictable failure modes:

- search snippets used as pseudo-APIs
- fetches against low-quality SEO pages
- JS-heavy pages yielding low-quality extracted text
- slow, token-heavy multi-step retrieval for simple questions

For simple side questions, this is the wrong shape of toolchain.

### 4. Skill compatibility gap

`/btw` still shares much of the broader prompt and skill environment, but most skills were not designed as side-lane skills.

This creates ambiguity:

- some skills are meaningfully usable in `/btw`
- some are partially usable if write/export steps are skipped
- some should be rejected immediately

Right now this boundary is too dependent on model judgment.

### 5. Escalation/refusal gap

`/btw` does not yet have a strong capability-aware refusal layer.

For unsupported work, the best behavior is:

- refuse early
- explain that the request belongs in the main queue
- preserve the user’s intent instead of trying to improvise badly

Today the system still leans too much on prompt-based adaptation.

### 6. State/continuity gap

`/btw` is ephemeral:

- it can read current session history and Discord recall context
- it does not persist new session state
- it does not contribute to long-term summary state

This is defensible for disposable side questions, but it means `/btw` is not a true continuing conversational lane.

That needs to be explicit in the product design rather than an incidental implementation detail.

### 7. Observability gap

The scheduler and cards are in decent shape, but the product does not yet expose a clear capability story for `/btw`.

What we should be able to answer easily:

- what tool categories `/btw` supports
- which `/btw` requests escalate to the main queue
- which tool classes most often fail in `/btw`
- which domains or tasks are producing low-quality answers

Today this requires log inspection.

### 8. Testing gap

The queue and card path is tested and canaried.

What is not yet comprehensively tested is the capability contract itself:

- supported `/btw` task classes succeed through the intended tools
- unsupported `/btw` task classes fail early and cleanly
- common utility asks do not degrade into brittle generic-web behavior

### 9. Naming and mental-model gap

The implementation moved away from implicit `external_readonly`, which was the right decision. But `/btw` still risks becoming "the weird side runtime" instead of a clearly defined mode.

That is a product-design problem, not just a code problem.

## Design Principles

1. `/btw` should be explicit.
2. Normal user turns stay in the main lane.
3. `/btw` should support a small number of side-question classes very well.
4. `/btw` should refuse unsupported work quickly and clearly.
5. `/btw` should prefer typed, structured tools over generic web tools whenever possible.
6. `/btw` should remain safe to run concurrently with a main job.

## Proposed Direction

### A. Keep `/btw`, but define supported task classes

Initial intended categories:

- quick current-info lookup
- quick PubMed/citation lookup
- quick clarification/chat question
- quick read-only reference question

If a request does not fit those categories, `/btw` should not attempt to stretch.

### B. Build the `/btw` tool profile around those categories

The side-lane tool profile should contain only tools that are:

- read-only
- low-side-effect
- high-trust for their task class
- fast enough for opportunistic use

Examples of the kind of additions that fit this model:

- a typed weather tool
- other typed structured lookup tools for common side questions

This is better than asking generic search and fetch tools to behave like APIs.

### C. Add a capability-aware refusal/escalation layer

If a `/btw` request needs:

- file writes
- attachments
- exports
- delivery workflows
- shell execution
- richer workspace mutation

then the system should refuse immediately and say:

- send it normally to place it in the main queue

This should not depend on the model improvising that boundary.

### D. Treat generic web as a fallback, not a primary side-lane contract

`web_search` and `web_fetch` remain useful, but they should not be the primary answer for structured, high-confidence utility questions.

If `/btw` keeps generic web tools, they should be backed by stronger task routing rules and source preferences.

### E. Make `/btw` intentionally disposable

Keep `/btw` ephemeral unless and until there is a strong reason to make it stateful.

That means:

- it can use current context
- it should not quietly reshape long-term workspace/session state

This is a feature, not a bug, if we define `/btw` correctly.

## Alternatives Considered

### 1. Remove `/btw` entirely

Pros:

- simplest mental model
- no second runtime surface

Cons:

- loses safe overlap for small side questions
- forces every interruption into the main queue

### 2. Give `/btw` main-lane parity

Pros:

- fewer surprises from missing tools

Cons:

- defeats the concurrency-safety purpose
- increases risk of workspace mutation collisions
- makes `/btw` an alternate scheduler, not a side lane

### 3. Keep current `/btw` and just patch prompts

Pros:

- cheapest

Cons:

- does not solve capability gaps
- leaves too much to model improvisation
- repeats the same class of failures on other utility tasks

## Recommendation

Choose a narrow, intentional `/btw` product rather than full parity or removal.

Specifically:

- keep `/btw` explicit
- keep it read-only and ephemeral
- give it a deliberately curated typed tool set for common side questions
- add hard refusal/escalation when a request belongs in the main queue
- stop treating generic web as the answer to every missing side-lane capability

## Implementation Plan

### Phase 1: Capability matrix

Document the supported `/btw` task classes and their intended tools.

Deliverables:

- internal capability matrix
- user-facing help copy for `/btw`

### Phase 2: Typed utility tools for `/btw`

Add the first missing structured tools for common side questions.

Priority candidate:

- weather

Goal:

- a weather question should be one structured call and one short answer
- not search plus arbitrary page fetches

### Phase 3: Refusal and escalation rules

Implement lane-aware refusal when `/btw` requires main-lane capabilities.

Examples:

- file creation
- attachment delivery
- shell commands
- multi-step workspace workflows

### Phase 4: Generic web tightening

Improve side-lane behavior when generic web is used:

- better source preferences
- stricter task-routing rules
- avoid low-trust weather or utility pages when typed sources exist

### Phase 5: Testing and telemetry

Add explicit coverage for:

- successful supported `/btw` categories
- clean refusal on unsupported `/btw` requests
- latency and token-budget sanity for common side questions

## Acceptance Criteria

1. `/btw` behavior is predictable from a documented capability contract.
2. Simple current-info questions do not require brittle generic-web scraping.
3. Unsupported `/btw` requests fail fast and route users to the main queue.
4. `/btw` remains concurrency-safe with a running main job.
5. Users no longer have to infer what `/btw` can do from trial and error.

## Open Questions

1. Which typed utility tools belong in the first `/btw` bundle beyond weather?
2. Should `/btw` have a token or latency budget stricter than the main lane?
3. Should `/btw` ever persist its own short-lived conversational state, or stay fully disposable?
4. Should `/btw` expose a visible "supported uses" help card in Discord?

## Bottom Line

The queue-first scheduler fixed the biggest architectural problem: normal work is no longer silently downgraded.

The next problem is different: `/btw` is now explicit, but it is still underdesigned.

The right move is not to make `/btw` equal to the main lane.
The right move is to make `/btw` a deliberately small, typed, trustworthy side-lane product.
