# sciClaw Project Spec

## Repository Model and Upstream Security Sync (Permanent)

This repository (`drpedapati/sciclaw`) is the canonical project and contains both the sciClaw codebase and the manuscript (`manuscript/`) for single-repo reproducibility.

Remote policy:
- `origin` = `https://github.com/drpedapati/sciclaw.git`
- `upstream` = `https://github.com/sipeed/picoclaw.git`

Before each loop, sync upstream so security and maintenance fixes are incorporated:
1. `git fetch upstream`
2. `git merge upstream/main` into local `main` (or create a short-lived sync branch if conflicts occur)
3. Resolve conflicts while preserving sciClaw-specific and manuscript changes
4. Run build/tests and log outcomes in `plans/main-plan-activity.md` and `plans/main-plan-log.csv`
