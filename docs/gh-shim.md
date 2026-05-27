---
title: gh shim
nav_order: 12
permalink: /gh-shim/
---

# gh shim
{: .no_toc }

`gitcrawl gh` moved to Octopool.
{: .fs-6 .fw-300 }

Gitcrawl no longer owns a GitHub CLI compatibility cache. Gitcrawl remains the local per-repo mirror, portable store, search, clustering, and triage tool. Octopool owns the org-authenticated shared GitHub cache and pooled read relay.

## Migrate

```bash
octopool login
octopool gh api repos/openclaw/openclaw/pulls/85341
```

To make existing `gh api ...` callers use Octopool, install or symlink the Octopool binary as `gh` or `octopool-gh`.

Unsupported or mutating commands fall through to the real GitHub CLI from Octopool. `gitcrawl gh ...` now fails with a migration note instead of maintaining a second cache.

## Still in gitcrawl

- `gitcrawl search issues|prs ...` keeps answering from the local SQLite mirror.
- `gitcrawl sync ... --with pr-details` keeps hydrating repo-local PR detail used by search, clustering, and TUI workflows.
- portable stores remain Git-backed repo snapshots; they do not carry the runtime `gh` command cache.
