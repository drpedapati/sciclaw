---
name: ship-job
description: Ship a SciClaw software change through an isolated worktree, implementation, pruning, verification, commit, push, draft pull request, CI closeout, and independent review. Use when asked to ship a job, complete a change through PR, prepare a release candidate, or follow the repository's happy path toward Homebrew release and Data3 deployment.
---

# Ship job

Deliver a reviewable packet, not uncommitted local work. Keep implementation in an isolated worktree and preserve unrelated changes.

## Authority boundary

Invocation authorizes implementation, commit, push, and a draft PR. It does not by itself authorize merge, tag creation, release publication, Homebrew tap mutation, or deployment. Treat those as explicit post-review gates.

## Workflow

1. Reconfirm the requested outcome, acceptance criteria, exclusions, and authority.
2. Inspect the repository root, remotes, worktrees, dirty state, base commit, related PRs, and repository guidance.
3. Create a unique branch and isolated worktree from the evidence-backed base. Reconfirm isolation before every edit, test, commit, and publish operation.
4. Read the governing code, tests, specifications, and skills. Implement the smallest cohesive change.
5. Prune the complete diff: remove dead code, duplicate paths, abandoned attempts, unnecessary abstractions, stale documentation, and generated debris.
6. Run focused tests followed by the broadest practical suite. Inspect formatting, build, `git diff --check`, status, and the complete diff. Visually inspect real renders for visual work.
7. Perform a pre-publish review. Fix material findings and rerun affected checks.
8. Commit only intended files, push the branch, and open a draft PR against the verified base.
9. Add a standalone independent-review packet using `references/external-review-prompt.md` and record the closeout using `references/correspondence-template.md`.
10. Monitor CI, mergeability, and in-scope feedback. Fix branch-caused failures without silently expanding scope.
11. Report the ordered merge train and the next authorized action.

## SciClaw release continuation

When the user separately authorizes merge, release, or deployment, read `references/sciclaw-release.md` completely and follow its gates. Never tag a commit other than the reviewed merge head. Use Homebrew as the deployment source of truth; never hot-copy or symlink a development binary into the service path.

## Completion contract

A ship job is incomplete without:

- isolated worktree and branch;
- pruned and verified change;
- commit and push;
- draft PR;
- independent-review prompt on the branch or PR;
- CI/review status and merge train;
- explicit limitations and next safe action.

Final reporting must include worktree, branch, base, commit, PR, verification, evidence, limitations, review packet path, merge train, and the next gate.
