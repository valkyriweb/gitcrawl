---
title: Search
nav_order: 8
permalink: /search/
---

# Search
{: .no_toc }

Local full-text and semantic search over the SQLite mirror, plus a `gh search`-compatible surface for scripts.
{: .fs-6 .fw-300 }

1. TOC
{:toc}

## Why local search

`gitcrawl search` runs against the local SQLite cache and the local vector store. It does not consume GitHub REST search quota and it returns deterministically ordered hits with full thread metadata. It is intended for **discovery**, not for write actions — use `gh` for the final live verification before commenting, closing, labeling, or merging.

## Direct mode

```bash
gitcrawl search owner/repo --query "download stalls"
gitcrawl search owner/repo --query "manifest cache" --mode hybrid --limit 30 --json
```

| Flag | Default | Description |
| --- | --- | --- |
| `--query <text>` | _(required)_ | Search text |
| `--mode keyword\|semantic\|hybrid` | `keyword` | `keyword` uses SQLite FTS, `semantic` uses vector cosine, `hybrid` blends them |
| `--limit <n>` | _(implementation default)_ | Maximum hits |

**Hybrid mode** is the most robust default — it blends full-text recall with semantic neighbors so typos, synonyms, and stack-trace fragments still surface relevant rows.

JSON output:

```json
{
  "repository": "owner/repo",
  "query": "download stalls",
  "mode": "hybrid",
  "hits": [
    { "number": 123, "kind": "issue", "title": "...", "score": 0.81, "url": "...", "updated_at": "..." }
  ]
}
```

## `gh search` compatibility mode

The same command also accepts the `gh search` shape so scripts that already speak `gh` work without rewriting:

```bash
gitcrawl search issues "download stalls" \
  -R owner/repo \
  --state open \
  --json number,title,state,url,updatedAt,labels \
  --limit 30

gitcrawl search prs "manifest cache" \
  -R owner/repo \
  --state open \
  --json number,title,state,url,updatedAt,isDraft,author \
  --limit 20
```

Recognized flags in this mode:

| Flag | Description |
| --- | --- |
| `-R` / `--repo` | Target repository (also reads `GH_REPO`) |
| `--state open\|closed\|all` | Issue state filter |
| `--json` | Comma-separated field list (gh-compatible) |
| `--limit` / `-L` | Maximum rows |
| `--match` | Accepted for parity; the local FTS index already covers documents |
| `--sort` / `--order` | Accepted for parity |
| `--sync-if-stale <duration>` | Run one metadata sync first if the local mirror is older than the duration |

The output shape matches `gh search issues|prs --json ...` exactly so you can pipe into the same `jq` filters you already have.

## `--sync-if-stale`

```bash
gitcrawl search issues "hot loop" \
  -R owner/repo \
  --state open \
  --sync-if-stale 5m \
  --json number,title,url
```

If the most recent successful sync for this repo is older than `5m`, gitcrawl runs one metadata sync first and then answers the search from the freshly populated cache. The search result still comes from SQLite — only the staleness check triggers GitHub.

This is the right pattern for agents: keep latency predictable on cache hits, and bound the staleness window for everything else.

## Search vs. the `gh` shim

There are two ways to run cached searches:

| Command | Best for |
| --- | --- |
| `gitcrawl search issues|prs ...` | Human use; mixes naturally with the rest of the gitcrawl CLI |
| `gitcrawl gh search issues|prs ...` | Agents and scripts that call `gh` directly — symlinked as `gh` or `gitcrawl-gh` it is invisible to callers |

Both paths share the same local cache and produce gh-shaped JSON. The shim adds the additional `gh issue/pr view`, `gh issue/pr list`, `gh pr checks`, `gh run`, and `xcache` surface — see [gh shim](/gh-shim/).

## Combining with sync

A common discovery pattern:

```bash
# 1. Find candidates locally.
NUMS=$(gitcrawl search issues "download stalls" -R owner/repo \
        --json number --limit 20 \
        | jq -r '[.[].number] | join(",")')

# 2. Hydrate them with comments + PR detail in one round-trip.
gitcrawl sync owner/repo --numbers "$NUMS" --include-comments --with pr-details

# 3. Re-query with full conversational context (or open in TUI).
gitcrawl tui owner/repo
```

## Limits

- The keyword index covers titles, bodies, and (when synced) comments and review comments.
- Semantic search relies on the local vector store. Run `gitcrawl embed` first.
- Hybrid mode degrades gracefully: with no vectors, it behaves like keyword.
- Closed threads are included by the FTS index when synced; locally closed threads are filtered out by the `--hide-closed` flag where applicable.
