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

    if problems:
        print("docs/ is published to sciclaw.dev. These files should not be there:\n")
        for p in problems:
            print(f"  - {p}")
        print(f"\n{len(problems)} problem(s).")
        return 1

    print("ok  docs/ contains only publishable files")
    return 0


if __name__ == "__main__":
    sys.exit(main())
