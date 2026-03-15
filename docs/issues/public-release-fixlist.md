# Public Release Fix List

This checklist tracks concrete pre-release blockers found during multi-review.
Each item includes a success criterion and a regression test target.

## 1. Queued Job Submit Must Be Transactional

- Problem:
  - A queued main-lane job can remain live if queue-status/progress send fails, even though the user is told the job failed to start.
- Success:
  - If queue-status send fails, the queued job is rolled back and never executes later.
  - User-visible failure and scheduler state agree.
- Regression tests:
  - `pkg/routing/jobs_test.go`
  - Simulate `SendOrEditProgress` failure during queue enqueue and assert:
    - no queued job remains
    - queued job never runs
    - no stale persisted queued record remains

## 2. Queue Must Survive Restart

- Problem:
  - Restart currently marks queued jobs interrupted instead of preserving backlog.
- Success:
  - Running jobs become interrupted on restart.
  - Queued jobs remain queued and are reconstructible from store.
- Regression tests:
  - `pkg/routing/jobs_test.go`
  - Persist one running and one queued job, construct a new manager, assert:
    - running -> interrupted
    - queued -> queued

## 3. Queued Cancel Must Update Existing Card

- Problem:
  - Cancelling a queued job leaves its progress card visually stale.
- Success:
  - Cancelling a queued job updates the existing status/progress message to cancelled or removed.
- Regression tests:
  - `pkg/routing/jobs_test.go`
  - Assert `SendOrEditProgress` is called for the cancelled queued job and updated state is visible

## 4. Queue Controls Must Match Actual Parsing Rules

- Problem:
  - Cards advertise bare `status/cancel/force`, but parser only treats controls as directed.
- Success:
  - Either:
    - card copy requires direct mention/reply, or
    - reply-based control detection works reliably and matches the card copy.
- Regression tests:
  - `pkg/routing/jobs_test.go`
  - Directed reply / directed mention path works
  - bare undirected control does not get advertised as valid

## 5. mention_only Must Ignore Role Mentions

- Problem:
  - Role mentions can activate sciClaw under mention-only mode.
- Success:
  - mention-only requires:
    - direct bot mention
    - or reply-to-bot
    - or DM
- Regression tests:
  - `pkg/routing/resolver_test.go`
  - `pkg/channels/discord_test.go`
  - Role mention metadata should not pass direct-mention gating

## 6. Anthropic OAuth Precedence Must Be Deterministic

- Problem:
  - `auth_method=oauth|token` can be bypassed by a stale API key in config.
- Success:
  - If Anthropic auth method is explicitly oauth/token, sciClaw uses the auth credential path first.
  - Stale API key does not silently reroute that user to the direct API provider.
- Regression tests:
  - `pkg/providers/claude_agent_provider_test.go`
  - Add precedence tests for:
    - `AuthMethod=oauth` + stale non-oat API key
    - `AuthMethod=token` + stored oat credential

## 7. Claude Agent Bridge Must Preserve Result-Only Success Payloads

- Problem:
  - Bridge responses can carry successful text in `result` with empty `content`.
- Success:
  - Provider uses non-empty `content`, else `result`, for final assistant text.
- Regression tests:
  - `pkg/providers/claude_agent_provider_test.go`
  - Mock bridge response with empty `content` and non-empty `result`

## 8. Non-Routing /btw Must Have channel_history Parity

- Problem:
  - Default gateway `/btw` loop is missing `channel_history` callback wiring.
- Success:
  - Non-routing main lane and `/btw` both have `channel_history` callback when Discord is present.
- Regression tests:
  - add a focused gateway/main wiring test if feasible
  - otherwise unit-test helper wiring extracted from `cmd/picoclaw/main.go`

## 9. /btw Registry Must Not Share Mutable Tool Instances

- Problem:
  - Filtered side-lane registry reuses mutable tool objects from the main registry.
- Success:
  - Main lane and `/btw` have independent contextual tool instances.
  - Context updates in one lane cannot corrupt the other.
- Regression tests:
  - `pkg/tools/*` or `pkg/agent/*`
  - Create main and side-lane registries, set different contexts, assert channel_history reads the correct chat in each

## 10. Subagent Bootstrap Must Stay Workspace-Scoped

- Problem:
  - Subagents can fall back to `$HOME/sciclaw` bootstrap files from unrelated workspaces.
- Success:
  - Bootstrap file loading stays within the task workspace only.
  - Missing local bootstrap files do not import cross-workspace policy.
- Regression tests:
  - `pkg/tools/subagent*_test.go`
  - Workspace A missing files should not load files from workspace B or `$HOME/sciclaw`

## 11. TOOLS.md Freshness Must Require Typed PubMed Guidance

- Problem:
  - Doctor can treat a stale policy file as current even when typed PubMed markers are missing.
- Success:
  - Freshness check requires `pubmed_search` and `pubmed_fetch`.
- Regression tests:
  - `cmd/picoclaw/tools_policy_test.go`
  - Old review-only policy should fail freshness

## 12. Typed PubMed Tools Need Fan-Out Caps

- Problem:
  - `pubmed_search` limit and `pubmed_fetch` PMID fan-out are currently unbounded.
- Success:
  - Search limit is capped to a sane max.
  - Fetch PMID list is capped to a sane max.
  - User gets a clear validation error on overflow.
- Regression tests:
  - `pkg/tools/pubmed_lookup_test.go`
  - Oversized search limit rejected or clamped
  - Oversized PMID list rejected or clamped

## 13. xlsx/pptx Remediation Hints Must Match Supported Install Path

- Problem:
  - Runtime client hints still point at the legacy standalone tap path.
- Success:
  - Client/runtime hints align with current same-tap companion rollout.
- Regression tests:
  - `pkg/xlsxreview/client_test.go`
  - `pkg/pptxreview/client_test.go`
  - Check hint strings
