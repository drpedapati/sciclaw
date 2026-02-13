---
name: biorxiv-database
description: Search and summarize preprints from bioRxiv/medRxiv with explicit date windows and traceable query logs.
---

# bioRxiv Database

Use this skill for preprint discovery and monitoring.

## When to use

- "search bioRxiv"
- "latest preprints on X"
- "track medRxiv updates"

## Quick commands

```bash
# Date-range query (YYYY-MM-DD)
curl -s "https://api.biorxiv.org/details/biorxiv/2025-01-01/2025-12-31/0"

# medRxiv date-range query
curl -s "https://api.biorxiv.org/details/medrxiv/2025-01-01/2025-12-31/0"
```

## Workflow

1. Use explicit date windows for reproducibility.
2. Filter by topic keywords in title/abstract.
3. Track DOI, server, posted date, version, and URL.
4. Flag preprints as non-peer-reviewed in manuscript text.
