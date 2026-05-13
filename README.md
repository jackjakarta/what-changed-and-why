# wcaw

**What changed and why** — narrate the history of a single TypeScript symbol.

Give `wcaw` a `<path>:<symbol>` and it walks git history, tracks the symbol across moves and renames via tree-sitter, groups commits by GitHub PR, and prints a timeline with the effective owner.

```
$ wcaw src/auth/login.ts:validateToken

validateToken — introduced 1 year ago, 5 commits across 3 PRs

  Aug 2024  PR #142 "Initial JWT validation"  @maria
            ─ 19 lines
            ─ alongside src/auth/login.test.ts
  Oct 2024  PR #189 "Fix clock-skew tolerance"  @jonas
            ─ linked issue: SEC-44
  Mar 2025  PR #311 "Refactor for refresh tokens"  @maria
            ─ 2 commits
            ─ also touched src/auth/session.ts
  Sep 2025  (no PR)

Effective owner: @maria (60% of changes, last touched 14 months ago)
```

## Install

```
go install github.com/jackjakarta/what-changed-and-why/cmd/wcaw@latest
```

Or from a clone:

```
go build -o wcaw ./cmd/wcaw
```

Requires Go 1.26+ and a C toolchain (the tree-sitter binding uses CGO).

## Usage

```
wcaw [--json] [--no-cache] <path>:<symbol>
```

| Flag         | Effect                                                              |
|--------------|---------------------------------------------------------------------|
| `--json`     | Emit schema v1 JSON instead of the human timeline (see [docs/SCHEMA.md](docs/SCHEMA.md)). |
| `--no-cache` | Bypass the local cache for this invocation.                         |

Must be run inside a git repository. `<path>` is resolved relative to the cwd.

## Configuration

| Env var                       | Purpose                                                                                  |
|-------------------------------|------------------------------------------------------------------------------------------|
| `GITHUB_TOKEN` / `GH_TOKEN`   | GitHub auth for PR enrichment. Falls back to `gh auth token`. Without any token, the GitHub API is anonymous (60 req/hr). |
| `DGPT_API_KEY`, `DGPT_MODEL`  | Optional LLM summarizer (OpenAI-compatible chat completions). Both required to enable.   |
| `DGPT_BASE_URL`               | Optional override for the LLM endpoint.                                                  |

The cache lives at `$XDG_CACHE_HOME/wcaw/cache.db` (or `~/.cache/wcaw/cache.db`) and is opened transparently. Forge and LLM failures degrade silently with a single stderr line.

## Exit codes

- `0` — success
- `1` — runtime error (no repo, unreadable file, unsupported extension, symbol not found, git/render failure)
- `2` — usage error (missing or malformed `<path>:<symbol>`)

## Development

```
go test ./...
go test ./internal/locator
go vet ./... && gofmt -w .
```

## Scope

v1 supports TypeScript (`.ts`) source files and GitHub-hosted repositories.
