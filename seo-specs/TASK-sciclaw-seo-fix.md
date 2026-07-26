# P2 Task — sciclaw.dev: close the sitemap + head-tag gaps

**For:** Engineering · **From:** Ernie · **Audited live:** 2026-07-25
**Repo:** `github.com/drpedapati/sciclaw` — site source lives in **`docs/`** (verified: `docs/sitemap.xml`
on `main` matches production byte-for-byte). **Host:** Cloudflare Pages, apex `sciclaw.dev`.
**Priority:** P2 — the site is in genuinely good shape. This is cleanup, not rescue. But **two of the
seven items below were not in the original crawl notes and one of them is a P1.**

> **Read this first:** the 2026-07-25 crawl (`CRAWL-automacy-and-sciclaw.md`) recorded that sciclaw.dev
> has a "clean per-page `<head>`: canonical, meta description, keywords, author, full OG + Twitter".
> **That is true of `/`, `/biography.html` and `/blog.html`. It is NOT true of
> `/section-install.html`** — the primary CTA page — which has almost no head at all. See S3.
> Correcting that is now the highest-value item in this task.

---

## What's actually wrong (each item verified live)

| # | Defect | Evidence (observed 2026-07-25) |
|---|---|---|
| S1 | **`/biography.html` and `/section-install.html` are missing from `sitemap.xml`.** The sitemap has exactly 8 `<loc>` entries: `/`, `/docs.html`, `/phi-mode.html`, `/changelog.html`, `/blog.html`, `/philosophy.html`, `/security.html`, `/sciclaw-open.pdf`. Both missing pages are linked from the homepage nav; `/section-install.html` is linked **twice** (nav "Install" + hero "Getting started") and is the site's only conversion page. | Read `docs/sitemap.xml` from `main` on GitHub raw; count and contents confirmed. Live `/sitemap.xml` returns `Content-Type: application/xml` and GSC reports "8 pages discovered", which matches. |
| S2 | **`/section-install.html` has no SEO head whatsoever.** Its entire head is `<title>Install \| sciClaw</title>` + `<meta name="viewport">`. **No canonical. No meta description. No OG tags. No Twitter card. No `robots` meta.** | Direct fetch of `https://sciclaw.dev/section-install.html`. Compare `/biography.html`, which has the full set. |
| S3 | **`/section-install.html` has no `<h1>`.** The first heading on the page is an `<h2>` ("Get started in four steps"). Fails pre-flight check #7. | Same fetch — heading hierarchy starts at h2. |
| S4 | **Unknown paths serve the homepage, not a 404 page.** `/zzz-nope-test-123` returned the full homepage body with `<link rel="canonical" href="https://sciclaw.dev/">`. This is the D3 soft-404 pattern *if* the status code is 200. **The HTTP status code is unverified — our fetch tool does not expose it. Engineer to confirm with `curl -I` (check 1 below).** If Cloudflare Pages is returning `404` with `index.html` as the error document, the impact is small but the page should still be a real 404 page rather than a clone of `/` carrying a canonical to `/`. | Fetch of `https://sciclaw.dev/zzz-nope-test-123` returned homepage markup + homepage canonical. |
| S5 | **Every `.html` URL 301s to its extensionless form, but the sitemap, the canonicals and all internal nav links use `.html`.** Observed redirects: `/blog.html → /blog`, `/biography.html → /biography`, `/section-install.html → /section-install`. Meanwhile `/blog.html` serves `<link rel="canonical" href="https://sciclaw.dev/blog.html">` — i.e. the canonical points at a URL that redirects. Net effect: the 8 sitemap entries are all redirect hops, so GSC will report them as "Page with redirect" and index the extensionless variants instead. **The redirect was inferred from the fetcher reporting a URL change; status codes unverified — engineer to confirm (check 2).** | Fetches of `/blog.html`, `/biography.html`, `/section-install.html` each resolved to the extensionless URL. `/robots.txt` and `/zzz-nope-test-123` did **not** redirect, so the reported URL changes are real redirects and not a tool artefact. |
| S6 | **Homepage `<title>` ≠ `og:title`.** `<title>` = "sciClaw \| Paired-Scientist Research Agent"; `og:title` = "sciClaw — Your Paired Scientist". *(Note: the crawl notes wrote this as "Your Paired-Scientist" with a hyphen — the live value has no hyphen.)* Harmless, but two different names for the same page is avoidable brand noise. Also: the nav brand link on `/` is `href="#"` rather than `/`. | Direct fetch of `https://sciclaw.dev/zzz-nope-test-123`, which serves the homepage document. |
| S7 | **Blog posts do not have their own URLs — all ~10 posts are inline on one page.** `/blog.html` renders every post (2026-07-17 back to 2026-04-14) as sections on a single URL, with no links to per-post permalinks. There is therefore **nothing to add to the sitemap** for the blog. Separately, `/blog.html`'s head is missing OG and Twitter tags (it has canonical + description + title only). | Direct fetch of `https://sciclaw.dev/blog.html` — full body read, no post permalinks present. |

**What's already right** (re-confirmed, do not touch): `robots.txt` returns `text/plain` with a correct
`Sitemap:` line; `sitemap.xml` returns `application/xml`; `/` and `/biography.html` have complete
canonical + description + OG + Twitter + `robots: index, follow`; the site is static server-rendered
HTML with a full crawler-visible body pre-JS (pre-flight #7 passes everywhere except S3).

---

## Fixes

### 1. Add the two missing pages to `docs/sitemap.xml` (S1)
```xml
  <url>
    <loc>https://sciclaw.dev/section-install</loc>
    <changefreq>monthly</changefreq>
    <priority>0.9</priority>
  </url>
  <url>
    <loc>https://sciclaw.dev/biography</loc>
    <changefreq>monthly</changefreq>
    <priority>0.6</priority>
  </url>
```
`priority 0.9` on the install page is deliberate — it is the conversion page and currently outranked
in our own sitemap by `/changelog.html`.

While in the file: **no entry has a `<lastmod>`.** Add one to each entry (build-time date is fine).
It costs nothing and gives Google a recrawl signal.

### 2. Give `/section-install.html` a real head and an `<h1>` (S2, S3) — *do this first*
This is a nav-linked, twice-CTA'd page that is currently invisible to social sharing and has no
canonical. Match the pattern already used on `/biography.html`:

- `<link rel="canonical">` — self-referencing, on the resolved URL (see decision in fix 4)
- `<meta name="description">` ≤155 chars. Suggested: *"Install sciClaw in four steps: one Homebrew
  command, workspace setup, provider login, and connect Telegram or Discord. macOS and Linux, no
  coding required."* (172 chars — trim to ≤155 before shipping)
- `<meta name="robots" content="index, follow">`
- Full OG set (`og:title`, `og:description`, `og:url`, `og:image`, `og:type=website`) + `twitter:card`,
  reusing `https://sciclaw.dev/og-image.jpg` and `@cbrainlab` as the homepage does
- Promote the existing "Get started in four steps" `<h2>` to the page's single `<h1>`, or add an `<h1>`
  above it (e.g. "Install sciClaw"). Exactly one `<h1>` on the page.

Also add OG + Twitter tags to `/blog.html` (S7), same reasoning.

### 3. Make unknown paths a real 404 (S4)
Add `docs/404.html` — a short branded page with `<meta name="robots" content="noindex">`, a link home,
and **no canonical to `/`**. Cloudflare Pages picks up `404.html` automatically and serves it with a
real 404 status. Confirm the status code afterwards; if it is currently 200, this is a D3-class defect
and should be treated as P1 rather than P2.

### 4. Pick one URL form and use it everywhere (S5)
Cloudflare Pages' default "strip `.html`" behaviour means the site has two URL forms for every page and
we are currently advertising the wrong one in three places at once. **Decide once**, then apply to all
three:

**Recommended: extensionless.** It's what the redirect already lands on, so it's what Google will index
anyway.
1. `sitemap.xml` → all `<loc>` values extensionless (`https://sciclaw.dev/docs`, `/blog`, …). Keep
   `/sciclaw-open.pdf` as-is; it's a real file.
2. Every `<link rel="canonical">` → extensionless self-reference.
3. Every internal `<a href>` → extensionless (nav, hero CTAs, footer, cross-page links). Today they
   all point at `.html`, so every internal click is a redirect hop.

If instead you prefer to keep `.html` as canonical, you must **disable** the Pages redirect, not just
change the links — otherwise the canonical keeps pointing at a redirect.

**Do not skip this in favour of only fixing the sitemap.** Adding `/biography.html` and
`/section-install.html` to the sitemap without resolving S5 just adds two more "Page with redirect"
rows in GSC.

### 5. Cosmetic (S6)
Align `og:title` to the `<title>` on `/` (pick one name — "sciClaw — Your Paired Scientist" reads better
as a social card; "sciClaw | Paired-Scientist Research Agent" is the better `<title>` for search, so the
honest fix is to make `og:title` match `<title>`). Change the nav brand `href="#"` to `href="/"`.

### 6. Out of scope for this task
Splitting the 10 blog posts onto individual URLs (S7) is a real opportunity — 10 dated posts competing
inside one URL is the same pillar-vs-spoke problem as automacy.org — but it is a content/IA decision,
not a fix. Flag it back to Ernie; do not build it here.

---

## Acceptance checks — paste output in the PR

```bash
# 1. unknown path is a true 404 (this is the one we could not verify — run it first)
curl -sI https://sciclaw.dev/zzz-nope-test-123 | head -1          # EXPECT: HTTP/2 404
curl -s https://sciclaw.dev/zzz-nope-test-123 | grep -c 'rel="canonical" href="https://sciclaw.dev/"'
# EXPECT: 0  (the 404 page must not canonicalize to the homepage)

# 2. no sitemap URL is a redirect, and all return 200
for u in $(curl -s https://sciclaw.dev/sitemap.xml | grep -oE 'https://sciclaw\.dev[^<]*'); do
  printf '%s  %-55s  %s\n' \
    "$(curl -s -o /dev/null -w '%{http_code}' "$u")" "$u" \
    "$(curl -s -o /dev/null -w '%{redirect_url}' "$u")"
done
# EXPECT: every line starts 200 and has an EMPTY third column

# 3. sitemap grew by exactly the two pages, and is still real XML
curl -sI https://sciclaw.dev/sitemap.xml | grep -i content-type    # EXPECT: application/xml
curl -s  https://sciclaw.dev/sitemap.xml | grep -c '<loc>'         # EXPECT: 10  (was 8)
curl -s  https://sciclaw.dev/sitemap.xml | grep -E 'biography|section-install'   # EXPECT: 2 lines
curl -s  https://sciclaw.dev/sitemap.xml | grep -c '<lastmod>'     # EXPECT: 10
curl -s  https://sciclaw.dev/sitemap.xml | grep -c 'sciclaw\.dev'  # EXPECT: == <loc> count (no foreign domains)

# 4. the install page now has a full head and exactly one h1
curl -s https://sciclaw.dev/section-install | grep -Eo \
  '<title>[^<]*|rel="canonical"[^>]*|og:title|og:description|og:image|og:url|name="description"|name="robots"|twitter:card'
# EXPECT: title, self-canonical, all four og:*, description, robots, twitter:card
curl -s https://sciclaw.dev/section-install | grep -c '<h1'        # EXPECT: 1

# 5. blog page has OG tags
curl -s https://sciclaw.dev/blog | grep -Eo 'og:title|og:image|twitter:card'   # EXPECT: 3 lines

# 6. internal links use the canonical URL form (no .html hops left)
curl -s https://sciclaw.dev/ | grep -c 'href="https://sciclaw.dev/[a-z-]*\.html"'   # EXPECT: 0
curl -s https://sciclaw.dev/ | grep -c 'href="#"'                  # EXPECT: 0

# 7. title/og:title aligned on the homepage
curl -s https://sciclaw.dev/ | grep -Eo '<title>[^<]*|og:title" content="[^"]*'

# 8. unchanged guarantees — re-confirm nothing regressed
curl -sI https://sciclaw.dev/robots.txt | grep -i content-type     # EXPECT: text/plain
curl -s  https://sciclaw.dev/robots.txt | grep -i sitemap          # EXPECT: Sitemap: https://sciclaw.dev/sitemap.xml
```

## Definition of done
- All 8 checks above pass, output pasted in the PR.
- Check 1 answered definitively: we know the real status code of an unknown path, and it is 404.
- One URL form (extensionless vs `.html`) chosen and applied consistently across sitemap, canonicals
  and internal links — stated explicitly in the PR description.
- `/section-install` passes the full 10-point pre-flight on its own (it currently fails #5, #6, #7, #8).
- **Then tell Ernie** — the sitemap needs re-submitting in Search Console so the two new URLs get picked
  up, and if S5 changed the URL form, the old 8 entries need re-processing too.

## Reference
- `dev-seo-spec.md` — the D1–D7 defect classes and the shared SEO contract. S4 is a D3 candidate; S2 is
  a D1/D4 relative (missing rather than wrong).
- `MASTER-SEO-CHECKLIST.html` → **"Standing pre-flight — 10 checks"**. sciclaw.dev currently passes
  #1–#4 (pending S4 confirmation) and #9–#10 site-wide, but `/section-install.html` fails #5, #6, #7
  and #8. A site isn't done until all 10 pass **on every route**.
- `CRAWL-automacy-and-sciclaw.md` — the 2026-07-25 crawl. Note the correction at the top of this doc.
- `KEYWORD-PASS-sciclaw-and-automacy.html` — why this site matters: the "AI research agent" framing is a
  dead end (170/mo), but the *literature-review-tool* and *Elicit/SciSpace alternative* territory is
  live. Content work for that is a separate task; this doc is plumbing only.
