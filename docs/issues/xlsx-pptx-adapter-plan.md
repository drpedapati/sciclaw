# XLSX / PPTX Typed Adapter Plan

## Goal

Bring `xlsx-review` and `pptx-review` into sciClaw using the same pattern already used for:
- `docx-review`
- `pdf-form-filler`

The objective is to stop relying on raw shell commands for common spreadsheet and presentation workflows, and to give the agent first-class typed tools with path validation, binary lookup, and structured JSON results.

## Current State

What already exists:
- Standalone CLIs:
  - `xlsx-review 1.2.1`
  - `pptx-review 1.2.0`
- Generic skills/docs references:
  - `skills/xlsx/SKILL.md`
  - `skills/pptx/SKILL.md`
  - docs mention `brew install drpedapati/tools/xlsx-review`
  - docs mention `brew install drpedapati/tools/pptx-review`
- `read_file` correctly blocks raw `.xlsx` / `.pptx` container bytes.

What does **not** exist yet:
- no `pkg/xlsxreview`
- no `pkg/pptxreview`
- no typed tools such as `xlsx_review_read` or `pptx_review_diff`
- no automatic fallback from blocked `read_file` to the proper format-aware tool

## Existing Pattern To Copy

### DOCX
- typed client package:
  - `pkg/docxreview/client.go`
  - `pkg/docxreview/types.go`
- typed tool wrappers:
  - `pkg/tools/docx_review.go`
- key properties:
  - binary resolution with env override
  - direct `exec.CommandContext`, not shell strings
  - path validation in tool layer
  - JSON parsing in client layer
  - write-output checks for apply/edit operations

### PDF
- typed client package:
  - `pkg/pdfform/client.go`
  - `pkg/pdfform/types.go`
- typed tool wrappers:
  - `pkg/tools/pdf_form.go`
- same architectural split:
  - client owns CLI execution and JSON contract
  - tool owns workspace/read-write policy and model-facing parameters

## Proposed V1 Scope

Keep this narrow and boring.

### XLSX tools
1. `xlsx_review_read`
- read workbook contents as structured JSON
- first-class replacement for trying to `read_file` an `.xlsx`

2. `xlsx_review_diff`
- semantic diff between two spreadsheets
- useful for review workflows and validation

3. `xlsx_review_apply`
- apply an `xlsx-review` JSON manifest to an existing workbook
- require writing to a new output path
- support `dry_run`
- support optional `author`

### PPTX tools
1. `pptx_review_read`
- extract slides, shapes, notes, comments as structured JSON

2. `pptx_review_diff`
- semantic diff between two presentations

3. `pptx_review_apply`
- apply a `pptx-review` manifest to an existing presentation
- require a new output path or explicit in-place flag only if we choose to allow it later
- support `dry_run`
- support optional `author`

## Explicit Non-Goals For V1

Do **not** expose these initially:
- `xlsx-review --create`
- `xlsx-review --textconv`
- `xlsx-review --git-setup`
- `pptx-review --textconv`
- `pptx-review --git-setup`
- broad shell fallback workflows

Reason:
- first-class sciClaw tools should cover the common agent workflows first
- Git integration and create flows are valid, but they are second-order features

## Proposed File Layout

### New packages
- `pkg/xlsxreview/client.go`
- `pkg/xlsxreview/types.go`
- `pkg/xlsxreview/client_test.go`

- `pkg/pptxreview/client.go`
- `pkg/pptxreview/types.go`
- `pkg/pptxreview/client_test.go`

### New tool wrappers
- `pkg/tools/xlsx_review.go`
- `pkg/tools/xlsx_review_test.go`
- `pkg/tools/pptx_review.go`
- `pkg/tools/pptx_review_test.go`

### Agent wiring
- `pkg/agent/loop.go`
  - register the new tools in `createToolRegistry()`

### Model/tool guidance
- `cmd/picoclaw/tools_policy.go`
- `pkg/workspacetpl/templates/workspace/TOOLS.md`
- optionally tighten `skills/xlsx/SKILL.md` and `skills/pptx/SKILL.md`

### Read-file guidance
- `pkg/tools/filesystem.go`
  - update `.xlsx` and `.pptx` hints to point at the new typed tools once they exist

## Environment / Binary Resolution

Match the existing style:
- `PICOCLAW_XLSX_REVIEW_BINARY`
- `PICOCLAW_PPTX_REVIEW_BINARY`

Resolution order:
1. explicit env override
2. `exec.LookPath("xlsx-review")` / `exec.LookPath("pptx-review")`
3. fallback Homebrew candidate paths

## Expected Client Contracts

### XLSX read
CLI:
- `xlsx-review <input.xlsx> --read --json`

Tool response:
- JSON string from `xlsx-review`

### XLSX diff
CLI:
- `xlsx-review --diff <old.xlsx> <new.xlsx> --json`

### XLSX apply
CLI:
- `xlsx-review <input.xlsx> <manifest.json> --output <out.xlsx> --json`
- optional:
  - `--dry-run`
  - `--author <name>`

### PPTX read
CLI:
- `pptx-review <input.pptx> --read --json`

### PPTX diff
CLI:
- `pptx-review --diff <old.pptx> <new.pptx> --json`

### PPTX apply
CLI:
- `pptx-review <input.pptx> <manifest.json> --output <out.pptx> --json`
- optional:
  - `--dry-run`
  - `--author <name>`

## Tool Parameter Shapes

### `xlsx_review_read`
- `input_path`

### `xlsx_review_diff`
- `old_path`
- `new_path`

### `xlsx_review_apply`
- `input_path`
- `manifest_path`
- `output_path`
- `dry_run`
- `author`

### `pptx_review_read`
- `input_path`

### `pptx_review_diff`
- `old_path`
- `new_path`

### `pptx_review_apply`
- `input_path`
- `manifest_path`
- `output_path`
- `dry_run`
- `author`

## Safety Rules

Tool layer must continue to own path safety:
- resolve read paths with existing workspace/shared-workspace policy
- resolve write paths separately
- forbid output path equal to input path for V1
- no shell strings
- direct argument arrays only

Client layer must own CLI correctness:
- timeout
- exit-code handling
- JSON parsing
- output-file existence checks for write/apply operations

## Fallback / Recovery Plan

V1 should **not** silently auto-fallback from `read_file`.

Instead:
1. add typed tools
2. update `read_file` hints to point to them explicitly
3. later add a narrow orchestration-layer redirect for:
   - `.xlsx` -> `xlsx_review_read`
   - `.pptx` -> `pptx_review_read`

That keeps V1 safer and easier to debug.

## Testing Plan

### Client tests
- binary resolution
- invalid/missing input path handling
- JSON parse behavior
- output path validation for apply
- output file actually written for non-dry-run apply

### Tool tests
- path validation errors
- structured JSON result pass-through
- write path enforcement
- shared workspace read/write policy

### Integration tests
- registry contains new tools
- `read_file` guidance can be updated to mention them
- agent/tool policy text prefers typed tools over raw shell

## Suggested Delivery Order

1. `pkg/xlsxreview`
2. `pkg/tools/xlsx_review.go`
3. tests
4. `pkg/pptxreview`
5. `pkg/tools/pptx_review.go`
6. tests
7. tool registry + policy wiring
8. update `read_file` hints
9. optional follow-up issue for automatic fallback

## Why This Is The Right Shape

This matches the current direction of sciClaw:
- typed tools over shell guessing
- workspace-safe execution
- explicit structured contracts
- model guidance layered on top of code, not replacing it

That is the same reason the `docx-review` and `pdf-form-filler` integrations are better than leaving those workflows as ad-hoc shell commands.
