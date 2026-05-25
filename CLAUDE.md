# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

`wcaw` ("what changed and why") is a Go CLI that takes `<path>:<symbolName>` for a TypeScript file and reconstructs the history of that symbol — every commit that touched it, grouped by GitHub PR, with linked issues, related test files, an LLM-generated one-line summary per PR, and the de facto owner. v1 (TypeScript source, GitHub forge) is shipped; `README.md` has the user-facing surface and `docs/SCHEMA.md` is the wire format for `--json`.

There is no `SPEC.md` in this repo (it was the original roadmap; removed once v1 landed). Don't introduce one — the architecture lives in the package doc comments and in this file.

## Commands

```
go build -o wcaw ./cmd/wcaw      # build the binary (./wcaw is gitignored)
go run ./cmd/wcaw <path>:<sym>   # run without building
go test ./...                    # run all tests
go test ./internal/locator       # test a single package
go test -run TestName ./...      # run a single test by name
go vet ./...                     # vet
gofmt -w .                       # format
```

Requires Go 1.26+ and a C toolchain — `smacker/go-tree-sitter` wraps the C tree-sitter library via CGO. macOS clang and Linux gcc work out of the box; cross-compiling without a C toolchain will fail.

The binary expects to be invoked inside a git repository; `<path>` is resolved relative to the cwd and the enclosing repo root is found by walking up for `.git`.

`internal/forge` has `github_live_test.go` — these hit the real GitHub API and are gated to skip unless explicitly enabled; the default `go test ./...` run stays offline.

## CLI surface

```
wcaw [--json] [--no-cache] <path>:<symbol>
```

- `--json` switches the renderer to the schema documented in `docs/SCHEMA.md` (golden in `internal/render/testdata/sample.json`).
- `--no-cache` skips opening the bbolt cache for this invocation.
- Exit codes: **0** success, **1** runtime error (no repo, unreadable file, unsupported extension, symbol not found, git/render failure), **2** usage error (missing or malformed `<path>:<symbol>`).
- Argument parsing splits on the **last** `:` so Windows paths and colons inside symbol names don't break; both halves must be non-empty.

Configuration sources (all optional, all degrade silently if absent):

- Config file at `$XDG_CONFIG_HOME/wcaw/config.json` (or `~/.config/wcaw/config.json`, including on macOS — matching the `cache.DefaultPath` choice). Schema is `{ "github_token": "...", "openai": { "api_key": "...", "model": "...", "base_url": "..." } }`. Loaded once in `main` via `internal/config.Load`; missing file = empty config, malformed JSON = one stderr warning + empty config.
- `GITHUB_TOKEN` / `GH_TOKEN` — auth for PR enrichment. Resolution order in `internal/forge/auth.go`: `GITHUB_TOKEN` → `GH_TOKEN` → `config.github_token` → `gh auth token` → anonymous (60 req/hr).
- `OPENAI_API_KEY` + `OPENAI_MODEL` — enable the LLM summarizer (OpenAI-compatible chat completions). Both must be set (env or config); missing either disables summaries.
- `OPENAI_BASE_URL` — override the LLM endpoint (point at any OpenAI-compatible API).
- `XDG_CACHE_HOME` — cache lives at `$XDG_CACHE_HOME/wcaw/cache.db`, falling back to `~/.cache/wcaw/cache.db`. Env-only (cross-tool XDG standard, not mirrored in config).
- `NO_COLOR` — disables ANSI codes (also auto-disabled when stdout isn't a TTY). Env-only (cross-tool standard).

Env vars override config: `config.EnvOr(envKey, configFallback)` returns the env value when non-empty, otherwise the configured value. This means `FOO=` (empty) does *not* shadow a configured `foo`.

## Architecture

The pipeline in `cmd/wcaw/main.go` is:

```
splitArg → history.Resolve → reject non-.ts → read file
        → locator.Locate           (find the starting symbol)
        → openCache                (bbolt; nil if --no-cache or open fails)
        → history.Track            (per-commit AST walk, follows moves/renames)
        → enrichOrFallback         (forge.GroupCommits or single no-PR group)
        → decorateTestFiles        (history.CollectTestFiles per group)
        → decorateSummaries        (summarize.DecorateGroups per group)
        → history.EffectiveOwner
        → render.Human | render.JSON
```

Packages:

- **`cmd/wcaw`** — only the entrypoint. Owns argument parsing, exit codes, stderr prefixing, and the four `decorate*` / `openCache` / `enrichOrFallback` helpers that wire the rest of the pipeline. Keep new business logic out of `main.go`; it should stay a thin orchestrator.
- **`internal/history`** — git walker and symbol tracker. `Resolve(cwd, userPath)` returns a `Resolved` struct (`AbsPath`, `RepoRoot`, `RelPath`, cached `*git.Repository` via `Repo()`). `Track(r, sym, enumerator)` is the core: parses every reachable revision of the file with tree-sitter and classifies each commit as `Introduced` / `Modified` / `Renamed` / `MovedFrom` / `Unrelated`, following in-file renames (AST-shape match via `isRename`) and cross-file moves (`scanCrossFileMove`). Merge commits use first-parent only. `CollectTestFiles` lives in `tests.go`, `EffectiveOwner` in `ownership.go`.
- **`internal/locator`** — tree-sitter symbol resolver. `Locate(source, name)` returns the first matching `Symbol` or `*NotFoundError` with up to 3 Levenshtein-ranked suggestions; `Enumerate(source)` returns *all* symbols (used by `history.Track` for per-commit parses). Kinds: `KindFunction`, `KindMethod`, `KindArrowConst`; `export …` shapes return the **outer statement** range. `Levenshtein` is exported because `internal/history` reuses it for rename detection.
- **`internal/forge`** — GitHub PR enrichment. `Forge` is a tiny interface (`PullsForCommit`) so `GroupCommits` is testable without network; the GitHub implementation in `github.go` uses `google/go-github`. `GroupCommits` dedups PRs by number, picks merged > unmerged then smallest number on ties (favours original PR over cherry-picks/reverts), and aborts after 5 consecutive failures or >50% failure rate to trigger the no-PR fallback. `IssueRef` covers both `#142` and `SEC-44` shapes.
- **`internal/render`** — human and JSON renderers. `Input` is the shared payload; `Now` is injected (not `time.Now()`) so reltime golden tests stay deterministic. Both renderers consume the input newest-first and **reverse to chronological order** internally so the timeline reads top-down; owner math runs on the un-reversed slice. `ResetColors(stdoutIsTTY)` is called from `main` so the package doesn't need to know about `os.Stdout`. The JSON schema is versioned (`schema_version: 1`) and documented in `docs/SCHEMA.md`; the golden lives at `internal/render/testdata/sample.json`.
- **`internal/cache`** — bbolt persistence layer. Caches per-commit AST parses (`ASTEnumerator`, satisfying `history.SymbolEnumerator`), per-commit forge lookups (`Wrap` around a `forge.Forge`), and per-PR LLM summaries (`WrapSummarizer`). **The entire package is opt-in and never alters output**: a nil `*Cache` (or any cache error — read miss, decode failure, lost write race) falls through to the underlying logic. Schema is versioned at three levels: top-level `schemaVersion` in `meta`, per-bucket suffixes embedding `locator.SchemaVersion` and `summarize.PromptVersion` so a locator/prompt change auto-invalidates without a full file reset.
- **`internal/summarize`** — LLM summarizer. `Summarizer` interface + OpenAI-compatible client in `openai.go`. `GroupBrief` is the structured prompt input; `BuildBrief` packs a `forge.Group` + `locator.Symbol` into one. `PromptVersion` is bumped when `buildPrompt` changes in a cache-incompatible way (read by `internal/cache` to invalidate stale entries).
- **`internal/config`** — optional JSON config loader. `Load()` reads `$XDG_CONFIG_HOME/wcaw/config.json` (or `~/.config/wcaw/config.json`) into a `Config` struct holding the GitHub token and OpenAI credentials. Missing file → empty Config + nil error; malformed JSON → empty Config + error so main can log one stderr line. `EnvOr(envKey, fallback)` is the helper main uses to enforce "env wins over config" while treating empty env values as unset. The package never imports forge or summarize — values are plumbed in by `cmd/wcaw/main.go`.

The interesting architectural pieces:

1. **Symbol tracking across renames and moves** (`history.Track` / `trackInFile` / `scanCrossFileMove`). The non-obvious bit: a cross-file move is detected by scanning every `.ts` file changed in the diff for a same-shape (AST + body-similarity) candidate, then preferring candidates that were *deleted or removed* at the child commit, since a leftover original is more often a copy than a move. On ambiguity the commit is classified `Introduced` and tracking stops with an `ambiguous move` stderr warning. The `from plumbing.Hash` argument and `flipInstruction` returned by `trackInFile` are how the outer `Track` loop hands off across a move boundary.
2. **The `SymbolEnumerator` seam** between `history` and `cache`. `history.Track` parses ASTs through this interface, not by calling `locator.Enumerate` directly. The default (`nil` enumerator) parses every time; `cache.ASTEnumerator` makes repeat parses of `(commitSHA, filePath)` free. Anything else that needs to intercept per-commit AST work (a metrics layer, a different cache backend) plugs in here.
3. **Graceful degradation contract.** Forge init failure, mid-walk forge abort, cache open failure, test-file enrichment failure, and summarizer failures all degrade with **a single stderr line** and a continuation. Match this pattern when adding new enrichment steps — the user should always get the unenriched timeline rather than a hard error.

## Conventions worth knowing

- User-facing errors are lowercased and prefixed with `wcaw:` in `main.go`; internal errors wrap with `fmt.Errorf("...: %w", err)` and let `main` add the prefix.
- The renderer reverses ordering internally — never pre-reverse the input. `history.Track` and `forge.GroupCommits` both return newest-first; `render.Human` / `render.JSON` produce chronological output without callers changing anything.
- When you change `locator.Symbol` shape, `Kind` values, or the `summarize` prompt, bump `locator.SchemaVersion` or `summarize.PromptVersion` respectively — the cache bucket names depend on them and stale entries will silently corrupt output otherwise.
- The cache is fail-open: never return an error from a cache miss/decode/write path that would propagate to `main`. Log and fall through.
- v1 only handles `.ts` files. `main.go` rejects other extensions before `locator.Locate`; `history.Track`'s `scanCrossFileMove` also filters changed paths by `.ts` suffix. Adding a language means updating both gates plus picking a tree-sitter grammar in `locator`.
- `fixtures/foo.ts` is the only checked-in TS source; unit tests synthesise their own fixtures inline.
