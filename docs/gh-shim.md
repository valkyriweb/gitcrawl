---
title: gh shim
nav_order: 12
permalink: /gh-shim/
---

# gh shim
{: .no_toc }

A `gh`-compatible binary that answers from local SQLite first and falls through to the real `gh` for everything else. The fastest way to cut GitHub API load across an agent fleet.
{: .fs-6 .fw-300 }

1. TOC
{:toc}

## What it is

The same `gitcrawl` binary serves a `gh`-compatible mode. Invoked as `gitcrawl gh ...`, or as `gh` / `gitcrawl-gh` via symlink, it intercepts read-only commands and serves them from the local mirror. Anything it cannot serve locally falls through to the real `gh` binary you already have installed, with a short persistent cache layered on top.

The shim never adds GitHub write behavior. Mutating commands (`gh issue close`, `gh pr merge`, `gh api -X POST ...`, `gh label create`, etc.) pass straight through to the real `gh`, increment a write counter, and clear the relevant cache entries on success.

## Install

```bash
# Side-by-side: agents opt in by calling `gitcrawl-gh`.
mkdir -p "$HOME/bin"
ln -sf "$(command -v gitcrawl)" "$HOME/bin/gitcrawl-gh"

# Or replace the global `gh` so every caller picks up the cache automatically.
REAL_GH="$(command -v gh)"              # capture this before shadowing gh
ln -sf "$(command -v gitcrawl)" "$HOME/bin/gh"
export GITCRAWL_GH_PATH="$REAL_GH"      # tell the shim where the real gh is
```

Make sure `~/bin` is on `PATH` before the original `gh` location if you want the shim to be picked up as `gh`. If `GITCRAWL_GH_PATH` is unset, the shim probes common Homebrew paths and then `PATH`. Set it explicitly when you replace the global `gh` so the shim does not recurse into itself.

## Supported local reads

### `gh search issues|prs`

```bash
gh search issues "download stalls" -R owner/repo --state open \
  --match comments --json number,title,url
gh search prs    "manifest cache" -R owner/repo --state open \
  --json number,title,url --limit 20
```

Answered from the local FTS index. Honors `--state`, `--json`, `--limit`. `--match` is accepted for parity (the local index already covers documents). Falls through if an unsupported filter combination is requested.

### `gh issue view` / `gh pr view`

```bash
gh issue view 123 -R owner/repo --json number,title,state,url,body,labels,author
gh pr view  123 -R owner/repo --json number,title,state,url,isDraft,author,headRef,baseRef
```

Supported JSON fields include `number`, `title`, `state`, `url`, `body`, `author`, `createdAt`, `updatedAt`, `closedAt`, `labels`, plus PR-specific `isDraft`, `headRef`, `baseRef`. PR detail fields (`files`, `commits`, `checks`, `statusCheckRollup`) are answered from cached PR detail and trigger [auto-hydration](#auto-hydration) on miss.

### `gh issue list` / `gh pr list`

```bash
gh issue list -R owner/repo --state open --search "hot loop" \
  --author octocat --label bug --label triage --json number,title,url
gh pr list    -R owner/repo --state open --search "manifest cache" \
  --assignee me --json number,title,url
```

Supports `--state`, `--search` (keyword search), `--author`, `--assignee`, repeated `--label`, `--limit`, and `--json`. Falls through for unsupported filters.

### `gh pr checks`

```bash
gh pr checks 123 -R owner/repo --json name,state,conclusion,detailsUrl
```

Returns the cached check/status summary for the PR. If the cached PR detail is older than 90 seconds or its head SHA is stale, [auto-hydration](#auto-hydration) refreshes it before answering. Supported fields: `name`, `state`, `status`, `conclusion`, `detailsUrl`, `workflow`, `startedAt`, `completedAt`.

### `gh run list` / `gh run view`

```bash
gh run list -R owner/repo --branch main --limit 20 \
  --json databaseId,workflowName,status,conclusion
gh run view 123456789 -R owner/repo --json status,conclusion,headSha
```

Workflow runs come from cached PR detail. Filters: `--branch`, `--commit` (head SHA). Supported fields: `databaseId`, `id`, `number`, `workflowName`, `name`, `displayTitle`, `status`, `conclusion`, `url`, `event`, `headBranch`, `headSha`, `createdAt`, `updatedAt`.

## Read-only fallthroughs (cached)

These commands always run real `gh` but the response body is cached for the next caller in the same workspace:

- `gh pr diff` — keyed by the cached PR head SHA when available, so the cache is stable across many sequential agent reads
- `gh issue list/status/view`, `gh pr list/status/view/checks`, and unsupported read-only local shim shapes
- `gh release list/view`, `gh workflow list/view`, `gh secret list`, and `gh variable get/list`
- `gh project list/view/field-list/item-list`, `gh ruleset check/list/view`, `gh gist list/view`, and `gh org list`
- `gh repo view` / `gh repo list`
- `gh search code/commits/issues/prs/repos`
- `gh label list`
- `gh api <GET path>` — only `GET` requests; never cached for `POST`/`PATCH`/`DELETE`/`PUT`

Default cache TTL is short (30 seconds for most reads, 60 seconds for `gh api`, 5 minutes for `gh pr diff` without a stable SHA, 7 days for `gh pr diff` with a stable SHA). Override with `GITCRAWL_GH_CACHE_TTL=5m` or similar.

Repeat read failures are cached by default too. That avoids a fleet of agents all rediscovering the same missing release, workflow, secret, or unsupported field. Set `GITCRAWL_GH_CACHE_ERRORS=0` to cache successful reads only.

## Auto-hydration

When a local PR-detail read misses the cache, the shim can auto-hydrate exactly one PR before falling back:

1. Shim detects missing or stale PR detail (older than 90s, or head SHA mismatch)
2. If `GITCRAWL_GH_AUTO_HYDRATE != 0` (the default), runs `gitcrawl sync --numbers <n> --with pr-details`
3. Retries the local query against the freshly populated cache
4. Falls through to the real `gh` if hydration failed

This keeps `gh pr view`, `gh pr checks`, and `gh run` reads cheap and fresh without manual sync orchestration. Disable with `GITCRAWL_GH_AUTO_HYDRATE=0` if you want the shim to be strictly cache-or-fallthrough.

## Cache inspection: `xcache`

```bash
gitcrawl gh xcache stats        # summary
gitcrawl gh xcache keys         # per-entry detail
gitcrawl gh xcache gc           # remove expired entries + stale lock files
gitcrawl gh xcache flush        # clear everything
```

All accept `--json` for scripting.

`stats` JSON:

```json
{
  "cache_dir": "/Users/me/.config/gitcrawl/cache/gh-shim",
  "entries": 142,
  "expired": 6,
  "locks": 0,
  "bytes": 1841234,
  "counters": {
    "local_hits": 540,
    "fallback_hits": 88,
    "backend_misses": 12,
    "pass_through_writes": 4
  },
  "commands": {
    "pr diff": { "entries": 30, "bytes": 184320 },
    "release view": { "entries": 14, "bytes": 18230 }
  }
}
```

`local_hits` are answered from SQLite; `fallback_hits` are answered from the fallthrough cache; `backend_misses` actually hit GitHub. Watching the ratio is the easiest way to confirm the shim is paying for itself.

## Cache key composition

Cache keys are deterministic SHA-256 hashes of:

- A version tag (`v2`)
- The resolved gitcrawl config path
- The current working directory
- The `GH_HOST` env var
- The `GH_REPO` env var
- For `gh pr diff`: the stable identity `pr-diff:owner/repo:number:head-sha` (when available)
- The full command argument vector, null-separated

This isolates sibling checkouts and portable stores while still coalescing repeated calls from the same agent workspace. Concurrent cache misses use a lock file so one process populates the entry while peers wait for the result, instead of all of them firing at GitHub.

## What does not flow through the shim

- **Mutating commands** — `gh issue close`, `gh pr merge`, `gh pr comment`, `gh api -X POST`, etc. These pass straight through, increment `pass_through_writes`, and clear the relevant cache entries on success.
- **Auth flows** — `gh auth login`, `gh auth refresh`, etc. Always real `gh`.
- **Anything the shim does not recognize** — falls through unmodified.

## Agent integration

Pattern: replace `gh` with `gitcrawl-gh` (or symlink to `gh`) for every agent in the fleet, then keep your existing prompts and tools. Most read-only triage flows ("look up this issue", "check the PR status", "list open issues for this label") become local-only without any prompt changes.

For best results, schedule a periodic `gitcrawl refresh owner/repo` (every few minutes per repo, depending on activity) so the local mirror stays warm. The shim's `--sync-if-stale` (via `gitcrawl search`) and auto-hydration handle the rest.

See [Automation](/automation/) for full agent recipes and JSON contracts.
