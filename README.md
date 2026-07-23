# git-branch-graph

Analyze any Git repository and view its **branches, merges, squashes, cherry-picks,
PRs, tags and releases** as a fixed-column, GitFlow-ordered swimlane graph — in an
interactive web UI, as a static site, or over MCP for AI assistants.

Point it at a GitHub URL or a local repository. It clones only the commit graph
(no file contents), analyzes it once, and serves the result. A single self-contained
binary (`gbg`) does everything.

---

## Quick start

Follow these steps top to bottom and you will end up with an interactive branch
graph of any repository in your browser.

### 0. Prerequisites

- **Go 1.25+**, **Git**, and **Node.js 18+ with npm** on your `PATH`.

```bash
go version && git --version && node --version && npm --version
```

A GitHub token is **optional** (only enriches PR/CI info) — everything below works
without one.

### 1. Get and build

```bash
git clone https://github.com/0xmhha/git-branch-graph.git
cd git-branch-graph
make binary        # builds the web UI, embeds it, and compiles bin/gbg
```

> Use `make binary`, not `make build`. `make build` compiles a Go-only binary
> **without** the web UI — `gbg serve` will then respond with an API but show a
> blank page in the browser.

### 2. Start the web UI

```bash
./bin/gbg serve
```

Open **<http://localhost:8080>**.

### 3. Analyze a repository — right in the browser

On the landing page, paste any GitHub URL (or a local repository path), e.g.

```
https://github.com/anthropics/anthropic-sdk-go
```

and hit **Analyze**. Progress is streamed live (`Cloning… → Extracting commits &
refs… → Computing branch graph… → Ready`), and when it finishes the repository
opens automatically:

- The **Graph** tab — the swimlane of branches, merges, squashes and
  cherry-picks, with branch highlighting and commit search.
- The **Releases** tab — the version × environment matrix and the
  "where is this fix?" lookup.

Every analyzed repository stays listed on the landing page for instant reopening.

> Prefer the terminal? `./bin/gbg ingest <url>` does the same analysis from the
> CLI (useful for scripts/CI); the result shows up in the UI the same way.

### If something goes wrong

| Symptom | Cause / fix |
|---|---|
| `./bin/gbg: no such file or directory` | The build step was skipped or failed — run `make binary` from the repo root and check its output. |
| Browser shows a blank page or only JSON | The binary was built with `make build` (no embedded UI). Rebuild with `make binary`. |
| `make binary` fails at `npm install` | Node.js 18+/npm missing. Install Node, or use `make build` + the API/MCP only. |
| Port 8080 already in use | `./bin/gbg serve --addr :9090` and open that port instead. |
| PRs show no merge method / CI status | No GitHub token found. Export `GITHUB_TOKEN` (or log in with `gh`) and re-run `ingest --refresh-enrich`. Analysis itself works without it. |

---

## Features

- **Any source** — a remote GitHub URL, a local repository path, or a folder you
  analyzed earlier.
- **Fixed branch columns** — branches are laid out left→right by GitFlow role
  (`feature → default → release → hotfix → master`), each with its own spine.
- **Merge classification** — distinguishes **merge / squash / cherry-pick** landings
  (offline from commit structure; cherry-picks from `-x` markers).
- **Release dashboard** — a version × environment matrix, per-release contents, and
  an "unreleased on the default branch" preview.
- **Reverse queries** — *"which branches and releases contain this commit / PR?"*,
  *"what's in this release?"*, PRs by merge method, commit search.
- **Optional PR/CI enrichment** — merge method, base/head refs, and CI status pulled
  from the GitHub API when a token is available; works fully offline without one.
- **Three ways to consume the data**
  - **Interactive web UI** served by `gbg serve`.
  - **Static export** (`gbg export`) that runs with no server — the browser queries
    the bundled SQLite via sql.js.
  - **MCP server** (`gbg mcp`) that exposes the analysis as tools for AI clients.
- **SQL-queryable output** — every run produces a `graph.sqlite` you can query directly.

---

## How it works

```
repo URL / local path
  → bare, blobless clone (commit graph only; re-runs fetch incrementally)
  → single-pass `git log` → raw CSV (commits, refs, edges)
  → optional GitHub API enrichment (PR state, CI, merge method)
  → analysis: columns, colors, merge/cherry classification, containment, links
  → data/<org>__<repo>__<branch>__<sha7>/{graph.json, graph.sqlite, raw/*.csv, meta.json}
  → web UI / static export / MCP read that folder
```

Runs are content-addressed by the default branch's HEAD, so re-analyzing an unchanged
repository is a fast cache hit.

---

## Requirements

- **Go 1.25+** — to build the binary.
- **Git** — used as a subprocess for cloning and history extraction.
- **Node.js 18+ and npm** — only to build the web UI. Not needed if you build the
  Go-only binary and use the CLI/API without the bundled front end.
- **A GitHub token (optional)** — enables PR/CI enrichment. Provide it via
  `GITHUB_TOKEN`, `GH_TOKEN`, or an authenticated `gh` CLI (`gh auth token`).
  Without a token, analysis still works using offline classification.

---

## Installation

See [Quick start](#quick-start) for the recommended path (`make binary`). If you
only need the CLI/API/MCP without the bundled front end, a Go-only build works
without Node.js:

```bash
make build           # Go-only; serve falls back to --web-dir or API-only
```

Both `make binary` and `make build` produce `bin/gbg`.

---

## Usage

### Analyze a repository — `gbg ingest`

```bash
gbg ingest <github-url | local-path | analyzed-folder> [flags]
```

| Flag | Description |
|---|---|
| `--data-dir <dir>` | Output root for runs (default `./data`). |
| `--default-branch <b>` | Override the default branch (useful for local paths). |
| `--repo <owner/name>` | Canonical GitHub repo for links and enrichment (fixes local-path guesses). |
| `--no-enrich` | Skip GitHub PR/CI enrichment even if a token is available. |
| `--refresh-enrich` | Ignore the per-repo enrich cache and re-query all PRs. |
| `--containment <mode>` | Containment output scope: `full` (default), `pr-only`, or `recent:N`. |
| `--force` | Re-analyze even if the run folder already exists. |

```bash
gbg ingest https://github.com/<org>/<repo>            # remote
gbg ingest /path/to/repo                               # local
gbg ingest /path/to/repo --repo <org>/<repo>           # correct links/enrichment for a local clone
gbg ontology data/<run-folder>                         # recompute graph.* from cached raw CSV
```

### Explore in the browser — `gbg serve`

```bash
gbg serve [--data-dir ./data] [--addr :8080] [--web-dir web/dist]
```

Serves the web UI plus a small HTTP API. With `make binary` the UI is embedded, so
`gbg serve` needs no `--web-dir`. The UI has two tabs:

- **Graph** — the swimlane, with branch highlighting, a commit filter, a viewport
  range control, and jump-to-commit.
- **Releases** — the release × environment matrix, per-release contents, and the
  "where is this fix?" lookup.

### Static export (no server) — `gbg export`

```bash
gbg export <out-dir> [--data-dir ./data] [--run <id>]
```

Writes a self-contained static site (UI + selected run data + a manifest). Host the
folder anywhere; the browser loads `graph.json` and queries `graph.sqlite` in-page via
sql.js — no backend required.

### AI integration (MCP) — `gbg mcp`

```bash
gbg mcp [--data-dir ./data]
```

Runs a [Model Context Protocol](https://modelcontextprotocol.io) server over stdio.
Instead of handing an LLM the whole graph, it exposes focused query tools:
`list_repositories`, `repository_summary`, `where_is` (SHA/PR → containing branches and
releases), `unreleased_commits`, `list_prs`, `releases`, and `search_commits`.

Register it with an MCP-capable client (e.g. Claude Desktop or Claude Code):

```jsonc
{
  "mcpServers": {
    "git-branch-graph": {
      "command": "/absolute/path/to/bin/gbg",
      "args": ["mcp", "--data-dir", "/absolute/path/to/data"]
    }
  }
}
```

### Manage runs — `gbg runs`

```bash
gbg runs list            # list analyzed repositories
gbg runs rm <id>         # delete a run folder
```

---

## Output layout

Each analysis produces one folder under the data directory:

```
data/<org>__<repo>__<branch>__<sha7>/
├── raw/
│   ├── commits.csv      # commit graph
│   ├── refs.csv         # branches and tags
│   ├── edges.csv        # parent relationships
│   └── prs.csv          # classified PR metadata
├── graph.json           # render-ready graph (nodes, edges, columns, links)
├── graph.sqlite         # the same data, SQL-queryable (containment, releases, PRs)
└── meta.json            # snapshot metadata (repo, default branch, counts, captured-at)
```

---

## Development

```bash
# Backend
go build ./...
go test ./...

# Web UI (Svelte + Vite)
cd web
npm install
npm run dev      # dev server on :5173, proxying /api to gbg serve (:8080)
npm run build    # outputs web/dist
```

Run `gbg serve` in one terminal and `npm run dev` in another to develop the UI
against live data.

---

## License

No license file is included yet. Add a `LICENSE` before distributing or accepting
external contributions.
