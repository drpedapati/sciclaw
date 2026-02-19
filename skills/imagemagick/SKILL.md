---
name: imagemagick
description: Use this skill whenever scientific image assets need deterministic preprocessing (resize, crop, convert, DPI normalization, montage/contact sheets) using ImageMagick 7 `magick`.
---

# ImageMagick Skill

Use this skill for reproducible image preparation in manuscript and presentation workflows.

Primary CLI: `magick` (ImageMagick 7)

## When to Use

Use this skill when a task involves:
- converting image formats (`png`, `jpg`, `tiff`, `webp`, `pdf` pages to images)
- resizing or cropping figures for publication constraints
- setting or normalizing DPI / pixel density
- trimming whitespace around plots/diagrams
- generating contact sheets / montages for quick review
- applying deterministic preprocessing before OCR or document export

## Core Rules

1. Always write to a new output path; do not overwrite originals unless explicitly requested.
2. Prefer deterministic commands with explicit size, density, and quality values.
3. Log command and output artifact path in the manuscript/project notes when results support claims.
4. Standardize on `magick` command syntax (not legacy `convert`) for portability in sciClaw.

## Quick Recipes

### 1) Resize to max width while preserving aspect ratio

```bash
magick input.png -resize 1600x output-1600w.png
```

### 2) Normalize whitespace and add consistent border

```bash
magick input.png -trim +repage -bordercolor white -border 20 output-trimmed.png
```

### 3) Convert with explicit quality

```bash
magick input.tif -quality 92 output.jpg
```

### 4) Set DPI metadata for journal pipelines

```bash
magick input.png -units PixelsPerInch -density 300 output-300dpi.png
```

### 5) Crop exact region

```bash
magick input.png -crop 1200x900+100+80 +repage output-crop.png
```

### 6) Build contact sheet for fast visual QA

```bash
magick montage figure-*.png -tile 4x -geometry 600x600+8+8 contact-sheet.png
```

### 7) First page of PDF to PNG for preview/review

```bash
magick -density 300 manuscript.pdf[0] -quality 95 manuscript-page1.png
```

## Scientific Workflow Patterns

### Figure normalization batch

```bash
mkdir -p normalized
for f in figures/*.png; do
  base="$(basename "$f" .png)"
  magick "$f" -trim +repage -resize 1800x -units PixelsPerInch -density 300 "normalized/${base}-norm.png"
done
```

### OCR pre-processing

```bash
magick scan.jpg -colorspace Gray -auto-level -sharpen 0x1.0 -density 300 scan-ocr-ready.png
```

## Validation Checklist

- Output files exist and are non-empty.
- Pixel dimensions match requested constraints.
- DPI metadata is set when required by downstream tools.
- No source files were mutated unintentionally.

## Failure Handling

- If `magick` is missing: install ImageMagick (`brew install imagemagick`).
- If policy/security errors occur on PDF operations: prefer working on image exports first, then re-run with simpler options.
- If results look oversharpened or lossy: reduce aggressive filters and keep a lossless intermediate (`.png` or `.tif`).

## Interoperability Notes

- Pair with `pandoc-docx` and `docx-review` when preparing manuscript-ready figures.
- Pair with `pdf` skill for PDF extraction/assembly workflows.
- Pair with `explainer-site` and `beautiful-mermaid` when polishing visual assets for web explainers.

