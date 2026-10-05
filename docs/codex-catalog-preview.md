# Dynamic model catalog preview

The CLI, terminal app and browser GUI now get configured Codex account choices
from its authenticated model catalog. Reasoning selectors use advertised levels.
Fresh configuration defaults to `gpt-6.1-sol` with `medium` reasoning. Existing
saved choices remain unchanged by upgrades or discovery refreshes.

Catalog and inference share Codex client identity version `0.160.0`. Visible,
compatible models retain order and deduplicated IDs; legacy `models` JSON stays
an array of IDs, augmented by a `metadata` map. Mixed-provider entries preserve
their own provider/source. OAuth refresh uses a bounded request without printing
retry messages into discovery JSON. API-key mode never uses the ChatGPT catalog.
Failure produces a warning and built-in fallback; no persistent catalog cache.

`ultra` is excluded from selectable reasoning levels because its advertised
automatic delegation behavior is not implemented by sciClaw. Other advertised
levels are preserved. Backend validation protects manual effort selections too.

## Local preview on Data3

```sh
sciclaw-preview models discover --json
sciclaw-preview models status
sciclaw-preview app
```

Browser preview: http://127.0.0.1:4143 on the server. For a remote browser, forward
that port using SSH. The wrapper runs the candidate binary at
`~/.local/share/sciclaw-preview/bin/sciclaw` with isolated HOME/config/workspace
and systemd user-bus environment. Its channel connections and routing are
disabled. It shares the existing credential file via a symlink so a refresh does
not rotate a detached credential copy. It does not rebind or restart production
services or change the original config. Do not install a service from this
preview; use the repository's Homebrew release path for a production upgrade.

The preview config selects Sol/medium explicitly. The existing Homebrew command
continues to use its saved model/effort. Returning to normal `sciclaw` leaves the
preview environment. The preview web PID is recorded at
`~/.local/share/sciclaw-preview/web.pid`; its log is under `logs/web.log` there.

## Verification

- Catalog parsing/identity/version/visibility, metadata round trips, secondary
  provider discovery, API-key separation, failure fallback, deadline and response
  bounds, credential refresh persistence/sanitization, UI adapters and effort
  validation are covered by focused tests.
- Config/provider/migration and terminal suites pass. Provider round-trip test
  explicitly asserts `gpt-6.1-sol` and `reasoning.effort=medium` in the request.
- Frontend TypeScript/Vite build and Go vet pass; rebuilt bundle is embedded.
- Full suite with an isolated HOME passes except the pre-existing
  `TestCheckCtxclawBinarySkipsWhenMissing` host-dependent failure, reproduced on
  unchanged main during the preflight. No live host credentials used by that run.
- Live preview discovery returns source `endpoint`, advertises Sol and its
  low/medium/high/xhigh/max levels. A narrow real Sol request returned
  `SCICLAW_PREVIEW_OK`. Browser catalog/status/HTML return HTTP 200; its effort
  endpoint rejects none (400) and accepts medium (200).
- Preview service status is unsupported/not installed/not running. The original
  Homebrew command still reports `gpt-5.6-terra` / `low`.

Tests/proof are not an independent approval or production release acceptance.
Interactive terminal and browser user acceptance remain for the user to try.
Concurrent multi-process OAuth refresh retains the existing shared-store
concurrency limits; this change does not introduce a credential lock or atomic
store migration. A transient refresh failure is reported and never overwrites
the previous credential.

## Independent review request

Compare base `eb557d5` to the PR head. Inspect the full diff and rerun focused
tests, vet, frontend build and an isolated-HOME full suite. Verify account-specific
discovery, OAuth versus API-key routing, shared inference identity, deadline and
response bounds, secret-free errors, secondary-provider metadata, CLI JSON
compatibility, both reasoning selectors, model/effort persistence and command
quoting. Confirm unsupported effort cannot be saved through CLI or browser,
and no discovery refresh overwrites selected defaults. Review token refresh
concurrency implications and preview isolation. Do not merge, deploy, or treat
the author's tests as independent approval. Return severity-ranked findings,
file/line evidence, commands rerun, and unverified claims.

Merge train: preflight PR #135 → implementation review → user acceptance →
separately authorized Homebrew release/deployment. No main merge, release tag,
tap change, or production service upgrade is included.
