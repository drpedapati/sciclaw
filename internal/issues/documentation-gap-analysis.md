# Documentation Gap Analysis

## Status

Updated during the `v0.2.4-dev.20` cycle.

This document separates:
- repo docs that were corrected in-tree
- remaining documentation gaps that still need follow-up before a public stable release

## Corrected In This Pass

### 1. README surface

Updated:
- Discord queue/background-job behavior
- `/btw` and `/skill` slash-command surface
- Anthropic OAuth/oat-token bridge wording
- bundled-skill list wording and missing `acroform-fill`

Files:
- [README.md](/Users/ernie/Documents/irl_projects/260212-sciclaw/README.md)

### 2. Release announcement draft

Updated:
- removed stale `v0.2.3` framing
- rewrote around the current public-release candidate feature set

Files:
- [next-release-announcement.md](/Users/ernie/Documents/irl_projects/260212-sciclaw/docs/next-release-announcement.md)

### 3. RFC / issue doc status drift

Updated:
- `/btw` RFC now marked implemented
- `/btw` slash-command RFC now marked implemented
- `/skill` slash-command RFC now marked implemented
- queue-first scheduler doc now marked historical/superseded
- public-release fix list now marked code-complete, validation-remaining

Files:
- [btw-side-lane-rfc.md](/Users/ernie/Documents/irl_projects/260212-sciclaw/docs/issues/btw-side-lane-rfc.md)
- [discord-btw-slash-command-rfc.md](/Users/ernie/Documents/irl_projects/260212-sciclaw/docs/issues/discord-btw-slash-command-rfc.md)
- [discord-skill-slash-command-rfc.md](/Users/ernie/Documents/irl_projects/260212-sciclaw/docs/issues/discord-skill-slash-command-rfc.md)
- [queue-btw-scheduler.md](/Users/ernie/Documents/irl_projects/260212-sciclaw/docs/issues/queue-btw-scheduler.md)
- [public-release-fixlist.md](/Users/ernie/Documents/irl_projects/260212-sciclaw/docs/issues/public-release-fixlist.md)

### 4. Claude OAuth parity notes

Updated:
- documented the empty-direct-answer retry path added in `b0db7c3`

Files:
- [claude-oauth-parity.md](/Users/ernie/Documents/irl_projects/260212-sciclaw/docs/claude-oauth-parity.md)

### 5. Static website docs

Updated:
- corrected stale `docs.html` wording around:
  - Anthropic auth paths
  - Discord queue/slash-command behavior
  - slash-command invite scope requirements
  - bundled-skill wording and doctor checks
- refreshed `changelog.html` so the public site reflects:
  - current stable `v0.2.3`
  - current dev preview `v0.2.4-dev.20`

Files:
- [docs.html](/Users/ernie/Documents/irl_projects/260212-sciclaw/docs/docs.html)
- [changelog.html](/Users/ernie/Documents/irl_projects/260212-sciclaw/docs/changelog.html)

## Remaining Gaps

### 1. Static site fragments still need a focused consistency pass

The main public pages are now materially closer to `main`, but the smaller static fragments were not fully audited in this pass. That includes:
- section pages under `docs/section-*.html`
- landing-page copy that may still under-describe queueing, `/btw`, `/skill`, and newer typed tools
- any duplicated capability copy embedded outside `docs.html`

Why this matters:
- the main docs page is now closer to current reality
- the rest of the website can still drift and create mixed messaging

Recommended fix:
- do one short public-site sweep before stable
- especially `index.html` plus the `section-*` fragments that echo capability claims

### 2. Public-release wording still needs final tag-specific polish

The repo now has a current announcement draft, but final public-release copy should wait until:
- the stable tag is chosen
- the final release scope is frozen

Recommended fix:
- final edit only after public stable cut decision

### 3. README on `main` now describes features newer than the last stable tag

This is normal for a repo default branch, but it creates a release-communication gap:
- repo README reflects current `main`
- Homebrew stable may still lag behind until public release is cut

Recommended fix:
- acceptable as-is for development
- pair public stable cut with final README/site sync

### 4. Operator docs for live validation are still scattered

There is no single short operator checklist for validating:
- queue behavior
- `/btw`
- `/skill`
- Claude OAuth bridge
- weather tool

Recommended fix:
- add one concise canary checklist before public release

## Release Read

Documentation is materially better after this pass, but not completely done.

The main remaining documentation work before public stable is:
1. focused public-site consistency sweep
2. final stable announcement copy
3. one short canary/validation checklist for operators
