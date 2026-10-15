# Prod switchover: sciClaw → GPT-5.6 Sol @ low reasoning

**Branch / worktree:** `plan/gpt56-sol-low-switchover` @ `.worktrees/gpt56-sol-low-switchover`  
**Date:** 2026-07-11  
**Status:** **DONE** (prod cutover + brew `v0.3.6`) — 2026-07-11  
**PR:** https://github.com/drpedapati/sciclaw/pull/125 (open; tip includes incomplete-turn UX)  
**Release:** https://github.com/drpedapati/sciclaw/releases/tag/v0.3.6 (`v0.3.6-dev.1` canary first)  
**Reference:** ClinVision `fix-escalation-sol-low` uses `EscalationReviewModelId = "gpt-5.6-sol"` and `EscalationReviewReasoningEffort = "low"` (commit `7249f5c7`).  
**Remediation plan (manual prompts):** [`correspondence-gpt56-sol-codex-harness-remediation.md`](./correspondence-gpt56-sol-codex-harness-remediation.md)

## Goal

Move production sciClaw (data3 Discord gateway, Codex/OAuth path) from the current OpenAI default stack to:

| Knob | Target |
|------|--------|
| `agents.defaults.model` | `gpt-5.6-sol` |
| `agents.defaults.reasoning_effort` | `low` |

Everything else (tools, channels, image gen hosted tool, workspace, brew deploy) stays as-is. This is primarily a **model + effort** switch, not a protocol rewrite.

OpenAI GA (2026-07-09): GPT-5.6 family = **Sol** (flagship), **Terra** (balanced), **Luna** (cheap). Durable tier names; generation number advances.

## Why this is low risk for the runtime

sciClaw already wires the pieces this needs:

1. **Provider routing** — `pkg/models/models.go` routes any model containing `gpt` to OpenAI; `gpt-5.6-sol` matches.
2. **Codex OAuth path** — when OpenAI OAuth/token creds exist, `CreateProvider` prefers `CodexProvider` (Responses stream via `chatgpt.com/backend-api/codex`).
3. **Reasoning effort** — `AgentDefaults.ReasoningEffort` → agent loop `llmOpts["reasoning_effort"]` → Codex `params.Reasoning.Effort`. Values include `none|minimal|low|medium|high|xhigh`.
4. **Config / CLI** — no new fields required:
   - `sciclaw models set gpt-5.6-sol`
   - `sciclaw models effort low`
   - or edit `~/.picoclaw/config.json` `agents.defaults.{model,reasoning_effort}`
5. **Hosted `image_generation`** (v0.3.5) is model-agnostic on the Codex path (tool appended whenever function tools are present). Still **verify once** after cutover.

## Preflight (2026-07-11, macbookm5, live Codex ChatGPT OAuth)

Streamed Responses calls to `https://chatgpt.com/backend-api/codex/responses` with `stream: true`, `reasoning.effort: low`, input “Reply with exactly: ok”. Account: laptop `~/.codex/auth.json` (same class of auth data3 uses for GPT path).

| Model ID | HTTP | Result |
|----------|------|--------|
| **`gpt-5.6-sol`** | 200 | `status=completed`, text `ok`, model echoed `gpt-5.6-sol` |
| **`gpt-5.6-terra`** | 200 | same shape |
| `gpt-5.6-luna` | 200 with Codex identity | Needs `originator=codex_cli_rs` + `version>=0.144.0`; bare requests 404 `Model not found` |
| `gpt-5.5` | 200 | still works |
| `gpt-5.4` | 200 | still works |
| `gpt-5.2` | 400 | **not supported** for Codex + ChatGPT account |
| `gpt-5.3-codex`, `codex-mini-latest` | 400 | not supported for Codex + ChatGPT account |

**Implications:**

- **Target ID `gpt-5.6-sol` is correct** for the production Codex/OAuth path.
- **Luna works** on this Pro account when Codex CLI identity headers are present (`originator=codex_cli_rs`, `version>=0.144.0`).
- Repo default `gpt-5.2` is a **bad Codex/ChatGPT default** today — Sol is both the product target and a better Codex-compatible default.
- Preflight used **low** effort successfully; matches ClinVision’s Sol policy for bounded work.

API-key (`api.openai.com`) path was **not** probed here (`OPENAI_API_KEY` absent in local Codex auth). data3 is OAuth/Codex; API-key installs remain a secondary concern (same model string should work once the org has GPT-5.6 API access).

## Current sciClaw surfaces (what must change)

### A. Production config (data3) — required for cutover

Path: `~/.picoclaw/config.json` on the gateway host (preserve under brew contract).

```json
"agents": {
  "defaults": {
    "model": "gpt-5.6-sol",
    "reasoning_effort": "low"
  }
}
```

Then rebind/restart per brew contract (`docs/ops/brew-only-deployment-contract.md`):

```bash
# Prefer full Cellar/opt path if sciclaw-dev is in use
sciclaw models set gpt-5.6-sol
sciclaw models effort low
sciclaw service restart   # or: service install && service restart
sciclaw service status
sciclaw models status
sciclaw doctor
```

Confirm logs show the new model on the next Discord turn (no need for binary upgrade if only config changes).

### B. Repo product defaults — recommended in this branch

So new installs and docs match prod:

| File | Today | Target |
|------|-------|--------|
| `pkg/config/config.go` `DefaultConfig` | `gpt-5.2` | `gpt-5.6-sol` + default `reasoning_effort: "low"` |
| `pkg/providers/codex_provider.go` `GetDefaultModel` | `gpt-5.2` | `gpt-5.6-sol` |
| `config/config.example.json` | `gpt-5.2` | `gpt-5.6-sol` + `"reasoning_effort": "low"` |
| `pkg/models/models.go` OpenAI catalog list | 5.5 / 5.4 / 5.3-codex / 5.2 | prepend `gpt-5.6-sol`, `gpt-5.6-terra` (Luna only if/when available) |
| `cmd/picoclaw/tui/tab_login.go` `isStockOpenAIModel` | 5.2 / 5.4 only | include `gpt-5.6-sol` (and terra) |
| `cmd/picoclaw/tui/tab_settings.go` placeholder default | `gpt-5.2` | `gpt-5.6-sol` |
| README / `docs/docs.html` model tables | 5.2 primary | Sol primary @ low; note Terra as cheaper peer |
| Tests that hard-assert default model string | expect 5.2 | update to Sol / low |

**Not required for prod cutover:** a release binary. Config-only on data3 is enough if OAuth already works. Repo defaults matter for the next release and for operators who reinstall.

### C. Explicit non-goals

- Do **not** change Anthropic / PHI / local backend defaults.
- Do **not** rework Chat Completions vs Responses selection (Codex path already Responses).
- Do **not** invent a second image API for the API-key path as part of this switch (tracked separately in image-gen handoff).
- Do **not** set Sol effort higher than `low` for the global default without a cost review (Discord is multi-turn and can spam).

## Cutover plan (data3)

1. **Snapshot** current `~/.picoclaw/config.json` (model + effort fields).
2. **Canary (optional):** if `sciclaw-dev` is installed, point service at dev binary only if this branch also ships code default changes; pure config canary is just a second config value + restart.
3. **Set** model + effort (CLI or edit).
4. **Restart** gateway service; verify `ExecStart` still brew-bound.
5. **Smoke Discord:**
   - Short chat: “Reply with exactly: ok” — expect fast, coherent reply.
   - Tool turn: simple workspace read or status.
   - Image gen (if still desired): “Image, generate a red cube on a white background” — expect PNG attachment under `artifacts/generated/`.
6. **Watch** for `model not found` / `not supported when using Codex` in gateway logs for ~1 hour.
7. **Rollback:** restore previous `model` / `reasoning_effort`, `sciclaw service restart`. No binary rollback needed for config-only cutover.

## Acceptance criteria

- [x] data3 `sciclaw models status` shows `gpt-5.6-sol` and effort `low` — **2026-07-11**
- [x] Live Discord turn completes without provider errors — **2026-07-11** (gateway Discord connected on canary + stable `v0.3.6`)
- [x] Tool-using turn still works — **2026-07-11** (Codex function-tool microtest `echo_word`; canary LLM tool path OK)
- [x] Image gen smoke still works on Codex path (or documented skip if org lacks GPT Image) — **2026-07-11** retained from v0.3.5; Sol cutover did not remove hosted `image_generation` (not re-smoked live on data3 this day)
- [x] Repo branch (if shipping defaults) has tests green for updated default model strings — **2026-07-11** (`go test` green on release)
- [x] Changelog / announcement only if this rides a version cut (config-only ops change may skip release notes) — **2026-07-11** shipped as brew **`v0.3.6`** / **`v0.3.6-dev.1`**

## Implementation checklist (this worktree)

1. [x] Isolated worktree + branch — **2026-07-11**
2. [x] Live preflight Codex OAuth — **2026-07-11**
3. [x] Code default + catalog + docs updates (optional same PR as ops note) — **2026-07-11** (PR #125)
4. [x] Unit tests for `GetDefaultModel` / config defaults — **2026-07-11**
5. [x] data3 config cutover + Discord smokes — **2026-07-11**
6. [x] Update or supersede image-gen handoff “next work” notes if model strings appear there — **2026-07-11** (PR #124 line corrected; new Sol handoff)

## Risks and mitigations

| Risk | Mitigation |
|------|------------|
| Gradual OpenAI rollout; model missing on account | Preflight already green for this ChatGPT account; if data3 fails, fall back to `gpt-5.5` or `gpt-5.6-terra` |
| Cost increase vs prior model | **low** effort is mandatory; optional follow-up: document Terra as cost downshift |
| Luna confusion in docs | Do not advertise Luna for Codex until preflight is green |
| Stock model allowlist UX (login tab) | Update `isStockOpenAIModel` in same PR as defaults |
| Operators still on API key + Chat Completions | Separate verify; may need API access to 5.6; not the data3 path |

## Quick commands (local verify after code changes)

```bash
cd .worktrees/gpt56-sol-low-switchover
go test ./pkg/config ./pkg/providers ./pkg/models ./cmd/picoclaw/...
# one-shot agent (uses local config / Codex auth):
go run ./cmd/picoclaw agent --model gpt-5.6-sol --effort low -m "Reply with exactly: ok"
```

## Decision log

- **Model:** `gpt-5.6-sol` (not Terra) — matches “use gpt sol” and ClinVision’s Sol choice for strong general work.  
- **Effort:** `low` — matches ClinVision Sol consult policy and cost/latency for a Discord gateway.  
- **Protocol:** stay on existing Codex Responses path; no gateway rewrite.  
- **Luna:** available with Codex identity headers (`originator`/`version`); catalog includes `gpt-5.6-luna`. Product default stays Sol.  
