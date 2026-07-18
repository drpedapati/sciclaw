# Independent review prompt

```text
Act as an independent senior engineer reviewing this SciClaw pull request. Do not trust the implementer's summary as proof. Inspect the exact base/head commits, full diff, tests, prompt assembly, runtime skill routing, packaging, and cited evidence.

Submit findings as a GitHub review. Approve only when no material findings remain; request changes for merge blockers; comment when evidence is incomplete. Do not merge or push fixes.

Verify:
1. The diff matches the requested behavior without unrelated changes.
2. Removed prompt language is absent from runtime, templates, documentation, and tests.
3. Artifact routing distinguishes standalone images, slide assets, editable PowerPoint, and complete image-generated slides.
4. Tests would fail for the prior behavior and cover important negative cases.
5. Provider image transport, persistence, and Discord attachment behavior remain intact.
6. Homebrew/release implications and rollback are accurately described.
7. No dead code, duplicate paths, stale attempt leftovers, secrets, or generated debris remain.

Return: verdict, findings by severity with file/line references, evidence and commands rerun, unverified claims, and minimal remediation before merge.
```
