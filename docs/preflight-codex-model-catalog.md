# Codex model catalog preflight

Goal: let the CLI, terminal app, and browser GUI select account-advertised Codex
models and supported reasoning levels; default to `gpt-6.1-sol` / `medium`.

Target: drpedapati/sciclaw, main base `eb557d5`.
Proposed minimal patch: shared authenticated Codex discovery in `pkg/models`,
additive metadata in its JSON result, and metadata consumption in existing UIs.
This PR contains tests and analysis only; no production behavior or installed
configuration changes. Test-only experiments are not a ready-to-ship client.

Route: this session owns analysis and acceptance; no worker or independent agent
was used. The executing model identity is not independently verified here.
Data boundary: source plus synthetic test fixtures; the earlier read-only live
catalog check used the user's existing sciClaw credential only with the existing
OpenAI endpoint. No credential, account identifier, prompt, or raw response is
included in this PR.

## A. Hidden contracts

| Hidden contract | Evidence | Risk | Smallest mitigation |
| --- | --- | --- | --- |
| All selectors share CLI discovery | CODE-SUPPORTED: `main.go:modelsCmd`, `tui/tab_models.go:fetchModelsCatalog`, `web_cmd.go:handleModelsAction` | Separate UI fetchers diverge | Add discovery once in `pkg/models`; reuse current command and route |
| OpenAI currently has no live discovery | CODE-SUPPORTED: `models.go:Discover` calls only Anthropic; OpenAI IDs are built-ins | New IDs never appear automatically | OAuth-specific Codex catalog branch; preserve API-key provider behavior |
| Catalog access works for this account | PROVEN runtime observation, 2026-10-05: HTTP 200 from `/backend-api/codex/models?client_version=0.160.0`; `gpt-6.1-sol` visible, minimum `0.153.0`, medium supported | Catalog access does not prove inference works | Separate live inference acceptance after implementation |
| Inference advertises an obsolete version | CODE-SUPPORTED: `providers/codex_provider.go` uses `0.144.1` | Discovery works but inference remains gated | Share identity/version between discovery and inference, update together |
| Native metadata differs from selected defaults | PROVEN: catalog advertises Sol default low; required product default is medium | Refresh overwrites user choice | Discovery is read-only; persist user selection through existing config path |
| Saved model/provider/effort survive reload | PROVEN: `TestPreflightExplicitSolMediumSurvivesConfigReload` | Fresh defaults alone do not migrate existing configs | Define upgrade policy explicitly; preserve saved choices unless separately migrated |
| Browser adapter discards unknown metadata | PROVEN: `TestPreflightWebCatalogCurrentlyDropsReasoningMetadata` | Backend improvement never reaches GUI | Extend typed CLI payload and browser adapter; invert characterization assertion |
| Reasoning lists are duplicated | CODE-SUPPORTED: terminal Models tab, terminal Settings tab, browser Models and Settings pages | Invalid none/minimal offered; valid max omitted | Shared per-model capabilities in each UI; backend validation also required |
| Secondary providers append built-ins | CODE-SUPPORTED: `discoverSecondaryProviders` and `Discover` | Secondary Codex catalog stale when Claude is selected | Discover each configured provider with bounded requests; retain true provider/source per entry |
| Browser assigns primary provider to every entry | CODE-SUPPORTED: `web_cmd.go` adapter sets `Provider: payload.Provider` for all IDs | Mixed-provider entries mislabeled | Preserve per-entry provider metadata rather than infer from catalog owner |
| Tokens can expire | CODE-SUPPORTED: `auth.GetCredential` does not refresh; inference has tokenSource | Catalog 401 after login ages | Reuse existing refreshable OAuth credential path; no second credential store |
| Selection is config, running gateway is separate | CODE-SUPPORTED: browser model flow restarts gateway; terminal saves through CLI | Saved model differs from active gateway | Preserve existing restart behavior and verify actual post-restart model/effort |
| UI commands have finite budgets | CODE-SUPPORTED: 20-second discovery command timeout | Unbounded retries stall selectors | Catalog context deadline shorter than command timeout; bounded response size |
| Failure bodies may contain sensitive content | PROVEN in synthetic tests: rejected status, malformed and oversized bodies yield generic diagnostics | Tokens or raw bodies reach terminal/GUI | Never surface authorization or raw provider response bodies |

Authoritative durable state is `agents.defaults` in the existing config file;
catalog/cache state must never become model-selection authority. Reload, retry,
and second tabs re-read config. Concurrent config-write behavior is pre-existing;
this proposal adds no new write path. Refreshing discovery must not restart the
gateway or reset model/effort. No PHI route, local inference custody, session
identity, image pipeline, or artifact persistence changes are proposed.

## B. Do not build

- No new UI page, HTTP route, model registry service, or credential store.
- No subprocess dependency on an installed Codex CLI or its private cache format.
- No universal API-key/Codex catalog: ChatGPT OAuth uses its account-specific
  backend; API-key model discovery is a separate transport contract.
- No automatic model substitution, config migration, release, tap edit, or deployment.
- No use of `ultra` merely because it is listed: it advertises automatic delegation,
  which sciClaw's generic effort serialization has not proven compatible with.

## C. Recommended patch and tests

1. Add a small common identity helper in an import-cycle-free package; update
   `providers/codex_provider.go` and discovery to use the same supported version.
   Upstream shared client sets originator and User-Agent; sciClaw's existing
   `version` header is retained in the experiment, not proven essential alone.
2. Add a Codex catalog fetch/parser in `pkg/models`, using existing credential
   refresh and Cloudflare transport facilities. Inject HTTP/auth boundaries for
   deterministic tests. Validate visible entries and numeric version gates,
   dedupe IDs, preserve order, efforts, display names, and provider/source.
3. Keep `DiscoverResult.Models []string` for backwards compatibility; add an
   optional map keyed by ID containing provider/source and reasoning metadata.
   Normalize native effort objects there. Do not silently append unavailable
   Codex built-ins after a successful account catalog response.
4. On failure, return a warning and clearly identified built-in fallback. Start
   with this existing fallback; defer disk caching until needed. If caching is
   added later, key it by account/provider/client version, restrict permissions,
   bound staleness, and preserve source labels. Never cache credentials.
5. Extend `web_cmd.go`, `tui/tab_models.go`, `tui/tab_settings.go`,
   `web/src/lib/api.ts`, `web/src/pages/ModelsPage.tsx`, and
   `web/src/pages/SettingsPage.tsx` to consume capabilities. The existing CLI
   `models list` prints built-ins while `discover` is the shared selector path:
   expose the discovered choices in list too, avoiding two conflicting catalogs.
6. Change fresh-config defaults in `pkg/config/config.go` and Codex provider
   fallback to `gpt-6.1-sol`; set fresh reasoning to medium. Update onboarding,
   templates/help, fixtures and docs only where they encode the actual default.
   Existing server configuration remains explicitly out of scope for this PR.
7. Validate effort on the backend for the selected model, including manual CLI
   selection. When metadata is unavailable, retain manual model entry and issue
   a clear validation limitation rather than claim account availability.

Tests in this PR:

```sh
go test ./internal/catalogpreflight -v -count=1
go test ./internal/catalogpreflight ./pkg/models ./cmd/picoclaw \
  -run 'Test(Catalog|VersionGate|Preflight|HandleModelsCatalog)' -count=1
```

The isolated HTTP experiment proves proposed request/selection/error mechanics;
it does not prove wiring to production `Discover`. The browser characterization
test deliberately proves the current limitation and must be inverted in the
implementation PR. The config test checks existing persistence, not new defaults.

Required implementation test ladder:

- Unit/contract: injected `/models` response excludes hidden/newer-client IDs,
  preserves medium/max, and bounds failed responses. Remove each filter/header
  or drop metadata and the relevant assertion must fail.
- Composition root: `models discover --json` with injected OAuth dependencies
  returns native catalog metadata; both UI adapters retain it. This must fail
  on the old hardcoded/discarding paths. Test secondary OpenAI when Claude is
  primary, and API-key mode separately.
- Packaging verifier: built CLI default config reports Sol/medium; embedded
  browser bundle matches source and selectors. Read-only installed CLI checks
  should identify old package versions rather than overwrite them.
- Live acceptance: installed candidate discovers account choices; select
  Sol/medium in terminal and browser, restart gateway, reload and verify status,
  then complete one inference/tool call. Repeat refresh offline to verify warning
  and unchanged durable selection. Catalog HTTP 200 alone is insufficient.

CI runs formatting, vet and `go test ./...`; browser bundle validation is separate
for the later UI implementation. Existing saved defaults and the Homebrew release
path require an explicit upgrade decision before deployment.

## D. Verdict

GO for the bounded implementation above. First falsifiable integration test:
inject a Codex catalog containing Sol with medium/max into `Discover`; assert
the CLI JSON and browser adapter preserve those capabilities without changing
saved selection. This currently fails by design because discovery has no Codex
endpoint branch and the adapter discards metadata.

UNKNOWN: live inference/tool compatibility with new identity, expired-token
refresh composition, installed GUI behavior, and existing-user migration policy.
These do not block coding the discovery path; they block release/deployment
acceptance until explicitly tested or decided.

## Verification results

- All microtests and existing catalog adapter test passed.
- `go vet ./...`, CLI build, formatting and diff checks passed.
- Mutation checks on temporary copies failed as expected when visibility filtering
  was removed, reasoning metadata discarded, or version gating bypassed.
- `go test ./...` did not pass on this host: ctxclaw missing-binary assertion,
  browser stderr assertion, and an agent TempDir cleanup race failed.
- Targeted unchanged-main comparison reproduces the ctxclaw assertion. The agent
  cleanup passes on retry. The browser test can take its real lightweight-chat
  path through host config instead of the stub: candidate run successfully called
  the configured provider and failed its assertion; unchanged-main retry hit a
  provider overload, fell back to the stub, and passed. This test is not hermetic.
  No production fixes for these unrelated failures are included.
- Host-dependent full tests inadvertently made live requests with the existing
  configured provider and synthetic `hello` input. No live chat is needed for
  this preflight; future broad runs should isolate HOME/config/auth before tests.

Local evidence logs (not committed): `/tmp/sciclaw-catalog-preflight-tests.log`,
`/tmp/sciclaw-catalog-base-check.log`, `/tmp/sciclaw-catalog-preflight-vet.log`,
and `/tmp/sciclaw-catalog-mutant-*.log`. Independent review remains pending.

Upstream references inspected:

- https://github.com/openai/codex/blob/main/codex-rs/models-manager/models.json
- https://github.com/openai/codex/blob/main/codex-rs/login/src/auth/default_client.rs
- https://github.com/openai/codex/blob/main/codex-rs/codex-api/src/endpoint/models.rs
- https://developers.openai.com/api/docs/models/gpt-6.1-sol

## Independent review request

Review exact base/head and full diff without trusting this report. No independent
review has been completed. Check that no production files or credentials changed;
rerun microtests; distinguish test-only experiment, characterization, and runtime
evidence. Verify request headers/version gates, sanitized bounded failures,
metadata loss in both adapter paths, config persistence, secondary providers,
OAuth/API-key separation, and release exclusions. Identify any proposed contract
the experiment fails to exercise. Return findings with file/line evidence and
GO/NO-GO for the proposed implementation; do not merge or deploy.
