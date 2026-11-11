# Deep crawl — automacy.org + sciclaw.dev

**Date:** 2026-07-25 · Crawled live (rendered, not just raw HTML) · Both sites now in Search Console
under `atomicqms@gmail.com` alongside the 6 Figs properties.

---

## automacy.org — you were right, there IS more content. It's just all on one URL.

**The finding:** the homepage is not a landing page — it's a **complete 15-section guide**, and
every section lives on `/`. The nav items (Concept, Install, Navigation, Reference) are
**in-page anchors, not pages.** That's why the sitemap only has 3 URLs.

### Actual indexable URLs (3)
| URL | Status |
|---|---|
| `/` | The entire 15-section guide |
| `/blog` | Real page, own title/canonical/description |
| `/blog/2025-11-10-generate-image-feature` | One post |

Also present: `/cheatsheet-builder` (**noindex — intentional**, leave it), `/reference` (**empty —
returns nothing**; if it's linked in nav it should either become a real page or stop being linked).

### The 15 sections currently buried on `/`
1. Concept · 2. Install the Essentials · 3. Configure Launch Hotkey · 4. Your First Automacy
Shortcut · 5. Adding Additional Applications · 6. Office Productivity · 7. System Applications ·
8. Creating Web Apps · 9. Web Browsing Setup · 10. Screen Capture Setup · 11. Voice Dictation and
Audio Setup · 12. Window Focus Indicators · 13. Useful Toggles · 14. Folder Shortcuts ·
15. Ghostty and Terminal Integration

Each is substantial (WHY + HOW + code blocks + shortcut tables) — genuinely page-worthy content,
covering named tools with their own search demand: **Raycast, JankyBorders, AltTab, TopIt, Handy,
Cap, Ghostty, btop, Hyper Key, Whisper/Parakeet, OrbStack, Positron**.

### Why this matters
Google ranks **pages**, not sections. One URL can realistically win one primary keyword — so 15
sections of good content are competing with each other inside a single page instead of each
owning its own query. **This is the single biggest SEO opportunity on the site.**

### Recommendation (the fix)
Split the guide into a page per section under a `/guide/` namespace, keeping `/` as a hub that
links to all of them (classic pillar-and-spoke). Example targets:
- `/guide/hyper-key-setup` → "raycast hyper key", "caps lock hyper key macos"
- `/guide/window-focus-indicator` → "jankyborders", "macos window border focus"
- `/guide/voice-dictation` → "macos local dictation", "whisper dictation mac"
- `/guide/screen-capture` → "macos screenshot shortcuts", "cleanshot alternative"
- `/guide/ghostty-terminal` → "ghostty setup", "btop macos"
- …one per section.

**Do NOT** just duplicate the text on both `/` and `/guide/*` — move it, and leave a short summary
+ link on the hub, or you create duplicate-content competition.

**Next step:** run an OpenSEO keyword pass on these tool names to confirm volume/difficulty before
committing to the split (same method used for MedicalFigs/QMSFigs).

---

## sciclaw.dev — technically in good shape

**sciClaw** — free, open-source "paired scientist" research agent (PubMed search, docx tracked
changes, reproducibility/audit trail). MIT, Go, runs locally, BYO model.

### What's already right ✅
- **Static, server-rendered HTML** — fully crawlable, no client-render problem (unlike automacy)
- Clean per-page `<head>`: canonical, meta description, keywords, author, full OG + Twitter cards,
  `robots: index, follow`
- `robots.txt` correct, with Sitemap line
- `sitemap.xml` valid, 8 URLs
- Genuinely good content: FAQ section, tool descriptions, use-case ladder, preprint PDF

### Gaps found ⚠
1. **Two nav-linked pages are missing from the sitemap:**
   - `/biography.html` (linked as "Creator")
   - `/section-install.html` (linked as "Install" *and* "Getting started" — a primary CTA!)
   → Add both to `sitemap.xml`. The install page especially: it's a conversion page that Google
   may under-prioritize without a sitemap entry.
2. **No blog posts crawled yet** — `/blog.html` exists; if posts live at sub-URLs they should be in
   the sitemap too.
3. **Homepage `<title>` vs `og:title` mismatch** — title is "sciClaw | Paired-Scientist Research
   Agent", og:title is "sciClaw — Your Paired Scientist". Harmless but worth aligning.

### Done today ✅
- **sciclaw.dev added + verified in Search Console** (Domain property, Cloudflare DNS auto-verify).
  TXT: `google-site-verification=7bLFX4E3KAzL1QynhZl3qN1lYV67DUPkT3y6A7Ag6ZI` — **don't delete it.**
- **Sitemap submitted → Status: Success, 8 pages discovered.** (Cleaner result than automacy's,
  which is still "Couldn't fetch" pending Google's first read.)

### Keyword angle (not yet researched — needs an OpenSEO pass)
Likely low-competition, high-intent territory: "AI research agent", "PubMed CLI", "literature
review AI", "tracked changes AI", "local AI research assistant", "reproducible research agent",
plus challenger angles vs Elicit / Consensus / SciSpace / Scite.

---

## Search Console — full portfolio status
All under `atomicqms@gmail.com`:

| Property | Verified | Sitemap |
|---|---|---|
| sciencefigs.com | ✅ | ✅ |
| medicalfigs.com | ✅ | ✅ re-submitted 7/22 |
| qmsfigs.com | ✅ | ✅ re-submitted 7/22 |
| teachfigs.com | ✅ | — check |
| atomicqms.com | ✅ | — check |
| figshub.com | ✅ new 7/25 | — **still to submit** |
| automacy.org | ✅ new 7/25 | ✅ submitted (pending first read) |
| sciclaw.dev | ✅ new 7/25 | ✅ Success, 8 pages |

**Open items:** submit figshub.com sitemap; verify teachfigs/atomicqms sitemaps; OpenSEO rank
tracking still pending (Cloudflare Access login codes kept expiring — easier via the Claude Code
CLI, where the OpenSEO MCP is already registered).
