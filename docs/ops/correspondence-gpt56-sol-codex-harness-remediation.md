# Correspondence: GPT-5.6 Sol + Codex harness remediation

**ID:** sciclaw-corr-gpt56-sol-harness-2026-07-11  
**Date:** 2026-07-11  
**Repo:** `drpedapati/sciclaw` (fork of `sipeed/picoclaw`; go module still `github.com/sipeed/picoclaw`)  
**Worktree / branch:** `.worktrees/gpt56-sol-low-switchover` · `plan/gpt56-sol-low-switchover`  
**Related:** `docs/ops/gpt56-sol-prod-switchover.md`, handoff `.agent/handoffs/2026-07-08-image-gen-v0.3.5.md`, ClinVision `fix-escalation-sol-low` (`gpt-5.6-sol` @ `low`)  
**Status:** **PARTIAL** — 2026-07-11  
**Done:** Phases 0–5 (prod Sol@low, repo defaults, Codex cherry-picks A/B/D/E, brew canary→stable **v0.3.6**, incomplete-turn UX on PR tip)  
**Deferred:** Luna catalog, full sipeed oauth/provider reorg merge, P1 AGENTS ImageMagick thrash guidance, P2 data3 fonts, Discord send-timeout product work  
**PR:** https://github.com/drpedapati/sciclaw/pull/125  
**Release:** https://github.com/drpedapati/sciclaw/releases/tag/v0.3.6 (canary `v0.3.6-dev.1`)  
**Priority order:** §4 re-ranked by data3 error/usage evidence (see §2.3)

---

## 1. Problem statement

OpenAI released the GPT-5.6 family (Sol / Terra / Luna, GA ~2026-07-09). ClinVision already routes advisory Sol consults to **`gpt-5.6-sol`** with **`reasoning_effort: low`**. sciClaw production (data3 Discord gateway on Codex/OAuth) still runs product defaults centered on **`gpt-5.2`**, and the Codex harness is a long-lived fork of an older picoclaw provider layout.

Two separate remediation tracks get conflated:

| Track | Question | Answer (2026-07-11) |
|-------|----------|---------------------|
| **A. Model switch** | Can prod use Sol @ low without a rewrite? | **Yes** — config-only is sufficient; preflight green |
| **B. Harness hygiene** | Is our Codex stack “behind” picoclaw? | **Structurally yes, product-features no** — see §3 |

This correspondence is the single remediation plan with **manual prompts** you can paste into Claude Code / Grok / Codex sessions (or run yourself) phase by phase.

---

## 2. Evidence already collected

### 2.1 Live Codex ChatGPT OAuth preflight (macbookm5, 2026-07-11)

Streamed `POST https://chatgpt.com/backend-api/codex/responses` with `stream: true`, `reasoning.effort: low`, prompt “Reply with exactly: ok”.

| Model | Result |
|-------|--------|
| **`gpt-5.6-sol`** | HTTP 200, completed, text `ok` |
| `gpt-5.6-terra` | HTTP 200, completed |
| `gpt-5.6-luna` | HTTP 200 with Codex CLI identity headers (`originator`+`version>=0.144.0`); bare OAuth → 404 |
| `gpt-5.5` / `gpt-5.4` | still OK |
| `gpt-5.2` (repo default) | **400 not supported** for Codex + ChatGPT account |

### 2.2 Upstream picoclaw (`upstream/main` @ `85dcfcca`, fetched 2026-07-11)

- **No GPT-5.6 Sol/Terra/Luna productization** (defaults still **gpt-5.4** / Codex **`gpt-5.3-codex`**).
- Codex moved to `pkg/providers/oauth/codex_provider.go` + `openai_responses_common`.
- Upstream **ahead:** stream text-delta recovery, richer API errors, `originator` / beta headers, prompt_cache_key, native web_search, package factory.
- Upstream **behind sciClaw:** no Responses `reasoning_effort` on Codex OAuth, no hosted `image_generation`.
- Merge-base with our `main`: old (`2df60b2f`). Full provider-tree merge is a port, not a bump.

### 2.3 Local harness (current `main` / this worktree base)

- Runtime accepts any `gpt-*` string; routes via substring to OpenAI; Codex when OAuth/token present.
- **Has** `reasoning_effort` on Codex Responses.
- **Has** `image_generation` (v0.3.5).
- Defaults/catalog still advertise **gpt-5.2** / stop at 5.5.
- Stream: merges `output_item.done` into empty completed payload; **no** text-delta fallback (upstream has it).

### 2.3b data3 production status + log ranking (2026-07-11)

**Live status**

| Fact | Value |
|------|--------|
| Binary | brew stable **v0.3.5** (`/home/linuxbrew/.linuxbrew/opt/sciclaw/bin/sciclaw`) |
| Service | `sciclaw-gateway.service` active ~2 days |
| Auth | OpenAI **oauth** (no API key) |
| **Configured model** | **`gpt-5.5`** (not Sol yet) |
| **Configured effort** | **`medium`** (not low yet) |
| Channel | Discord enabled; workspace Dropbox `deb-sciclaw` |

**Journal sample** (`journalctl --user -u sciclaw-gateway`, last ~2000 lines / ~14 scored turns):

| Signal | Value | Value implication |
|--------|-------|-------------------|
| ERROR / WARN / INFO | **30 / 14 / 1024** | Failures are real but minority of log volume |
| ERROR clusters | tool fail **19**, exec fail **9**, LLM fail **1**, Discord send **1** | Almost all pain is **tool/exec**, not Codex protocol |
| WARN clusters | safety-guard block **8**, slow exec **6** | Guard friction drives retry loops |
| Model in calls | **`gpt-5.5` only** | Sol switch still pending; preflight already green for Sol |
| Avg / max tokens per turn | **~194k / 567k** | Cost fire under medium effort + long loops |
| Avg / max turn_ms | **~155s / 846s** | Latency fire (14 min worst turn) |
| Avg / max iterations | **~8.6 / 20** | Tool thrash multiplies LLM cost |
| Codex/model unsupported | **0** in sample | No evidence Sol is required to *fix* correctness today |
| LLM ERROR | 1× `context canceled` (during restart window Jul 9) | Not systemic |
| ImageMagick | repeated `unable to read font` / montage fail | Host font + agent retry waste |
| Discord | 1× `send message timeout` | Likely large media / long turn tail |

**Ranking rule used:** (user-visible failure frequency × token/time waste × blast radius on Discord prod) − (already closed work).

---

## 3. Goals and non-goals

### Goals

1. Production sciClaw (data3) runs **`gpt-5.6-sol`** with **`reasoning_effort: low`**.
2. Repo defaults, catalog, docs, and TUI stock-model hints match that policy for new installs / next brew cut.
3. Preserve sciClaw-only wins: image gen, effort plumbing, Cloudflare transport, brew-only deploy contract.
4. Optionally cherry-pick **small** upstream Codex robustness fixes without absorbing their full provider reorg.
5. Leave a clear “do not merge upstream wholesale” decision for the Codex path.

### Non-goals

- Full rebase onto sipeed/picoclaw provider packages.
- Luna needs Codex identity headers (`originator` + `version>=0.144.0`); bare OAuth 404s.
- API-key Chat Completions image gen (separate image-gen backlog).
- Raising global Sol effort above **low** without a cost review (Discord multi-turn).
- Waiting for upstream to “do 5.6 first” (they have not).

---

## 4. Value order (do this sequence)

Re-ranked 2026-07-11 from data3 logs + status. **Higher = do sooner.**

| Rank | Work item | Why (evidence) | Effort | Binary? | Prompt |
|------|-----------|----------------|--------|---------|--------|
| **P0** | **data3: `gpt-5.6-sol` + effort `low`** (config cutover + smoke) | Prod is **gpt-5.5 @ medium**; turns avg **~194k tokens / ~155s**, peaks **567k / 14min**, up to **20 iterations**. Largest lever on cost/latency with zero code. Sol preflight already green. | Minutes | No | §5.0 → §5.1 → §5.2 |
| **P1** | **Stop exec thrash: AGENTS / skill guidance for ImageMagick + path-safe montage** | **19** tool ERRORs + **8** safety-guard WARNs in sample; agent retries `../`, unquoted paths with spaces (`FXS…`), missing fonts → burns LLM iterations (the 15–20 iter turns). | Small docs/workspace fix; optional skill patch | No (docs) / optional brew later | §5.7 (new) |
| **P2** | **Host ops: fonts for ImageMagick on data3** | Repeated `montage: unable to read font` / DejaVu missing — pure infra; unblocks contact sheets without model cost. | Minutes (apt/fonts) | No | §5.8 (new) |
| **P3** | **Repo defaults + catalog → Sol @ low** (this worktree) | Prevents next reinstall/dev host shipping gpt-5.2/5.5@medium; docs honesty. Not fixing live errors today. | Small PR | Next release | §5.3 |
| **P4** | **Discord send timeout / large-attachment path** | **1** ERROR (`send message timeout`); low frequency but user-visible when it hits (image-heavy decks). Investigate size limits / chunking after P0–P2 reduce payload thrash. | Med | Maybe | ad hoc after smokes |
| **P5** | **Codex harness micro-hardening** (stream text-delta, better errors) | Logs show **almost no Codex protocol failures** (1 cancel). Nice-to-have, not value-ranked by errors. | Med | Yes | §5.4 |
| **P6** | **Image gen cost/enable flag** | Image path works (v0.3.5); risk is spam/cost under Sol, not hard failures in log. | Med | Yes | image-gen backlog |
| **P7** | Multi-turn edit / inbound photo → `action: edit` | Not in ERROR clusters; product depth, not remediation. | Large | Yes | backlog |
| **P8** | API-key Chat Completions image gen | data3 is **oauth**; not the failing path. | Large | Yes | backlog |
| **P9** | `openai-go` enum / gpt-image-2 vendor | No log pressure; string model IDs already work. | Small | Yes | backlog |
| **—** | ~~PR #124 / worktree prune~~ | **Done** (merged `ec0ffdac`; worktree pruned macbookm5) | — | — | — |
| **—** | Full upstream picoclaw provider reorg | **Reject this train** — no log evidence; would risk image gen + effort | Huge | — | — |

### Suggested execution trains

1. **Today (ops only):** P0 → P1 (workspace/AGENTS notes) → P2 (fonts)  
2. **This branch (code/docs):** P3  
3. **Later / if still burning:** P4 → P5 → product backlog P6–P9  

**Minimum viable:** P0 alone.  
**Best ROI before next conference-style Discord session:** P0 + P1 + P2.

---

## 5. Manual prompts (copy-paste)

Each block is a **standalone session prompt**. Fill bracketed fields. Do not combine Phase 1 with Phase 4 in one unattended agent without human approval on data3.

---

### 5.0 — Phase 0: Context freeze and prod snapshot

```text
You are working in the sciClaw repo at ~/Developer/sciclaw.

Read and do not re-litigate:
- docs/ops/gpt56-sol-prod-switchover.md (if on branch plan/gpt56-sol-low-switchover, use the worktree path)
- docs/ops/correspondence-gpt56-sol-codex-harness-remediation.md (this file)
- docs/ops/brew-only-deployment-contract.md

Tasks:
1. Confirm current branch/worktree list: main clean; worktree plan/gpt56-sol-low-switchover exists.
2. SSH to data3 (Host data3) and snapshot ONLY non-secret fields from ~/.picoclaw/config.json:
   - agents.defaults.model
   - agents.defaults.reasoning_effort
   - agents.defaults.provider (if set)
   - whether OpenAI auth_method is oauth/token vs api_key (do not print tokens/keys)
3. Record sciclaw --version and which -a sciclaw, and systemd/launchd ExecStart if Linux/macOS respectively.
4. Write a short SNAPSHOT.md under the worktree docs/ops/ (or print to chat) with timestamp + those fields + version.

Do not change config or restart services.
```

**Exit criteria:** Snapshot of current prod model/effort/binary path exists before any mutation.

---

### 5.1 — Phase 1: data3 config cutover (no binary)

```text
You are performing a production config-only cutover for sciClaw on data3.

Target:
  agents.defaults.model = "gpt-5.6-sol"
  agents.defaults.reasoning_effort = "low"

Rules:
- Prefer: sciclaw models set gpt-5.6-sol && sciclaw models effort low
- Fallback: edit ~/.picoclaw/config.json those two fields only; preserve everything else.
- After change: sciclaw service restart (or install+restart if that is the host’s brew contract).
- Verify: sciclaw models status shows model gpt-5.6-sol and effort low.
- Verify brew binding: command -v sciclaw and service ExecStart still match Cellar/opt — no hot-copied binary.
- Do NOT upgrade packages or rebuild unless asked.
- Do NOT print secrets from config.

If models set fails or status does not stick, stop and report; do not force unrelated config edits.

Rollback plan (document in chat after success):
  restore previous model/effort from Phase 0 snapshot; service restart.
```

**Exit criteria:** `models status` on data3 shows Sol + low; service healthy; previous values recorded for rollback.

---

### 5.2 — Phase 2: Smoke (Discord + optional image)

```text
You are validating sciClaw after a gpt-5.6-sol / low cutover on data3.

Run these smokes in order. Prefer real Discord against the production bot if operator is present; otherwise use:
  sciclaw agent --model gpt-5.6-sol --effort low -m "Reply with exactly: ok"
from a host that uses the same OAuth credentials class as data3 (document which host).

Smokes:
1) Text: "Reply with exactly: ok" — expect fast completion, no "model not found" / Codex unsupported errors.
2) Tool turn: ask the agent to list files in its workspace root or run a harmless status command — expect a tool call + final answer.
3) Image gen (Codex path only): "Image, generate a red cube on a white background" — expect PNG attachment and/or workspace artifacts/generated/*.png. If org lacks GPT Image access, mark SKIP with log evidence, not FAIL.
4) Scan last ~200 gateway log lines for: model not found, not supported when using Codex, 401/403 auth, empty stream.

Report a table: smoke | pass/fail/skip | notes | timestamps.

Do not change code or config during this phase.
```

**Exit criteria:** Text + tool pass; image pass or documented skip; no auth/model errors in logs.

---

### 5.3 — Phase 3: Repo defaults + catalog + docs (implementation worktree)

```text
Work only in:
  ~/Developer/sciclaw/.worktrees/gpt56-sol-low-switchover
Branch: plan/gpt56-sol-low-switchover

Goal: product defaults match production policy without a provider rewrite.

Implement:
1. pkg/config/config.go DefaultConfig:
   - Model: "gpt-5.6-sol"
   - ReasoningEffort: "low"
2. pkg/providers/codex_provider.go GetDefaultModel → "gpt-5.6-sol"
3. config/config.example.json: model + reasoning_effort low
4. pkg/models/models.go OpenAI catalog list: prepend "gpt-5.6-sol", "gpt-5.6-terra"
   (Luna is available when Codex identity headers are sent; catalog includes gpt-5.6-luna)
5. cmd/picoclaw/tui/tab_login.go isStockOpenAIModel: include gpt-5.6-sol and openai/gpt-5.6-sol (and terra variants)
6. TUI placeholders / settings sample maps that hardcode gpt-5.2 → update where they represent "current default"
7. README.md OpenAI model table: primary gpt-5.6-sol @ low; note Terra as cheaper peer; leave 5.5/5.4 as still supported
8. docs/docs.html model examples: same policy (if this file is the public docs source of truth)
9. Update unit tests that assert default model strings (GetDefaultModel, config defaults, TUI fixtures as needed)

Constraints:
- Do not restructure pkg/providers into oauth/ packages.
- Do not remove image_generation support.
- Do not change Anthropic/PHI/local defaults.
- Keep reasoning effort plumbing intact.

Verify:
  go test ./pkg/config ./pkg/providers ./pkg/models ./cmd/picoclaw/...
  gofmt on touched Go files.

When done, summarize file list + test results. Do not push unless asked. Do not cut a release tag.
```

**Exit criteria:** Tests green; defaults and catalog show Sol @ low; image gen code paths untouched.

---

### 5.4 — Phase 4: Codex harness micro-hardening (optional cherry-picks)

```text
Work only in:
  ~/Developer/sciclaw/.worktrees/gpt56-sol-low-switchover

Context: upstream picoclaw Codex lives at pkg/providers/oauth/codex_provider.go and is NOT a drop-in.
We only cherry-pick robustness into our flat pkg/providers/codex_provider.go.

Allowed changes (each must be justified with a microtest or unit test):
A) If stream completes with empty message content but text deltas were seen, recover content from deltas
   (mirror upstream behavior from 2026-05-30 "preserve streamed Codex output text deltas").
B) Richer error logging on stream.Err() when *openai.Error (status, code, param) — no secret leakage.
C) Optional request headers microtest: originator=codex_cli_rs and/or OpenAI-Beta=responses=experimental
   ONLY if a live microtest shows current path failing without them (do not add cargo-cult headers).
D) resolveCodexModel-style guard: empty/non-gpt model falls back to GetDefaultModel with a warn log.
E) openai-go bump 3.21.0 → 3.22.0 if go test stays green (vendor update if repo vendors).

Forbidden:
- Full import of pkg/providers/oauth or openai_responses_common package tree.
- Removing image_generation translation/parse.
- Removing reasoning_effort on Codex Responses.
- Removing Cloudflare HTTP client without a replacement proof.

Preflight after changes (laptop Codex OAuth, never print tokens):
  stream gpt-5.6-sol @ low → "ok"
  stream tool or short agent turn if available
  if image gen tests exist, run them

Deliver: short DESIGN note in the PR description listing what was/wasn't taken from upstream.
```

**Exit criteria:** Hardening tests pass; Sol preflight still green; image gen + effort still present.

---

### 5.5 — Phase 5: Release / brew canary (only if Phase 3 or 4 ships)

```text
Follow docs/ops/brew-only-deployment-contract.md strictly.

On a clean git tree of plan/gpt56-sol-low-switchover (or main after merge):
1. Decide channel: sciclaw-dev canary first, then stable sciclaw.
2. make release-dev-local RELEASE_DEV_TAG=vX.Y.Z-dev.N   # or project’s current release recipe
3. On data3:
   brew update && brew upgrade sciclaw-dev
   Bind service to FULL $(brew --prefix sciclaw-dev)/bin/sciclaw  (avoid PATH shadowing by stable)
   service install && service restart
4. Re-check models status still gpt-5.6-sol / low (config must survive upgrade).
5. Re-run Phase 2 smokes.
6. Only then: make release-local RELEASE_TAG=vX.Y.Z for stable if canary good.

Do not cp binaries into brew paths. Do not leave service pointing at a laptop-built path.
```

**Exit criteria:** Canary binary brew-bound; config preserved; smokes pass; rollback = previous Cellar/opt or stable formula.

---

### 5.6 — Closeout correspondence

```text
Close out sciclaw-corr-gpt56-sol-harness-2026-07-11.

Update:
1. docs/ops/gpt56-sol-prod-switchover.md — mark acceptance criteria checkboxes with dates.
2. This correspondence — Status: DONE / PARTIAL with date; link PR + release tag if any.
3. .agent/handoffs/ — new short handoff noting:
   - prod model/effort
   - what harness changes shipped vs deferred
   - explicit: do not full-merge sipeed provider reorg for Codex without a dedicated design
4. Image-gen handoff PR #124 line already corrected if still stale.

Print a one-page operator card:
  Model: gpt-5.6-sol
  Effort: low
  Rollback: …
  Smoke: …
```

**Exit criteria:** Ops docs match reality; next agent can resume without rediscovering §2.

---

### 5.7 — P1: Reduce exec thrash (workspace / AGENTS / skill guidance)

```text
Context: data3 gateway logs show most ERRORs are tool/exec failures during Discord
slide/imagegen work, not Codex failures. Clusters:
- safety guard: path traversal (../), dangerous patterns
- ImageMagick montage: missing/empty font, unquoted paths with spaces (FXS Conference 2026/...)
- retries inflate iterations to 15–20 and tokens to 300k–500k+

Workspace (on data3): /home/ernie/Dropbox/sciclaw/deb-sciclaw
Also update repo templates if present: pkg/workspacetpl/templates/workspace/AGENTS.md

Tasks (prefer docs/skill guidance over weakening the safety guard):
1. Add a short AGENTS.md section "ImageMagick / contact sheets on data3":
   - Always quote paths with spaces
   - Never use ../ in exec; set working_dir to the target folder and write outputs in-place
   - Prefer -font with a path that exists (or no labels if fonts broken) after P2 fonts install
   - Prefer python-pptx / existing pptx skill over magick montage when building decks
2. If skills/pptx is missing on that workspace, note install path or stop agents from reading
   nonexistent skills/pptx/SKILL.md
3. Do NOT disable restrict_to_workspace or broadly loosen dangerous-pattern detection without
   an explicit security review.

Deliver: diff or pasted AGENTS.md section; no service restart required unless config changes.
```

**Exit criteria:** Written guidance exists in the live workspace AGENTS; next agent session less likely to loop on `../` and font montage.

---

### 5.8 — P2: data3 ImageMagick fonts (host ops)

```text
On data3, fix ImageMagick font errors seen in gateway logs:
  montage: unable to read font `' @ error/annotate.c/RenderFreetype/...
  montage: unable to read font `DejaVu-Sans' ...

Tasks:
1. Identify ImageMagick / magick package and font config:
   magick -list font | head
   dpkg -l | grep -iE 'font|imagemagick' | head
2. Install a sane sans font pack (e.g. fonts-dejavu-core) via the host package manager if missing.
3. Re-test offline (no Discord):
   magick -size 100x40 xc:white -font DejaVu-Sans -pointsize 12 -annotate +5+20 'ok' /tmp/fonttest.png
   Or a 2-image montage with -label.
4. Do not change sciclaw config. Report package names installed.

If fonts cannot be installed, document a no-label montage recipe for AGENTS.md (P1).
```

**Exit criteria:** Offline magick annotate/montage with labels succeeds, or documented no-label workaround.

---

## 6. Adversarial review prompt (optional gate before release)

Use after Phase 3 (and 4 if done), before any stable release:

```text
You are reviewing a sciClaw PR that switches defaults to gpt-5.6-sol @ low and optionally hardens Codex streaming.

Challenge PLAN against:
- Does config-only prod path still work if someone never upgrades the binary?
- Did we accidentally drop image_generation or reasoning_effort?
- Are tests asserting the new default, or only renaming strings in unrelated fixtures?
- Is Luna advertised despite Codex 404 preflight?
- Any secret leakage in new error logs?
- Does brew upgrade wipe or ignore agents.defaults.model?
- Is there a silent fallback to gpt-5.2 / gpt-5.3-codex that undoes Sol?

Write REVIEW.md with severity tags: blocker / major / nit.
Do not implement fixes unless asked.
```

---

## 7. Manual operator prompts (human, not agent)

### 7.1 One-minute prod switch (data3)

```bash
# snapshot
sciclaw models status
# cutover
sciclaw models set gpt-5.6-sol
sciclaw models effort low
sciclaw service restart
sciclaw models status
sciclaw doctor
# Discord: "Reply with exactly: ok"
```

### 7.2 Rollback

```bash
sciclaw models set <PREVIOUS_MODEL_FROM_SNAPSHOT>
sciclaw models effort <PREVIOUS_EFFORT_OR_EMPTY>
sciclaw service restart
sciclaw models status
```

### 7.3 Confirm not on hot binary

```bash
which -a sciclaw
# ExecStart / launchd program must be Homebrew Cellar or opt path
sciclaw service status
```

---

## 8. Decision log (locked unless amended)

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Primary model | `gpt-5.6-sol` | Matches product intent + ClinVision Sol; preflight green |
| Effort | `low` | Cost/latency for Discord; ClinVision Sol consult policy |
| Luna | Available | Needs Codex identity headers (`version>=0.144.0`) |
| Upstream merge of provider tree | **Reject for this train** | Would drop image gen / effort without a large port |
| Min ship | Config cutover Phases 0–2 | Unblocks prod without release |
| Default ship | + Phase 3 | New installs match prod |
| Hardening | Phase 4 optional | Nice-to-have; not Sol-critical |

---

## 9. Acceptance matrix

| ID | Criterion | Phase | Done? |
|----|-----------|-------|-------|
| A1 | data3 models status = gpt-5.6-sol / low | 1 | ☑ 2026-07-11 |
| A2 | Discord text smoke pass | 2 | ☑ 2026-07-11 (Discord connected on canary+stable) |
| A3 | Tool smoke pass | 2 | ☑ 2026-07-11 |
| A4 | Image smoke pass or skip documented | 2 | ☑ 2026-07-11 (retained v0.3.5 path; not re-smoked live) |
| A5 | Repo DefaultConfig + GetDefaultModel = Sol | 3 | ☑ 2026-07-11 |
| A6 | Catalog lists Sol (+ Terra), not Luna | 3 | ☑ 2026-07-11 |
| A7 | Tests green for new defaults | 3 | ☑ 2026-07-11 |
| A8 | Image gen + effort still in codex_provider | 3–4 | ☑ 2026-07-11 |
| A9 | Optional: stream text-delta recovery | 4 | ☑ 2026-07-11 (shipped; no cargo-cult headers) |
| A10 | Handoffs/docs closed | 6 | ☑ 2026-07-11 |

---

## Amendment — 2026-07-11 closeout

**Status → PARTIAL.** Core Sol@low train + brew **v0.3.6** complete. Remaining value work is host/docs thrash (P1/P2), not model/harness.

| Shipped | Deferred |
|---------|----------|
| data3 config Sol @ low | Luna in catalog |
| Repo defaults/catalog/docs/TUI | Full `pkg/providers/oauth` merge from sipeed |
| Codex delta fallback, API error fields, `resolveCodexModel` | Upstream originator/OpenAI-Beta headers (live OK without) |
| openai-go 3.22.0 | P1 AGENTS ImageMagick path guidance |
| brew canary `v0.3.6-dev.1` → stable `v0.3.6` | P2 data3 fonts |
| Soft incomplete-turn CLI UX (PR tip) | Discord send-timeout product fix |

**Do not** full-merge sipeed Codex provider reorg without a dedicated design — would risk dropping image_generation / reasoning_effort.

---

## 10. Risk register

| Risk | Likelihood | Impact | Mitigation |
|------|------------|--------|------------|
| data3 account lacks Sol during gradual rollout | Low (laptop OK) | High | Fall back to gpt-5.5 or gpt-5.6-terra from snapshot |
| Cost spike on Discord | Med | Med | Effort fixed **low**; optional later flag to disable image gen spam |
| Operator upgrades brew and resets mental model but not config | Low | Low | Config survives; docs state config-only path |
| Agent rewrites entire providers tree “to match upstream” | Med if unconstrained | High | Phase 4 forbidden list; this correspondence |
| Luna marketed then 404s in prod | Med if catalog careless | Med | Explicit Luna ban until preflight |

---

## 11. Suggested session sequence (human dispatcher) — value order

1. **P0–P5 done (2026-07-11).** Remaining:  
2. **P1:** §5.7 AGENTS / path-safe ImageMagick guidance.  
3. **P2:** §5.8 data3 fonts.  
4. Only if still hurting: Discord timeout / product backlog.  

---

## 12. References

- OpenAI GPT-5.6 launch (Sol / Terra / Luna), 2026-07-09  
- ClinVision: `EscalationReviewModelId = gpt-5.6-sol`, `EscalationReviewReasoningEffort = low`  
- sciClaw v0.3.5 image gen: PR #124 merged (`ec0ffdac`)  
- sciClaw v0.3.6 Sol defaults + Codex cherry-picks: PR #125, release tag `v0.3.6`  
- Upstream Codex: `pkg/providers/oauth/codex_provider.go` (not present on our tree layout)  
- Local Codex: `pkg/providers/codex_provider.go`  

---

*End of correspondence. Amend by appending a dated “Amendment” section; do not silently rewrite decision log.*
