---
name: pubmed-database
description: Query PubMed with reproducible search terms and collect citation metadata for literature review workflows.
---

# PubMed Database

Use this skill for biomedical literature retrieval and structured citation collection.

## When to use

- "search PubMed"
- "find papers on X"
- "collect PMID list"
- "build literature table"

## Quick commands

```bash
# Search PMIDs
curl -s "https://eutils.ncbi.nlm.nih.gov/entrez/eutils/esearch.fcgi?db=pubmed&retmode=json&term=autism+digital+phenotyping"

# Fetch citation metadata for PMIDs (comma-separated)
curl -s "https://eutils.ncbi.nlm.nih.gov/entrez/eutils/esummary.fcgi?db=pubmed&retmode=json&id=12345,67890"
```

## Workflow

1. Keep search terms explicit and versioned in notes/logs.
2. Record query date and exact query string.
3. Capture PMID, title, journal, year, DOI, and abstract availability.
4. Mark review decisions: include, exclude, or uncertain.
