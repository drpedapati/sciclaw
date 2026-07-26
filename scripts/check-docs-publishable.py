#!/usr/bin/env python3
"""Guard: everything under docs/ is published to sciclaw.dev.

docs/ is the Cloudflare Pages publish root. There is no build step and no
ignore mechanism, so any file placed there is world-readable the moment it
merges. Internal engineering docs (RFCs, ops runbooks, correspondence, drafts)
belong in internal/ at the repo root instead.

This check exists because 23 internal documents — ops runbooks, a production
switchover doc, RFCs, a grant-application email, and draft tweets — were live
and crawlable on sciclaw.dev before anyone noticed. Nothing prevented it.

Run: python3 scripts/check-docs-publishable.py
"""

import os
import re
import sys
from pathlib import Path

DOCS = Path(__file__).resolve().parent.parent / "docs"

# Markdown files that are deliberately public.
#   - v*-announcement.md : release notes; changelog.html links two of them
#   - phi-mode-local-model-flow.md : user-facing product documentation
ALLOWED_MD = re.compile(r"^(v[\d.]+(-dev\.\d+)?-announcement\.md|phi-mode-local-model-flow\.md)$")

# Directory names that must never appear under docs/.
FORBIDDEN_DIRS = {"ops", "issues", "internal", "correspondence", "rfc", "rfcs"}

# Filename fragments that suggest a document was never meant to be published.
SUSPICIOUS = re.compile(
    r"(^|[-_])(internal|private|draft|secret|credential|runbook|switchover|"
    r"correspondence|tweets?|grant)([-_.]|$)",
    re.IGNORECASE,
)


def main() -> int:
    problems: list[str] = []

    for path in sorted(DOCS.rglob("*")):
        if path.is_dir():
            if path.name.lower() in FORBIDDEN_DIRS:
                problems.append(
                    f"{path.relative_to(DOCS.parent)}/ — internal directory inside the publish root; "
                    f"move it to internal/"
                )
            continue

        rel = path.relative_to(DOCS)

        if path.suffix == ".md" and not ALLOWED_MD.match(path.name):
            problems.append(
                f"docs/{rel} — markdown in the publish root. Move to internal/, "
                f"or add it to ALLOWED_MD if it is genuinely public."
            )

        if SUSPICIOUS.search(path.stem) and path.suffix in {".md", ".html", ".txt"}:
            problems.append(
                f"docs/{rel} — filename suggests an internal document; confirm it should be public."
            )

    problems += check_seo_contract()

    if problems:
        print("docs/ is published to sciclaw.dev. Problems found:\n")
        for p in problems:
            print(f"  - {p}")
        print(f"\n{len(problems)} problem(s).")
        return 1

    print("ok  docs/ contains only publishable files and meets the SEO contract")
    return 0


def check_seo_contract() -> list[str]:
    """Every indexable page needs a head and exactly one <h1>, and the sitemap
    must match the filesystem. These are the defects an external audit had to
    find by hand: section-install.html sat at the top sitemap priority with no
    description, no canonical and zero <h1>."""
    problems: list[str] = []

    sitemap = (DOCS / "sitemap.xml").read_text()
    listed = {
        m.rstrip("/").rsplit("/", 1)[-1] or "index"
        for m in re.findall(r"<loc>https://sciclaw\.dev/([^<]*)</loc>", sitemap)
    }

    for path in sorted(DOCS.glob("*.html")):
        slug = "index" if path.stem == "index" else path.stem
        html = path.read_text()
        noindex = 'name="robots" content="noindex' in html
        is404 = path.name == "404.html"

        if noindex or is404:
            if slug in listed:
                problems.append(f"docs/{path.name} — noindex but listed in sitemap.xml")
            continue

        h1s = len(re.findall(r"<h1[\s>]", html))
        if h1s != 1:
            problems.append(f"docs/{path.name} — has {h1s} <h1>, expected exactly 1")
        if 'name="description"' not in html:
            problems.append(f"docs/{path.name} — no meta description")
        if 'rel="canonical"' not in html:
            problems.append(f"docs/{path.name} — no canonical")
        elif f'href="https://sciclaw.dev/{"" if slug == "index" else slug}"' not in html:
            problems.append(f"docs/{path.name} — canonical is not self-referencing")
        if ".html" in " ".join(re.findall(r'href="([^"]*)"', html)):
            problems.append(f"docs/{path.name} — .html link (Pages 308-redirects these)")
        if slug not in listed:
            problems.append(f"docs/{path.name} — indexable but missing from sitemap.xml")

    for slug in sorted(listed):
        if slug.endswith(".pdf"):
            continue
        name = "index.html" if slug == "index" else f"{slug}.html"
        if not (DOCS / name).exists():
            problems.append(f"sitemap.xml lists /{slug} but docs/{name} does not exist")

    return problems


if __name__ == "__main__":
    sys.exit(main())
