# PR #118 follow-up review packet

## Requested review

Act as an independent senior engineer reviewing this SciClaw pull request. Do not trust the implementer's summary as proof. Inspect the exact base/head commits and full diff. Submit findings as a GitHub review; do not merge or push fixes.

Verify:

1. The diff contains only the two Codex P1 migration fixes from #118.
2. A failed copy or config rewrite preserves the legacy directory and prevents cleanup/symlinking.
3. `~/.picoclaw/workspace` moves into `~/sciclaw` before legacy cleanup.
4. The regression tests fail against the #118 base behavior.
5. No secrets, generated files, or unrelated history are included.

Return a verdict, findings by severity with file and line references, commands rerun, and the minimal remediation needed before merge.

## Closeout

- Outcome: ready for review.
- Base: `origin/codex/migrate-config-from-.picoclaw-to-sciclaw` (`e106d88`).
- Scope: migration safety fixes and focused regression tests only.
- Verification: `go test ./pkg/migrate`, `go build ./...`, and `git diff --check`.
- Merge train: merge #118, then this stacked draft.
- Excluded: no release, deployment, or unrelated migration work.
