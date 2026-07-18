# Image-generated slide artifact routing closeout

## Answer first

Ready for independent review in draft PR [#128](https://github.com/drpedapati/sciclaw/pull/128). The implementation is committed and pushed; merge, release, Homebrew publication, and Data3 deployment have not been performed.

## What changed

SciClaw now routes image generation by the requested final product instead of allowing tool salience to switch between a wordless illustration and an editable PowerPoint. The obsolete “real experimental data” warning was removed from runtime guidance, workspace templates, and the image-generation RFC policy. A built-in `image-generation` skill distinguishes four products: standalone image, visual asset for another artifact, editable presentation, and complete worded image-generated slide.

The complete-slide contract encodes the successful DEB stress-capacity pattern: 16:9 composition, action title, concise audience-facing wording, integrated exhibit or visual analogy, and clear takeaway. Provider image transport, persistence, and Discord attachment code are unchanged.

The repository now vendors `.agents/skills/ship-job/`, and root `AGENTS.md` points repository work to that copy. Its SciClaw continuation gates merge, versioning, release, Homebrew deployment, surgical DEB workspace guidance refresh, canary verification, and rollback.

## Repository position

- Repository: `https://github.com/drpedapati/sciclaw`
- Worktree: `/Users/ernie/Developer/sciclaw/.worktrees/image-slide-artifact-routing`
- Branch: `codex/image-slide-artifact-routing`
- Base: `main` at `8b9a9df8670e93ef5e233aa2c2ad5cc7bc7a7743`
- Reviewed implementation commit: `8d3cae4c`
- Pull request: [#128](https://github.com/drpedapati/sciclaw/pull/128)

This correspondence is a packet-only follow-up to the reviewed implementation commit. Review the current PR head to include this document.

## Merge train

One car: PR #128, `main` → `codex/image-slide-artifact-routing`. It has no branch or PR prerequisite. Safe order is review and green CI, then an explicitly authorized merge. Release and deployment follow only after that merge.

## Verification

- `go test ./pkg/agent ./pkg/providers ./pkg/workspacetpl/...` — passed.
- `go test ./... -count=1` — passed sequentially across all packages.
- `go vet ./...` — passed.
- `go build ./cmd/picoclaw` — passed.
- `git diff --check` — passed.
- `uv run --with pyyaml python .../quick_validate.py .agents/skills/ship-job` — valid.
- `uv run --with pyyaml python .../quick_validate.py skills/image-generation` — valid.
- Independent standards review — initial duplication concern fixed; re-review found no material findings.
- Independent spec review — DEB refresh, academic hierarchy, and routing-test gaps fixed; re-review found no material gaps.

One timing-sensitive binary-inspection test was killed while four Go workloads ran concurrently. The isolated test passed, and the complete suite then passed sequentially. This was treated as resource contention, not hidden as a green first run.

## Limits

- No merge, tag, GitHub release, Homebrew tap update, or deployment has occurred.
- Live four-mode DEB acceptance requires the released binary and refreshed routed workspace, so it remains a post-release canary.
- The provider does not persist an internal rewritten image prompt; this change does not claim prompt-level reproducibility beyond saved user/session context.

## Next safe action

Run the independent GitHub review below against the current PR head and close CI. If no material findings remain, request explicit merge authorization. After merge, use `.agents/skills/ship-job/references/sciclaw-release.md` for a separately authorized release and Data3 deployment.

## Independent review prompt

```text
Act as an independent senior engineer reviewing a proposed SciClaw change. Do not trust the implementer's summary as proof. Inspect the repository, exact PR diff, tests, prompt assembly, skills, packaging, and cited evidence yourself.

When finished, submit findings on the pull request as a GitHub review. Approve only if no material findings remain; request changes for merge blockers; comment when evidence is incomplete. Do not merge, push fixes, or resolve your own findings.

Repository: drpedapati/sciclaw — https://github.com/drpedapati/sciclaw
Pull request: #128 — https://github.com/drpedapati/sciclaw/pull/128
Base branch and commit: main at 8b9a9df8670e93ef5e233aa2c2ad5cc7bc7a7743
Head branch: codex/image-slide-artifact-routing; inspect the current PR head
Worktree: /Users/ernie/Developer/sciclaw/.worktrees/image-slide-artifact-routing

Job requested:
Vendor the ship-job development skill inside SciClaw and point repository guidance to it. Remove the misleading generated-image provenance warning. Preserve working provider transport. Route standalone images, slide assets, editable PowerPoint, and complete worded image-generated slides as distinct products. Complete-slide mode must encode the successful DEB stress-capacity communication hierarchy. Prepare the verified draft PR and a gated Brew/Data3 continuation without merging or deploying.

Material constraints:
- No provider image transport, media persistence, or Discord attachment rewrite.
- No merge, release, Homebrew mutation, or production deployment in this PR run.
- Existing DEB guidance must be refreshed surgically after release; unrelated workspace instructions must survive.

Implemented design:
The runtime hosted-tool note and workspace template route to one canonical built-in image-generation skill. That skill owns the detailed four-product contract and presentation hierarchy. Baseline onboarding installs it. Negative regression tests prevent restoration of the removed warning and assert all four routes. A repo-local ship skill owns PR closeout and a separately gated SciClaw release continuation.

Changed-file map:
- pkg/agent/loop.go: concise hosted-tool routing pointer.
- pkg/agent/context_test.go: prompt, template, and canonical-skill regressions.
- skills/image-generation/SKILL.md: four-product source of truth.
- cmd/picoclaw/main.go: baseline skill installation.
- pkg/workspacetpl/templates/workspace/AGENTS.md: workspace routing pointer.
- docs/issues/gpt55-image-generation-rfc.md: replace obsolete policy.
- .agents/skills/ship-job/** and AGENTS.md: vendored development workflow and pointer.
- .gitignore: allow only the vendored ship skill beneath otherwise local `.agents` state.

Claims requiring independent verification:
1. Runtime/template/skill guidance no longer contains the obsolete warning except negative test literals.
2. All four requested artifact modes, academic hierarchy, 16:9 complete-slide contract, and DEB reference are protected by meaningful tests.
3. Provider transport and outbound attachment behavior are unchanged.
4. Existing DEB workspace remediation is bounded, diff-verified, and release-gated.

Tests reported:
- go test ./... -count=1 — pass
- go vet ./... — pass
- go build ./cmd/picoclaw — pass
- git diff --check — pass
- both SKILL.md packages pass quick_validate.py

External ground truth:
The DEB Discord thread produced a successful complete image-generated stress-capacity slide only after the user explicitly distinguished it from both a wordless conceptual image and an editable PowerPoint. The implementation uses that communication hierarchy as a qualitative reference, not a pixel snapshot.

Known limitations:
- Live DEB four-mode acceptance is a post-release canary.
- Internal provider-rewritten image prompts are not persisted.

Review tasks:
1. Confirm the diff matches requested behavior without broader changes.
2. Flag dead code, dual policies, or leftovers from earlier attempts.
3. Trace runtime prompt assembly and baseline skill installation.
4. Check failure modes, workspace migration safety, packaging, and rollback.
5. Confirm tests fail for old behavior and cover important negatives.
6. Reproduce critical claims where practical.
7. Report findings by severity with file/line references.
```
