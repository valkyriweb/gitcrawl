---
title: Home
layout: home
nav_order: 1
description: "gitcrawl is a local-first GitHub issue and pull request crawler for maintainer triage."
permalink: /
---

# gitcrawl
{: .fs-9 }

A local-first GitHub issue and pull request crawler for maintainer triage. Sync, search, cluster, and review related threads from a SQLite cache that lives entirely on your machine.
{: .fs-6 .fw-300 }

[Quickstart](/quickstart/){: .btn .btn-primary .fs-5 .mb-4 .mb-md-0 .mr-2 }
[View on GitHub](https://github.com/openclaw/gitcrawl){: .btn .fs-5 .mb-4 .mb-md-0 }

---

## What gitcrawl does

`gitcrawl` mirrors a GitHub repository's issues and pull requests into local SQLite, then layers semantic clustering, full-text search, and a `gh`-compatible shim on top so a maintainer (or an agent acting on their behalf) can triage threads without burning live API quota.

- **Local SQLite first.** All issues, PRs, comments, reviews, files, commits, checks, and workflow runs land in `~/.config/gitcrawl/gitcrawl.db`. Queries hit the disk, not GitHub.
- **Semantic clustering.** OpenAI embeddings group related reports, with deterministic GitHub reference evidence (`#123`, `pull/123`) preventing weak similarity bridges from forming mega-clusters.
- **`gh`-compatible shim.** Drop `gitcrawl gh` (or symlink it as `gh`) into agent workflows and most read-only `gh search`, `gh issue/pr view`, `gh pr checks`, and `gh run` calls answer from local cache instead of the GitHub API.
- **Terminal UI.** `gitcrawl tui` is a keyboard- and mouse-driven cluster browser with bidirectional sort, jump-to-number, neighbors, and member-level governance actions.
- **Agent-friendly JSON.** Every command supports `--json` for clean automation surfaces.

---

## Pick your path

<div class="code-example" markdown="1">

### I want to try it
[Quickstart](/quickstart/) walks you from `git clone` to a populated cluster view in five minutes.

### I want to wire up an agent
The [`gh` shim](/gh-shim/) is the fastest way to cut GitHub API load — point your agent at `gitcrawl-gh`, keep the agent's `gh` calls intact.

### I want to triage a busy repo
Read [Sync](/sync/) to bring data local, then [Clustering](/clustering/) and the [TUI](/tui/) for the maintainer workflow.

### I want the full reference
[Commands](/commands/) lists every flag and JSON field. [Configuration](/configuration/) covers env vars and paths.

</div>

---

## Project status

Early bootstrap. The implementation is being built in small commits — see the [changelog](https://github.com/openclaw/gitcrawl/blob/main/CHANGELOG.md) for what shipped recently.

The product contract in [SPEC.md](https://github.com/openclaw/gitcrawl/blob/main/SPEC.md) is the source of truth for what is in and out of scope.

## Out of scope

- Local HTTP API
- Hosted service runtime
- Browser web UI
- GitHub write-back actions (use `gh` for those)

---

## License

Released under the [MIT license](https://github.com/openclaw/gitcrawl/blob/main/LICENSE).
