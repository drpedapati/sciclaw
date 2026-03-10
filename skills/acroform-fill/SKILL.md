---
name: acroform-fill
description: "Inspect, schema, and fill true AcroForm PDFs using sciClaw's pdf_form_inspect, pdf_form_schema, and pdf_form_fill tools. Use when: (1) checking whether a PDF is a real fillable AcroForm, (2) listing form field names, types, and choices, (3) filling a supported form from structured values, (4) preparing reviewable insurance, legal, administrative, or clinical form drafts, (5) reporting skipped fields or unused input keys after a fill. Do not use for scanned PDFs, OCR tasks, or XFA-only forms."
metadata: {"nanobot":{"requires":{"bins":["pdf-form-filler"]},"install":[{"id":"brew","kind":"brew","formula":"pdf-form-filler","bins":["pdf-form-filler"],"label":"Install pdf-form-filler (brew)"}]}}
---

# acroform-fill

Use this skill for fillable PDF form workflows backed by sciClaw's dedicated PDF form tools.

## Core workflow

1. Run `pdf_form_inspect` first.
2. If `isSupportedAcroForm` is `false`, stop and explain that sciClaw will not force-fill non-AcroForm, XFA-only, or scanned PDFs.
3. Run `pdf_form_schema` to get the field inventory before preparing values.
4. If values are not already in a JSON file, create one in the workspace with `write_file`.
5. Run `pdf_form_fill` to a **new** output path.
6. Keep `flatten=false` by default. Only flatten when the user explicitly wants final non-editable output.
7. Report `skippedFields` and `unusedInputKeys` clearly. If either is non-empty, treat the output as review-needed.
8. If you need to send the resulting PDF back to the user, use `message` with the output file as an attachment.

## Rules

- Never overwrite the source PDF.
- Never skip `pdf_form_inspect`.
- Do not use `read_file` on raw PDFs.
- Prefer exact field names from `pdf_form_schema`; do not invent field names.
- For choice fields, use the schema's `choices` exactly.
- If the user only wants to know whether a PDF is fillable, stop after `pdf_form_inspect`.
- If the user gives narrative text instead of structured values, convert that text into a JSON values file before calling `pdf_form_fill`.
- For sensitive or regulated workflows, prefer reviewable output before flattening.

## Minimal examples

### 1. Check whether a PDF is a real AcroForm

Use when the user asks: "Is this PDF fillable?"

```json
{"pdf_path":"forms/prior-auth.pdf"}
```

Tool:

- `pdf_form_inspect`

Good outcome:

- `isSupportedAcroForm = true` -> continue if needed
- `isSupportedAcroForm = false` -> stop and explain why

### 2. Get the field schema before filling

Use when the user asks: "What fields are on this form?"

```json
{"pdf_path":"forms/prior-auth.pdf"}
```

Tool:

- `pdf_form_schema`

Then summarize the important field names, types, and any `choices` the model must respect.

### 3. Fill from an existing values JSON file

First make sure the values file exists in the workspace, for example:

```json
{
  "PatientName": "Jane Doe",
  "DOB": "1978-02-14",
  "Urgent": true
}
```

Then call:

```json
{
  "pdf_path":"forms/prior-auth.pdf",
  "values_path":"memory/prior-auth.values.json",
  "output_path":"memory/prior-auth.filled.pdf",
  "flatten":false
}
```

Tool:

- `pdf_form_fill`

### 4. Fill from narrative text

Use when the user gives a note, letter, or summary instead of a ready JSON file.

Workflow:

1. `pdf_form_inspect`
2. `pdf_form_schema`
3. `write_file` a JSON values file in the workspace
4. `pdf_form_fill`
5. Report any skipped or unused fields

## When not to use this skill

- OCR or scanned-document extraction
- Free-form PDF text extraction
- XFA-only forms that are not true AcroForms
- Cases where the user wants arbitrary PDF editing instead of form filling
