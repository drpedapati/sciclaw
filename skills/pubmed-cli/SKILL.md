---
name: pubmed-cli
description: "Search PubMed, fetch article metadata, traverse citation graphs, and look up MeSH terms from the command line. Use when: (1) Searching PubMed with Boolean/MeSH queries, (2) Fetching article details by PMID (abstract, authors, DOI, MeSH terms), (3) Finding papers that cite a given article (cited-by), (4) Finding papers cited by a given article (references), (5) Finding related articles with relevance scores, (6) Looking up MeSH vocabulary (tree numbers, scope notes), (7) Exporting citations in RIS format for Zotero/EndNote, (8) Building reproducible literature review workflows."
metadata: {"nanobot":{"emoji":"🔬","requires":{"bins":["pubmed"]},"install":[{"id":"brew","kind":"brew","formula":"sciclaw-pubmed-cli","bins":["pubmed"],"label":"Install pubmed-cli (brew)"}]}}
---

# pubmed-cli

PubMed from your terminal. Search, fetch, cite, traverse — built for humans and AI agents. Zero dependencies, structured JSON, agent-ready.

## Install

```bash
brew tap drpedapati/tap
brew install sciclaw-pubmed-cli
```

Binary: `/opt/homebrew/bin/pubmed`

Verify: `pubmed --help`

## Configuration

Set your NCBI API key for higher rate limits (recommended):

```bash
export NCBI_API_KEY="your-key"
```

- Without key: 3 requests/second
- With key: 10 requests/second
- Get a key at: https://www.ncbi.nlm.nih.gov/account/settings/

## Commands

### search — Query PubMed

```bash
pubmed search "fragile x syndrome" --json --limit 10
pubmed search "autism AND EEG" --json --limit 20 --sort date
pubmed search "ALS" --json --year 2023-2025 --type review
pubmed search "CRISPR" --json --sort cited --limit 5
```

Returns: `count` (total hits), `ids` (PMIDs), `query_translation` (how NCBI interpreted the query).

Supports: Boolean operators (AND, OR, NOT), MeSH terms (`[MeSH Terms]`), field tags (`[Title/Abstract]`, `[Author]`), wildcards (`neoplas*`), phrase search (`"exact phrase"`).

### fetch — Get article details

```bash
pubmed fetch 38000001 --json
pubmed fetch 38000001 38000002 38000003 --json
pubmed fetch "38000001,38000002" --json
```

Returns per article: `pmid`, `title`, `abstract`, `abstract_sections` (structured), `authors` (with affiliations), `journal`, `volume`, `issue`, `pages`, `year`, `doi`, `pmcid`, `mesh_terms` (with major topic flags), `publication_types`.

### cited-by — Papers that cite this article

```bash
pubmed cited-by 38000001 --json --limit 10
```

Returns: `source_id`, `links` (PMIDs of citing papers).

### references — Papers cited by this article

```bash
pubmed references 38000001 --json --limit 10
```

Returns: `source_id`, `links` (PMIDs of referenced papers).

### related — Similar articles with relevance scores

```bash
pubmed related 38000001 --json --limit 10
```

Returns: `source_id`, `links` (PMIDs with `score` — higher = more similar).

### mesh — MeSH vocabulary lookup

```bash
pubmed mesh "depression" --json
pubmed mesh "autism spectrum disorder" --json
```

Returns: `ui` (MeSH ID), `name`, `scope_note`, `tree_numbers`, `entry_terms` (synonyms), `annotation`.

## Output Formats

| Flag | Format | Use case |
|------|--------|----------|
| `--json` | Structured JSON | Agent parsing, programmatic use |
| `--human` / `-H` | Rich terminal tables | Interactive exploration |
| `--csv FILE` | CSV export | Spreadsheet import, data analysis |
| `--ris FILE` | RIS citations | Zotero, EndNote, Mendeley import |

Always use `--json` for agent workflows. The other formats are for human review and export.

## Search Modifiers

| Flag | Values | Example |
|------|--------|---------|
| `--limit N` | Any positive integer (default 20) | `--limit 50` |
| `--sort` | `relevance`, `date`, `cited` | `--sort cited` |
| `--year` | `YYYY` or `YYYY-YYYY` | `--year 2020-2025` |
| `--type` | `review`, `trial`, `meta-analysis`, `randomized`, `case-report`, or custom | `--type review` |

## Workflow: Systematic Literature Search

```bash
# 1. Search with filters
pubmed search "ALS AND biomarkers" --json --limit 50 --year 2020-2025 --type review > search.json

# 2. Fetch full details for top results
cat search.json | jq -r '.ids[:10] | join(" ")' | xargs pubmed fetch --json > articles.json

# 3. Check what each key paper cites
pubmed references 38000001 --json --limit 20 > refs.json

# 4. Find related work
pubmed related 38000001 --json --limit 20 > related.json

# 5. Export for reference manager
pubmed fetch 38000001 38000002 38000003 --ris bibliography.ris
```

## Workflow: Citation Network Traversal

```bash
# Start with a seed paper
pubmed fetch 38000001 --json

# Forward citations (who cited this?)
pubmed cited-by 38000001 --json --limit 20

# Backward citations (what did this cite?)
pubmed references 38000001 --json --limit 20

# Similar papers (NCBI's relevance algorithm)
pubmed related 38000001 --json --limit 20

# Chain: fetch details of citing papers
pubmed cited-by 38000001 --json | jq -r '.links[].id' | head -5 | xargs pubmed fetch --json
```

## Workflow: MeSH-Guided Search

```bash
# 1. Look up the correct MeSH term
pubmed mesh "fragile x" --json

# 2. Use the official MeSH term in search
pubmed search '"Fragile X Syndrome"[MeSH Terms]' --json --limit 20

# 3. Combine with other terms
pubmed search '"Fragile X Syndrome"[MeSH] AND EEG[Title/Abstract]' --json --year 2020-2025
```

## Exit Codes

- `0` — success
- `1` — user error (invalid flags, bad PMID) or API failure

Error messages are written to stderr with actionable descriptions.

## Rate Limiting

Built-in rate limiter respects NCBI guidelines:
- 3 req/s without API key, 10 req/s with key
- Automatic retry on HTTP 429 with exponential backoff
- Respects `Retry-After` headers
