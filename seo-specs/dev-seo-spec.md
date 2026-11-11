# Engineering Spec: SEO Recovery — ScienceFigs & FigsHub

**Repo:** `github.com/drpedapati/Scientific-Figures` (deploy controller: Data1, via Kamal → hel1 behind Cloudflare Tunnel)
**Date:** 2026-07-03 · **Requested by:** Ernie · **Priority:** P0 — organic signups dropped from steady growth (250 users) to ~1/week

---

## 1. Background — what was diagnosed (verified July 3, 2026)

Live diagnostics (Google SERP checks, Rich Results Test, external probes of both domains, Search Console now verified) found the infrastructure healthy but the SEO plumbing broken. The site went from steady organic signups to ~1/week; simultaneously ~6 new competitors (FigPad, ScholarViz, ConceptViz, FigCanvas, SciFig, Stork) now occupy page 1 for "AI scientific figure generator" — our exact title-tag keyword. We are fighting them with **one indexed page**.

Confirmed defects, in causal order:

| # | Defect | Evidence |
|---|--------|----------|
| D1 | **Sitewide canonical points to homepage.** Every route (e.g. `/signup`) serves `<link rel="canonical" href="https://www.sciencefigs.com/">` | Raw HTML of `/signup`; `site:sciencefigs.com` returns exactly 1 result |
| D2 | **Sitemap is 5 URLs, 3 of them foreign domains** (`teachfigs.com`, `medicalfigs.com`, `qmsfigs.com` — Google ignores cross-domain sitemap entries). Net: we submit 2 URLs | `https://www.sciencefigs.com/sitemap.xml` |
| D3 | **Soft-404s:** every unknown path returns HTTP 200 with the app shell (`/no-such-page-qx123` → 200) | External probe via Cloudflare |
| D4 | **One global title/meta for all routes** (SPA shell) | Raw HTML fetch of any route |
| D5 | **Apex + www both serve 200** (no 301; canonical mitigates but sloppy) | Probe of `https://sciencefigs.com/` |
| D6 | **figshub.com has no robots.txt, no sitemap, no canonical, no OG tags, and a catch-all route** that returns the homepage for every path incl. `/robots.txt` | Direct fetches |
| D7 | **All three sister domains (teachfigs.com, medicalfigs.com, qmsfigs.com) serve the identical ScienceFigs shell with canonical → `https://www.sciencefigs.com/`**, ScienceFigs title/meta/OG, and a robots.txt pointing at ScienceFigs' sitemap. Each domain explicitly tells Google to index sciencefigs.com instead of itself → zero pages indexed, zero organic traffic possible on any of them | Direct fetches of all 3; `site:teachfigs.com` returns nothing; Cloudflare DNS shows all 3 routed to the same hel1-tunnel origin (June 21, 2026) |

Not broken (do not spend time here): Googlebot renders the SPA fine (Rich Results Test crawls successfully and detects the SoftwareApplication JSON-LD, which is injected client-side); the Cloudflare tunnel / kamal-proxy serve all paths 200 with correct content-types; the site is not penalized (brand query ranks #1).

---

## 2. Workstream A — SEO plumbing (P0, target: this week)

### A1. Per-route `<head>` management
Replace the hardcoded head with per-route values. Every indexable route must render its **own** canonical, title, and meta description. Non-indexable app routes (`/signup`, `/login`, dashboard, editor, `/credits`) get `<meta name="robots" content="noindex">` instead of canonicalizing to home.

- Canonical: exact self URL, `https://www.sciencefigs.com` host, no trailing-slash variants.
- Title ≤ 60 chars, meta description ≤ 155 chars, unique per route (marketing copy will be supplied — see §4; use sensible placeholders meanwhile, not the homepage string).
- OG/Twitter tags per route (title, description, per-page OG image; fall back to default image only).

### A2. Real sitemap generation
Generate `sitemap.xml` at build or deploy time from the route manifest:

- Same-domain URLs only — **remove teachfigs/medicalfigs/qmsfigs** (if those sites matter, they need their own sitemaps on their own domains).
- Include every indexable marketing route (incl. all Workstream B pages), with `<lastmod>`.
- Exclude noindex/app routes.
- Must not be hand-maintained — new routes appear automatically.

### A3. True 404s
Unknown routes must return **HTTP status 404** (or 410) with a branded not-found page. Because the SPA is served by kamal-proxy/origin with a catch-all, this needs a server-side route manifest check (or prerender layer, see A4) — client-side rendering a "not found" component over a 200 is not sufficient.

### A4. Prerender/SSR for marketing routes (recommended, not required for A1–A3)
Googlebot renders the SPA today, but rendering-queue delays are real and per-route head management is simpler server-side. Recommended: prerender (e.g. build-time static generation) for all marketing/SEO routes, keeping the app itself client-rendered. Engineer's choice of mechanism; acceptance criteria in §5 are mechanism-agnostic.

### A5. Host canonicalization
301 `sciencefigs.com/*` → `www.sciencefigs.com/*` (Cloudflare redirect rule is fine — no code needed). Same for figshub.com once it has a canonical host decision.

### A6. Structured data
Keep/complete the existing `SoftwareApplication` JSON-LD (Rich Results Test flags non-critical issues — pull the report and clear them: likely missing `offers`, `aggregateRating` or `applicationCategory`). Add `Organization` JSON-LD sitewide. Serve both in initial HTML (not JS-injected) once prerendering exists. Add `FAQPage` schema on the new pages (§3).

### A7. FigsHub fix pack (P1, after A1–A6 ship for ScienceFigs)
robots.txt; sitemap.xml; canonical + OG/Twitter tags; per-route titles; distinct indexable page per studio (science, education, healthcare, quality management); true 404s; cross-link footer between figshub.com and sciencefigs.com. Mirror the same mechanisms built for ScienceFigs.

---

## 3. Workstream B — Indexable marketing pages (P1, target: next 2 weeks)

### B1. Page template (build once, data-driven)
A reusable marketing-page template rendered/prerendered with real HTML:

1. H1 + subhead (keyword-bearing)
2. Pain-point intro paragraph
3. **Live example figures** (3–6 images; this is our differentiator — real gallery output, not stock). Images need descriptive filenames + alt text (e.g. `nature-style-signaling-pathway-figure.png`)
4. How-it-works (3 steps) with CTA into the app
5. 3–5 FAQs (with `FAQPage` JSON-LD)
6. Internal links: to 3 sibling pages + homepage; homepage links back to a "Use cases" index page
7. Per-page title/meta/canonical/OG image via A1

### B2. First batch — 10 pages (slugs final, copy supplied separately)

| Slug | Target keyword |
|------|----------------|
| `/graphical-abstract-maker` | graphical abstract maker |
| `/pathway-diagram-maker` | pathway diagram maker |
| `/scientific-figure-maker` | scientific figure maker |
| `/biorender-alternative` | BioRender alternative |
| `/nature-style-figures` | figures for Nature papers |
| `/cell-style-figures` | figures for Cell papers |
| `/elife-style-figures` | figures for eLife papers |
| `/mechanism-diagram-generator` | mechanism diagram |
| `/immunology-figures` | immunology figures |
| `/neuroscience-figures` | neuroscience figures |

The template must make adding the next 40+ pages (figure-type × discipline matrix) a data/content task, not an engineering task — routes, sitemap entries, and internal links derive from one manifest.

### B3. Content
Page copy (titles, metas, H1s, intros, FAQs) will be supplied by Ernie/Claude as structured data (JSON/MD per page) — engineers own the template and manifest, not the prose. Placeholder-quality copy must not ship.

---

## 3b. Workstream C — Sister domains: teachfigs / medicalfigs / qmsfigs (P1–P2)

**Decision required from Ernie before eng work starts** (the two options need different code):

**Option 1 — Standalone brand site (recommended for teachfigs only).** The education market ("AI diagram maker for teachers," "biology teaching diagrams," "classroom science figures") is a distinct, less crowded keyword space than research figures, and none of the six new competitors target it. Requirements per activated domain:

- Host-aware rendering: when served on `teachfigs.com`, the app must emit its **own** identity — self-canonical, own title/meta/OG ("TeachFigs — AI Diagram & Figure Maker for Teachers"-class copy, supplied), own `robots.txt` pointing to **its own** sitemap, own sitemap with only its own URLs. The current single-shell-for-all-hosts behavior is the root cause of D7.
- An education-specific homepage (not the ScienceFigs homepage rebranded): teacher pain points, classroom example gallery, education FAQs.
- 3–5 education use-case pages using the B1 template (e.g. `/biology-diagrams-for-teaching`, `/anatomy-diagrams`, `/lesson-figure-maker`).
- Same plumbing standards as Workstream A: true 404s, apex→www 301, JSON-LD, GSC property verified (Ernie/assistant).

**Option 2 — Consolidate (recommended for medicalfigs + qmsfigs).** 301-redirect the domain (all paths) to a dedicated section on the main property (e.g. `sciencefigs.com/education`, `/medical`, `/qms` — or the matching FigsHub studio). One Cloudflare redirect rule each, zero app code. This concentrates authority instead of splitting it across five weak domains — with a small team, more than two active content-bearing properties will underperform one strong one.

**Recommended split: teachfigs = Option 1; medicalfigs + qmsfigs = Option 2** (revisit after teachfigs proves out). Whatever is chosen, the current state — live domains canonicalizing to a different domain — should not persist: it wastes the domains entirely.

## 4. Deliverables back to Ernie (definition of done)

1. **PR(s) on GitHub** — suggested: PR-1 = A1+A2+A3+A5 (plumbing), PR-2 = A4+A6, PR-3 = B1+B2 template & pages, PR-4 = A7 FigsHub. Each PR description lists which defects (D1–D6) it closes.
2. **Verification evidence in each PR** (see §5 — paste command output).
3. **A route manifest doc** (or self-documenting file in repo): every public route, its indexability, title, canonical.
4. **Deploy confirmation** on hel1 via Kamal, with the date/time, so search-impact can be correlated in Search Console (now verified and collecting).
5. **The content contract**: file format + location where per-page copy should be dropped for B pages, so content can be added without engineering involvement.

---

## 5. Acceptance criteria (run against production)

```bash
# D1/A1 — unique per-route canonicals; app routes noindex
curl -s https://www.sciencefigs.com/graphical-abstract-maker | grep -E 'canonical|<title>|og:title'
# EXPECT: canonical = the page's own URL; unique title
curl -s https://www.sciencefigs.com/signup | grep -E 'canonical|robots'
# EXPECT: noindex (no canonical-to-home)

# D2/A2 — sitemap: same-domain, complete
curl -s https://www.sciencefigs.com/sitemap.xml | grep -c '<loc>https://www.sciencefigs.com'
# EXPECT: count == total indexable routes (≥12 after batch B2); zero non-sciencefigs domains

# D3/A3 — true 404
curl -s -o /dev/null -w '%{http_code}' https://www.sciencefigs.com/no-such-page-qx123
# EXPECT: 404

# D5/A5 — host redirect
curl -s -o /dev/null -w '%{http_code} %{redirect_url}' https://sciencefigs.com/
# EXPECT: 301 https://www.sciencefigs.com/

# A6 — structured data
# Rich Results Test on / and one B page: SoftwareApplication + Organization valid, zero flagged issues; FAQPage detected on B pages

# A7 — FigsHub
curl -s -o /dev/null -w '%{http_code}' https://www.figshub.com/robots.txt   # EXPECT 200 text/plain
curl -s -o /dev/null -w '%{http_code}' https://www.figshub.com/no-such-page # EXPECT 404

# D7/C — sister domains (after decision)
curl -s https://www.teachfigs.com/ | grep -E 'canonical|<title>'
# Option 1 EXPECT: canonical = https://www.teachfigs.com/ and a TeachFigs title (NOT sciencefigs.com)
curl -s https://www.teachfigs.com/robots.txt | grep -i sitemap
# Option 1 EXPECT: Sitemap: https://www.teachfigs.com/sitemap.xml
curl -s -o /dev/null -w '%{http_code} %{redirect_url}' https://www.medicalfigs.com/
# Option 2 EXPECT: 301 to the mapped section on the main property
```

Post-deploy (Ernie/assistant, not eng): submit new sitemap in Search Console, request indexing on the 10 new pages, monitor Pages report.

## 6. Explicitly out of scope for eng

Keyword research, page copy, backlink outreach, directory listings, review generation, GA4/GSC analysis — handled separately under the SEO program (`seo-strategy-buildout.html` / `seo-assistant-todo.html`).

---

*Questions → Ernie. Diagnostic details available on request: SERP evidence, probe transcripts, Rich Results Test results.*
