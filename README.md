# sciClaw

Autonomous paired-scientist platform built as a custom fork and research extension of [PicoClaw](https://github.com/sipeed/picoclaw).

## Project Goal

Build `sciclaw.dev`: a lightweight autonomous scientist that pairs with a human researcher across planning, execution, reproducibility logging, and manuscript generation.

## Current Status

- Private GitHub repo created: `https://github.com/drpedapati/sciclaw`
- Upstream cloned locally in `github/picoclaw`
- Upstream initial build verified (`make deps && make build`)
- Initial Quarto manuscript outline created in `manuscript/sciclaw-manuscript-outline.qmd`

## Structure

- `plans/` planning loop, activity log, and execution log
- `manuscript/` Quarto manuscript drafts and assets
- `github/` local upstream clone workspace (git-ignored)
