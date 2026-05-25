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

Quick install (prebuilt binary, macOS & Linux):

```
curl -fsSL https://wcaw.jackjakarta.xyz/install.sh | bash
```

Installs to `/usr/local/bin` (or `~/.local/bin` when sudo isn't available). Override the
location with `WCAW_INSTALL_DIR`, or pin a version with `WCAW_VERSION`.

From source with Go:

```
go install github.com/jackjakarta/what-changed-and-why/cmd/wcaw@latest
```

Or from a clone:

```
go build -o wcaw ./cmd/wcaw
```

Building from source requires Go 1.26+ and a C toolchain (the tree-sitter binding uses CGO).

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

wcaw reads optional settings from a JSON file at `$XDG_CONFIG_HOME/wcaw/config.json` (or `~/.config/wcaw/config.json` if `XDG_CONFIG_HOME` is unset, including on macOS). Every field is optional and the file may be omitted entirely.

```json
{
  "github_token": "ghp_...",
  "dgpt": {
    "api_key": "sk-...",
    "model": "gpt-4o-mini",
    "base_url": "https://api.openai.com/v1"
  }
}
```

The file may contain secrets — `chmod 600 ~/.config/wcaw/config.json` is recommended.

Environment variables override the config file (env wins when set to a non-empty value):

| Env var                       | Purpose                                                                                  |
|-------------------------------|------------------------------------------------------------------------------------------|
| `GITHUB_TOKEN` / `GH_TOKEN`   | GitHub auth for PR enrichment. Order: `GITHUB_TOKEN` → `GH_TOKEN` → `config.github_token` → `gh auth token`. Without any token, the GitHub API is anonymous (60 req/hr). |
| `DGPT_API_KEY`, `DGPT_MODEL`  | Optional LLM summarizer (OpenAI-compatible chat completions). Both required to enable. Override `config.dgpt.api_key` / `config.dgpt.model`. |
| `DGPT_BASE_URL`               | Optional override for the LLM endpoint. Overrides `config.dgpt.base_url`.                |
| `XDG_CACHE_HOME`              | Standard XDG var; cache path base (see below).                                           |
| `NO_COLOR`                    | Standard cross-tool var; disables ANSI colors when set to any non-empty value.           |

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
