---
title: Concepts
nav_order: 4
permalink: /concepts/
---

# Concepts
{: .no_toc }

The handful of nouns gitcrawl uses, and how they connect.
{: .fs-6 .fw-300 }

1. TOC
{:toc}

## Repository mirror

A **repository** is the `owner/repo` you sync. Every gitcrawl command takes one, and most state in SQLite is keyed by it. You can mirror as many repos as you like into a single `gitcrawl.db`; commands always scope to the one you name.

The mirror is metadata-first: titles, bodies, authors, labels, state, timestamps, and IDs land in SQLite immediately. Comments, reviews, review comments, and full PR detail (files, commits, checks, workflow runs) are opt-in on a per-sync basis (see [Sync](/sync/)).

## Thread

A **thread** is a single GitHub issue or pull request, with its body and metadata. The CLI exposes threads via `gitcrawl threads` and via the `gh` shim's `gh issue/pr view` and `gh issue/pr list` paths.

Threads have two state dimensions:

- **GitHub state** — `open` or `closed` upstream.
- **Local close** — a maintainer-only override stored locally. `gitcrawl close-thread` and `reopen-thread` flip this without touching GitHub. Local closes drive the `--hide-closed` and `--include-closed` filters across `clusters`, `cluster-detail`, the TUI, and search.

Local close is for triage workflow: "I have handled this duplicate locally, I do not need it shown next time." It does not write back to GitHub.

## Document

A **document** is the canonical text gitcrawl indexes for a thread — title plus body, with comments folded in when present. Documents back the FTS index used by `gitcrawl search` and feed the embedding pipeline.

Most users never interact with documents directly; they show up in JSON output as a `document` field on neighbors and search hits.

## Embedding

An **embedding** is a vector representation of a thread's document, produced by an OpenAI model (default `text-embedding-3-small`, 1024 dimensions). Vectors live in `~/.config/gitcrawl/vectors` and are referenced from the `thread_vectors` table.

The **embedding basis** controls what text gets embedded. The default `title_original` uses title plus an excerpt of the original body. This is configurable via `gitcrawl configure --embedding-basis ...` but only `title_original` is currently implemented.

`gitcrawl embed` is the explicit command that fills the vector table. `gitcrawl refresh` runs it automatically as part of its sync → embed → cluster pipeline.

When the embedding input rune cap or model changes, vectors are rebuilt to avoid stale comparisons.

## Cluster

A **cluster** is a group of related threads inferred from vector similarity, with deterministic GitHub reference evidence (`#123`, `pull/123`, `issues/123`) folded in to harden weak edges.

Clustering is run by `gitcrawl cluster` (or as part of `gitcrawl refresh`). Defaults are tuned to ghcrawl's profile: `--threshold 0.80`, `--min-size 1`, `--max-cluster-size 40`, `--k 16` nearest-neighbor fanout, `--cross-kind-threshold 0.93` for issue↔PR edges.

Two safeguards keep mega-clusters from forming:

- **Title-token overlap.** A weak embedding edge needs concrete shared title tokens unless its similarity is already high or there is direct GitHub reference evidence.
- **Cross-kind pruning.** Issue↔PR edges need a higher similarity floor (`--cross-kind-threshold`) than issue↔issue or PR↔PR.

### Cluster kinds

Every cluster ships with a kind that explains its shape:

- `singleton_orphan` — one member, no neighbors above threshold. Useful for surfacing isolated reports.
- `duplicate_candidate` — multiple members above the merge threshold. The default duplicate triage row.

### Durable clusters

A **durable cluster** is a stable, long-lived row in `durable_clusters` with a stable ID derived from its representative thread. Durable cluster IDs survive re-runs of `cluster` and `refresh`, so the local close, exclusion, and canonical-member overrides you apply persist across re-clustering.

`gitcrawl clusters` and `gitcrawl tui` show the latest raw run's clusters first, with closed durable rows merged in as historical context. Use `gitcrawl durable-clusters` for an audit view that stays on the durable rows.

### Cluster overrides (governance)

Per-cluster maintainer overrides let you correct what the algorithm produced without re-tuning thresholds:

- **Local close** (`close-cluster`/`reopen-cluster`) — hides a duplicate-candidate from active triage.
- **Member exclusion** (`exclude-cluster-member`/`include-cluster-member`) — pulls a specific thread out of a cluster and remembers why.
- **Canonical member** (`set-cluster-canonical`) — pins which thread represents the cluster.

See [Governance](/governance/) for the full workflow.

## Run

Every sync, embed, and cluster operation records a **run** in `run_records` with start/finish timestamps, status, and stage-specific stats. `gitcrawl runs --kind sync|embedding|cluster` lists them, useful for debugging or auditing.

## Portable store

A **portable store** is a Git-backed publish target for a `gitcrawl.db` plus its derived bodies, designed for sharing a local cache across agents or machines without a hosted service.

`gitcrawl init --portable-store https://github.com/org/repo` clones a portable store into `~/.config/gitcrawl/portable/`, points the runtime at it, and `gitcrawl portable prune --body-chars 256` keeps the published payload small while retaining comments, PR details, checks, and workflow runs. Read-only commands run against portable stores refresh the checkout before reading. See [Portable stores](/portable-stores/).

## Cache

The `cache/` directory under `~/.config/gitcrawl/` holds:

- `cache/gh-shim/` — the short-lived fallthrough cache for the `gh` shim, keyed by config path, CWD, `GH_HOST`, `GH_REPO`, and command args. Inspect or clean it with `gitcrawl gh xcache stats|keys|gc|flush`.
- `cache/pr/` — hydrated PR detail blobs used to answer `gh pr view`, `gh pr checks`, and `gh run` reads from local SQLite.

See [gh shim](/gh-shim/) for the cache key composition and TTL behavior.
