# Punch-List — New Sister-Site Pages Need the SEO Layer

**For:** Engineering · **From:** Ernie · 2026-07-04
**Verified live (external, initial-HTML = crawler view) on medicalfigs.com + qmsfigs.com.**

## Summary
New content pages shipped under a `/medical/*` and `/qms/*` namespace and **render great for
humans** (real walkthroughs + generated imagery). But they **bypassed the D1–D4 SEO layer** the
main site now uses: their crawler-visible HTML is a generic shell, and they're not in the
sitemap. To Google they're near-invisible. This is the same D1/D2/D4 pattern we already fixed —
re-introduced on the new pages. Below: what exists, the gaps, and acceptance checks.

## What actually exists (both brands, symmetric)
| Path | HTTP | Renders (human) | In sitemap? |
|---|---|---|---|
| `/medical` · `/qms` (hub) | 200 | yes | no |
| `/medical/how-to` · `/qms/how-to` | 200 | yes (nice) | no |
| `/medical/examples` · `/qms/examples` | 200 | yes | no |
| `/medical/use-cases` · `/qms/use-cases` | **404** | — | — |
| keyword landing pages (`/patient-education-illustrations`, `/process-flowchart-maker`, …) | **404** | — | — |

Sitemaps still list only the 6 generic URLs (`/`, `/examples`, `/how-to`, `/docs`, `/privacy`,
`/terms`). None of the new `/medical/*` or `/qms/*` pages appear.

## The gaps

### G1 — Pages are client-rendered; no per-route head in initial HTML (D1/D4 regression)
Fetching the raw HTML of `/medical/how-to` (before JS) returns:
- `<title>MedicalFigs | MedicalFigs</title>` — generic, doubled, not the page's real title
- no `<h1>` (JS-injected)
- no `<link rel="canonical">`
- no OG/Twitter tags
- no `FAQPage` JSON-LD

Same on `/qms/how-to` (`QMSFigs | QMSFigs`), the hub pages, and `/…/examples`.
**Fix:** render every `/medical/*` and `/qms/*` page through the **same prerender/head pipeline
as the main landing pages** — unique `<title>` (≤60), meta description (≤155), self-canonical,
OG/Twitter, and `FAQPage`+`Organization` JSON-LD, all in the **initial HTML**. Contrast target:
`https://www.sciencefigs.com/graphical-abstract-maker` already does this correctly — match it.

### G2 — New pages not in the host sitemap (D2)
The route manifest → sitemap generator isn't picking up the `/medical/*` and `/qms/*` routes.
**Fix:** ensure every indexable new page is emitted into its host sitemap (same-domain only).
Target counts after fix: each host sitemap should list the hub + how-to + examples + the
landing pages below.

### G3 — Duplicate content: `/medical/how-to` vs `/how-to`
Both `/how-to` (in sitemap) and `/medical/how-to` (new) return 200 with similar content; same
for `/examples` vs `/medical/examples`. Two near-duplicate pages compete with each other.
**Fix:** pick one canonical location per topic. Recommended: keep the branded `/medical/*`
versions as the real pages, `301` the bare `/how-to` and `/examples` to them (or make the bare
ones canonical to the branded ones), and ensure only the chosen version is in the sitemap.

### G4 — The keyword landing pages were never built (the actual SEO value)
The pages that target search demand — from the delivered content package
(`content/medicalfigs.json`, `content/qmsfigs.json`) — do **not** exist (all 404):
- Medical: `/patient-education-illustrations`, `/mechanism-of-action-diagrams`,
  `/anatomy-illustrations`, `/nursing-education-diagrams`, `/medical-poster-maker`, `/use-cases`
- QMS: `/process-flowchart-maker`, `/fishbone-diagram-maker`, `/sop-visuals`,
  `/capa-diagram-maker`, `/iso-process-maps`, `/use-cases`

The how-to/examples/hub pages are supporting content; **these keyword pages are what rank for
"process flowchart maker," "patient education handout maker," etc.** Build them from the supplied
JSON through the standard template (they inherit the correct head/schema/sitemap treatment by
construction, which also resolves G1/G2 for them). Slugs may sit at root or under the `/medical`
/`/qms` namespace — just keep them consistent in the content `slug` field and the sitemap.

## Priority
1. **G4** — build the keyword landing pages (highest SEO value; doing it via the template also
   gives them G1/G2 for free).
2. **G1 + G2** — apply prerender head + sitemap inclusion to the already-live hub/how-to/examples.
3. **G3** — de-duplicate `/how-to` vs `/medical/how-to` with a canonical/301 decision.

## Acceptance checks (initial HTML, before JS — this is what Google sees)
```bash
# G1: crawler-visible head on a new page — must be page-specific, not "Brand | Brand"
curl -s https://www.medicalfigs.com/medical/how-to | grep -E '<title>|canonical|"FAQPage"'
# EXPECT: unique <title>, a self-canonical <link>, and FAQPage JSON-LD — NOT "MedicalFigs | MedicalFigs"

# G4: keyword landing pages exist, self-canonical, unique title (run for each slug)
curl -s -o /dev/null -w '%{http_code}\n' https://www.medicalfigs.com/patient-education-illustrations  # EXPECT 200
curl -s -o /dev/null -w '%{http_code}\n' https://www.qmsfigs.com/process-flowchart-maker              # EXPECT 200

# G2: sitemap now includes the new pages, same-domain only
curl -s https://www.medicalfigs.com/sitemap.xml | grep -c '<loc>'   # EXPECT >= 11 (was 6)
curl -s https://www.qmsfigs.com/sitemap.xml    | grep -c '<loc>'    # EXPECT >= 11 (was 6)

# G3: only one of /how-to vs /medical/how-to is canonical; the other 301s or canonicals to it
curl -sI https://www.medicalfigs.com/how-to | grep -i location    # if 301 chosen
```

Deliver back a PR with the above evidence, same format as PR #137. Ping when the initial-HTML
head is fixed and I'll re-run the external crawler-view battery on every new page.
