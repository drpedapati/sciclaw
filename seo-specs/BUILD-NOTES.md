# BUILD-NOTES — sciclaw (verified 2026-07-26 by orchestrator)

- The site is NOT flat files at repo root (the brief is wrong there): the published Cloudflare Pages site is the **`docs/` directory**. The repo root is a Go project — do not touch it.
- (a) New page: create `docs/<name>.html` by hand, copying structure/nav/footer/CSS from a sibling (e.g. `docs/philosophy.html`, `docs/security.html`; shared stylesheet `docs/styles.css`).
- (b) Head: hand-authored in each .html file. No helper, no injection, no build step.
- (c) sitemap: hand-maintained `docs/sitemap.xml` — add `<loc>` entries yourself. Currently missing `/biography.html` and `/section-install.html`.
- (d) CI: `.github/workflows/{build,pr,release,docker-build}.yml` test the Go app only; nothing tests the docs site. No site test harness to wire into.
- **404 defect CONFIRMED live:** `https://sciclaw.dev/definitely-not-real-xyz.html` → HTTP 200 serving the homepage (Cloudflare Pages SPA fallback). Fix: add a `docs/404.html` (Pages serves it with a real 404 status when present). Verify after deploy.
- Keep internal specs in `seo-specs/` (repo root) — anything under `docs/` gets published to sciclaw.dev.
