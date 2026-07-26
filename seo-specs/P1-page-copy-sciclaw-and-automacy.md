# P1 page copy — sciClaw + Automacy (ready for build)

**Date:** 2026-07-25. Two pages, drafted from the OpenSEO keyword pass. Competitor facts verified
live where stated. **Re-verify pricing at publish time — it drifts.**

---

# PAGE 1 — sciClaw: "Free Elicit Alternative"

- **URL:** `/elicit-alternative.html` (matches existing flat `.html` structure)
- **Primary kw:** elicit ai (6,600/mo, diff 15) · **Secondary:** elicit ai tool (320, diff 6),
  elicit ai research assistant (590, diff 6), elicit literature review (140, diff 5),
  best ai for literature review (210, diff 21)
- **Title (≤60):** `Free Elicit Alternative — Local, Open-Source | sciClaw`
- **Meta (≤155):** `Looking for an Elicit alternative? sciClaw is a free, open-source research agent that runs on your machine — literature search, drafting, and an audit trail. No cloud.`
- **H1:** `The free, open-source Elicit alternative that runs on your machine`
- Canonical self · `index, follow` · FAQPage + SoftwareApplication JSON-LD

## Hero
> **H1:** The free, open-source Elicit alternative that runs on your machine
>
> **Sub:** sciClaw is a paired-scientist agent for literature search, manuscript drafting, and
> document review — MIT licensed, running locally with your own API key. Your papers, notes, and
> data never leave your machine, and every step lands in a folder you can open, version, and audit.
>
> **CTA:** `brew tap drpedapati/tap && brew install sciclaw` (copy button) · secondary: `Read the docs`
> **Trust line:** Free & open source (MIT) · Runs locally · Bring your own model

## Why researchers look for an Elicit alternative (3 cards)
- **Cost at scale** — Elicit's free Basic tier gives unlimited search/summaries/chat but only
  *limited* usage of Research Agent and Research Reports, and **export (RIS/CSV/BIB/PDF/DOCX) is a
  paid feature**. sciClaw is free at every tier; you pay only your own model provider (typically a
  few dollars a month).
- **Where your data lives** — Elicit is a hosted cloud service; **"no training on your data by
  default" is listed as an Enterprise-tier feature**. sciClaw runs on your machine: files stay
  local, there's no sciClaw account, no telemetry.
- **Beyond search** — Elicit is built around screening and extracting from papers. sciClaw also
  **writes and runs code, produces .docx with real tracked changes, and logs a full audit trail**
  for reproducibility.

## Comparison table (crawlable HTML, not an image)

| | sciClaw | Elicit |
|---|---|---|
| Price | **Free, MIT open source** (you pay your own model API) | Free Basic tier; paid from **$11/user/mo academic** (billed $132/yr) or **$49/user/mo Pro (industry)**; Scale $89–$169/mo |
| Where it runs | **Your machine** — local files, no vendor account | Hosted cloud |
| Data handling | Files never leave your device | Cloud-processed; *"no training on your data by default"* is an **Enterprise** feature |
| Paper corpus | PubMed / NCBI E-Utilities (+ your own PDFs) | 138M+ papers, 500k+ clinical trials |
| Export | Local files by default (.docx, .csv, RIS) | RIS/CSV/BIB/PDF/DOCX **on paid plans** |
| Systematic review | Scriptable via protocols/skills | Dedicated workflow; screens 5,000 papers (Pro) |
| Writing & review | **Tracked changes in Word, margin comments, semantic diff** | Not a manuscript-revision tool |
| Runs code / stats | **Yes — R, Python, Quarto with provenance** | No |
| PHI / clinical use | **PHI mode** | Enterprise security controls |
| Interface | Discord / Telegram / CLI | Web app |
| Best for | Researchers who want local control, reproducibility, and drafting | Teams doing large-scale screening in the cloud |

**Honest framing (keep it):** *Elicit is excellent at what it does — large-scale paper screening
and structured extraction across a huge corpus. If that's your core need and cloud is fine, it's a
strong tool. sciClaw is for researchers who want the work to happen on their own machine, with an
auditable trail and the ability to draft and run analyses, for free.*

## "Choose sciClaw if / Choose Elicit if"
- **sciClaw if:** you handle sensitive or clinical data, you want reproducibility and provenance,
  you want manuscript drafting + tracked changes, you'd rather not pay per seat, or you want to
  fork and extend the tooling.
- **Elicit if:** your main job is screening thousands of papers with a polished web UI and a
  managed 138M-paper index, and you want it to just work without local setup.

## FAQ (FAQPage schema)
- **Is sciClaw really free?** Yes — MIT licensed, no account, no telemetry. You pay your AI
  provider directly (most researchers spend a few dollars/month).
- **Does my data go to the cloud?** Your files stay on your machine. Only your prompt/conversation
  goes to whichever model provider you choose, using your own key.
- **Can it do systematic reviews?** It can run multi-step, logged protocols and PubMed queries.
  For very large-scale screening with a dedicated managed workflow, Elicit Pro is purpose-built.
- **Can I use it with PHI?** sciClaw has a PHI mode — see /phi-mode.html. You remain responsible
  for your institution's compliance requirements.
- **What do I need?** Mac or Linux (Windows via WSL), one Homebrew command, and an AI provider account.

## CTA
> **Install in one line. Keep your data.** `brew tap drpedapati/tap && brew install sciclaw`

## Internal links (required)
→ `/docs.html` · `/phi-mode.html` · `/philosophy.html` · `/security.html` · `/section-install.html`
· `/` — and link **from** the homepage nav/footer to this page.

---

# PAGE 2 — Automacy: "Mac Keyboard Shortcuts" (+ cheatsheet landing)

**This is the biggest cheap win in the whole portfolio:** `keyboard shortcuts mac` = **9,900/mo at
difficulty 10**, and `macbook shortcuts pdf` = 390/mo at difficulty 9 — which is *literally what
your Cheat Sheet Builder outputs*, currently sitting behind `noindex`.

- **URL:** `/mac-keyboard-shortcuts`
- **Primary kw:** keyboard shortcuts mac (9,900, diff 10) · **Secondary:** macbook shortcuts pdf
  (390, diff 9), mac shortcuts list, hyper key mac
- **Title:** `Mac Keyboard Shortcuts: The Complete List (+ Free PDF)`
- **Meta:** `Every essential macOS keyboard shortcut in one place — system, window, text, and screenshot shortcuts. Plus build your own printable cheatsheet PDF, free.`
- **H1:** `Mac keyboard shortcuts: the complete reference`
- Canonical self · `index, follow` · FAQPage JSON-LD

### Why this page wins
Searchers want a **reference they can scan or print**. Most results are thin listicles. You can beat
them by: (a) being genuinely complete and well-organised, (b) offering the **printable PDF** they
actually want, and (c) linking into the deeper Automacy guide for people who want the full setup.

### Structure
1. **Intro (2–3 sentences)** — what's here, and the free printable cheatsheet.
2. **Early CTA** → "Build your own printable cheatsheet" → cheatsheet builder.
3. **Shortcut tables**, grouped with real `<table>` markup (crawlable, and eligible for rich results):
   - Essential system (⌘Space, ⌘Tab, ⌘Q, ⌘W, ⌘,)
   - Window management (snapping, fullscreen, Mission Control)
   - Text editing & navigation
   - Screenshots & screen capture (⇧⌘3/4/5 + the Automacy remaps)
   - Finder & file management
   - Browser/tabs
   - **Hyper Key shortcuts** ← bridges into Automacy's actual product
4. **"Go further: the Hyper Key"** — short section explaining Caps Lock → Hyper, with a link to the
   guide. This is the funnel from a generic reference into your opinionated setup.
5. **FAQ** — How do I print these? / What is the Hyper Key? / Can I customise shortcuts? / Where's
   the PDF?
6. **CTA** → cheatsheet builder + the Automacy setup guide.

### ⚠ The `noindex` fix (do this first — it's the fastest win)
`/cheatsheet-builder` currently emits `<meta name="robots" content="noindex">`. Recommended:
- Create an **indexable landing page** for it — e.g. `/mac-shortcuts-cheatsheet` — with real copy
  targeting `macbook shortcuts pdf` / `mac shortcuts cheat sheet`, a description of what the builder
  does, an example output image, and a CTA into the tool.
- **Keep the interactive builder itself `noindex`** if you prefer (thin/app-like UI) — that's fine.
  The landing page is what ranks.
- Add both new pages to `sitemap.xml`.

### Also fix while you're in there
- **`/reference` returns nothing** — either build it or remove it from the nav (dead link).
- Nav items Concept/Install/Navigation are in-page anchors; consider real `/guide/*` URLs per the
  crawl doc, prioritising sections with actual demand (tiling, borders, capture, dictation).

---

## Build notes (both pages)
- Unique title/meta/canonical, one `<h1>`, FAQPage JSON-LD, OG + Twitter tags, sitemap entries.
- All tables as real HTML. All comparison claims factual and dated.
- Reciprocal internal links (homepage → new page → related pages).
- **Competitor-claim caveat:** every Elicit figure above was verified at elicit.com/pricing on
  2026-07-25 (Academic: Basic free, Plus $11/mo billed $132/yr, Pro $39/mo, Scale $89/mo; Industry:
  Pro $49/mo, Scale $169/mo). Add a visible "verified [date]" line on the page and re-check before
  publish. Do not state anything about Elicit not covered above.
