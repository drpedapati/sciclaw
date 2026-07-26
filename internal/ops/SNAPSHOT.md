# Phase 0 snapshot — sciClaw prod (data3)

**Captured:** 2026-07-11T08:14:32Z  
**Captured from:** macbookm5 → SSH `Host data3`  
**Mutation:** none (read-only)

---

## Local repo (macbookm5)

| Item | Value |
|------|--------|
| Main checkout | `/Users/ernie/Developer/sciclaw` · branch **`main`** · clean · `ec0ffdac` · tracks `origin/main` |
| Worktree | `/Users/ernie/Developer/sciclaw/.worktrees/gpt56-sol-low-switchover` · **`plan/gpt56-sol-low-switchover`** · same tip `ec0ffdac` · untracked ops docs only |
| Other worktree | `teams-integration` @ `irl_projects/260212-sciclaw-teams` (unrelated) |

Docs present under worktree: `gpt56-sol-prod-switchover.md`, `correspondence-gpt56-sol-codex-harness-remediation.md`, `brew-only-deployment-contract.md`.

---

## data3 binary / service

| Item | Value |
|------|--------|
| Host | `data3` |
| `sciclaw --version` | **v0.3.5** · Build 2026-07-08T22:42:01-0400 · Go go1.26.2 |
| `which -a sciclaw` | `/home/linuxbrew/.linuxbrew/bin/sciclaw` |
| Resolved binary | `/home/linuxbrew/.linuxbrew/Cellar/sciclaw/0.3.5/bin/sciclaw` (symlink from brew bin) |
| Service unit | `~/.config/systemd/user/sciclaw-gateway.service` (systemd-user) |
| **ExecStart** | `/home/linuxbrew/.linuxbrew/opt/sciclaw/bin/sciclaw gateway` |
| ActiveState | **active** (running) |
| UnitFileState | **enabled** |
| MainPID | 1614404 (started Thu 2026-07-09 02:42:47 UTC) |
| Backend | systemd-user · Installed yes · Running yes · Enabled yes |

Brew binding looks clean (opt/sciclaw, not a laptop hot-copy).

---

## data3 config (non-secret only)

Source: `~/.picoclaw/config.json`

| Field | Value |
|-------|--------|
| `agents.defaults.model` | **`gpt-5.5`** |
| `agents.defaults.reasoning_effort` | **`medium`** |
| `agents.defaults.provider` | **`openai`** |
| `agents.defaults.mode` | unset / null |
| `agents.defaults.workspace` | `~/sciclaw` |
| OpenAI `auth_method` | **`oauth`** |
| OpenAI auth class | **oauth** (not api_key) |
| OpenAI has API key field set | **false** |
| Discord enabled | **true** |

`sciclaw models status` agrees: Model `gpt-5.5`, Provider `openai`, Auth `oauth`, Reasoning Effort `medium`.

---

## Rollback targets (for Phase 1)

If Sol cutover is attempted later, restore:

```bash
sciclaw models set gpt-5.5
sciclaw models effort medium
sciclaw service restart
```

---

## Target (Phase 1) — **APPLIED 2026-07-11T08:14:59Z**

| Field | Before (Phase 0) | After |
|-------|------------------|-------|
| model | `gpt-5.5` | **`gpt-5.6-sol`** |
| reasoning_effort | `medium` | **`low`** |

Method: `sciclaw models set gpt-5.6-sol` + `sciclaw models effort low` + `sciclaw service restart`  
Gateway: Discord reconnected as `sciclaw-app`; brew ExecStart still `opt/sciclaw` (v0.3.5). New MainPID 3546763.

### Rollback

```bash
export PATH="/home/linuxbrew/.linuxbrew/bin:$PATH"
sciclaw models set gpt-5.5
sciclaw models effort medium
sciclaw service restart
sciclaw models status
```

---

## Phase 2 smokes (2026-07-11, host **data3**, CLI agent + OAuth)

| Smoke | Result | Notes | Time (UTC) |
|-------|--------|-------|------------|
| 1 Text | **PASS** (model) | `gpt-5.6-sol` LLM ~1.7s, `response_chars=2` (`ok`). CLI exit≠0 due to delivery verification harness, not model/Codex. No model-not-found. | 08:16:11–08:16:14 |
| 2 Tool | **PASS** | `list_dir` on `.` + final listing; exit 0; ~5s | 08:16:19–08:16:24 |
| 3 Image | **PASS** | `media_count=1`; PNG `~/sciclaw/artifacts/generated/generated-20260711-081648-1.png` (1254×1254 RGB, ~1.0MB); ~19.5s | 08:16:29–08:16:48 |
| 4 Logs | **PASS** | Post-cutover journal: 0 ERROR/WARN; 0× model not found / Codex unsupported / empty stream / 401 / 403. Discord connected. | 08:16:53 |

Config still: `gpt-5.6-sol` / `low` / oauth after smokes. No config changes during Phase 2.

### Discord human smoke (gateway path)

| Smoke | Result | Notes | Time |
|-------|--------|-------|------|
| **1d Discord text** | **PASS** | User: `@sciclaw-app reply wih exactly: ok` → card `sciClaw · 000J7` Done in **~3s**, reply **`ok`**. Confirms post-cutover gateway + Discord delivery (not only CLI). | ~2026-07-11 04:18 local / ~08:18 UTC |

---

*Phase 0 complete (read-only). Phase 1 cutover applied 2026-07-11. Phase 2 smokes PASS (CLI + Discord).*
