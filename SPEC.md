# `wcaw` — a "what changed and why" CLI

## 1. Overview

`wcaw` answers the question *"why does this function look the way it does?"* by pointing at a symbol in a source file and getting back the full story: every PR that touched it, the commit messages, related test changes, and the de facto owner. It wraps `git` but presents history as a narrative rather than a flat log.

Most useful in codebases older than six months, where the original context lives in PR descriptions and reviewer threads, not in code.

**Target user:** a developer working in a TypeScript codebase, trying to understand a function's history before changing it.

**Non-goals for v1:**
- Languages other than TypeScript
- Forges other than GitHub
- AI-driven summaries (later phase, optional)
- Web UI, GitHub Action, editor integration
- Multi-symbol queries in one invocation

## 2. Usage

```
wcaw <path>:<symbolName> [flags]
```

Example:

```
$ wcaw src/auth/login.ts:validateToken
```

**Planned v1 flags:**

| Flag           | Purpose                                              |
| -------------- | ---------------------------------------------------- |
| `--json`       | Emit a stable JSON schema instead of human output    |
| `--since DATE` | Limit history to commits after this date             |
| `--limit N`    | Show at most N PRs / commit groups                   |
| `--no-cache`   | Bypass the local cache for this invocation           |

Default output is a human-readable timeline grouped by PR. See the brainstorm in `chat_with_claude.md` for the visual shape.

## 3. Architecture

```
cmd/wcaw/               CLI entrypoint, arg + flag parsing
internal/locator/       Tree-sitter symbol resolution (find a function in a file)
internal/history/       Git walker; per-commit symbol tracking
internal/forge/         Interface + GitHub implementation; commit -> PR mapping
internal/render/        Human + JSON renderers
internal/cache/         Bolt-backed cache keyed by (repo, commit, file, symbol)
internal/summarize/     AI phase grouping (later, optional)
```

The interesting piece is `internal/history`: function tracking across moves and renames. `git log -L` is too brittle for that; we parse each historical revision of the file with tree-sitter and match by symbol name plus AST shape.

## 4. Library choices

These are starting points; revisit during Phase 1 if any prove painful.

- **Git:** [`go-git`](https://github.com/go-git/go-git) — pure Go, no CGO. Swap for `git2go`/libgit2 only if perf forces it.
- **Tree-sitter:** [`github.com/smacker/go-tree-sitter`](https://github.com/smacker/go-tree-sitter) with the TypeScript grammar.
- **CLI parsing:** stdlib `flag`. Promote to `cobra` only when subcommands appear.
- **GitHub API:** [`github.com/google/go-github`](https://github.com/google/go-github). Auth via `GITHUB_TOKEN` or `gh auth token`.
- **Cache:** [`go.etcd.io/bbolt`](https://github.com/etcd-io/bbolt) — single-file kv store, pure Go.
- **Color:** [`github.com/fatih/color`](https://github.com/fatih/color), auto-disabled when stdout is not a TTY.

## 5. Implementation phases

Each phase ends with a runnable binary and a concrete demo. Phases are sized to fit in one session.

### Phase 1 — Skeleton + git-only walker ✅ shipped

- Project layout (`cmd/wcaw`, `internal/history`). Other `internal/` packages are added when their phase arrives, not stubbed up front.
- CLI scaffold; parse `<path>:<symbol>` argument; surface clear errors on bad input.
- Use `go-git` to walk commits that touched the given file (ignore the symbol for now).
- Print one line per commit: short hash, date, author, subject (tab-separated, no color).

**Demo:** `wcaw chat_with_claude.md:anything` lists the single commit on this repo.

Notes from shipping:
- Path is resolved relative to cwd; the enclosing git repo is found by walking up for `.git`.
- Argument splits on the **last** `:`; both halves must be non-empty.
- Symbol value is parsed and validated but ignored — Phase 2 wires it in.
- HEAD-tree check runs before history iteration to fail fast on typos.
- Exit codes: 0 success, 1 runtime errors (no repo, file not in HEAD), 2 usage errors.
- `/wcaw` build artifact is gitignored.

### Phase 2 — Tree-sitter symbol resolution in the working tree ✅ shipped

- Add `go-tree-sitter` and the TypeScript grammar.
- Given `path:symbol`, parse the current file and locate the symbol's AST node and byte range.
- Support: function declarations, exported functions, methods on a class, arrow-function consts.
- Error clearly when the symbol isn't found, suggesting the closest matches.

**Demo:** add `fixtures/foo.ts` with a few functions; `wcaw fixtures/foo.ts:bar` prints the resolved range.

Notes from shipping:
- New package `internal/locator` exposes `Symbol`, `Kind` (`KindFunction`, `KindMethod`, `KindArrowConst`), `Locate(source []byte, name string) (Symbol, error)`, and `*NotFoundError` carrying up to 3 Levenshtein-ranked suggestions.
- Walker is a hand-rolled recursive descent over named children rather than a tree-sitter `Query`: easier to debug and trivially extensible when Phase 3 grows new shapes.
- `export function …` / `export const …` resolve to the **outer** `export_statement` range (not the inner declaration) — this is what Phase 3 will want to diff against.
- Symbol ambiguity (same name in two classes, etc.) silently picks the first occurrence in source order. Phase 3 owns proper disambiguation.
- `cmd/wcaw/main.go` rejects non-`.ts` extensions up front; `.tsx` is deliberately deferred (adds a second grammar import and isn't required by the demo).
- `internal/history` gained `Resolve(cwd, userPath)` returning a `Resolved` struct plus `WalkResolved(Resolved)`. `Resolve` no longer enforces HEAD membership — the locator runs on the working tree, and `WalkResolved` returns an empty slice for untracked files instead of erroring. Typo detection now happens at `os.ReadFile` time.
- Build now requires CGO (`smacker/go-tree-sitter` wraps the C tree-sitter library).
- `fixtures/foo.ts` covers all four supported shapes; tests in `internal/locator/locator_test.go` cover each kind plus the export-range, first-wins, and not-found-suggestion behaviors.

### Phase 3 — Per-commit symbol tracking (the hard, interesting part)

- For each commit that touched the file, read the file blob at that commit and re-parse it.
- Match by symbol name first.
- On miss, fall back to AST-shape similarity to detect renames.
- Detect cross-file moves: when the symbol disappears from file A at commit X, scan other files modified in that commit for a matching AST node and follow it.
- Classify each commit as `introduced`, `modified`, `moved-from`, `renamed`, or `unrelated` (file touched but symbol unchanged).

**Demo:** synthetic git history in a test fixture exercising rename + cross-file move; output shows the correct classification per commit.

### Phase 4 — GitHub PR enrichment

- `internal/forge.Forge` interface; GitHub implementation behind it.
- For each commit, resolve the merging PR (commit-to-PR endpoint; fall back to search).
- Group commits by PR; pull title, author, linked issues (`#123`, `FOO-44` patterns).
- Auth: `GITHUB_TOKEN` env, then `gh auth token` shell-out fallback.

**Demo:** run against a public TS repo; output groups commits under PR titles with authors and issue refs.

### Phase 5 — Ownership and related test changes

- "Effective owner" = (% of touching commits, last-touched date). Show both.
- For each PR, list co-modified files matching `*.test.ts`, `*.spec.ts`, `__tests__/**`.

**Demo:** output ends with an "Effective owner" line and each PR notes the test files it touched.

### Phase 6 — Output polish

- Human renderer rewritten to match the brainstorm's timeline mockup: PR header line, indented detail lines, summary blocks.
- `--json` mode with a stable, documented schema (commit + PR + ownership objects).
- Color via `fatih/color`, automatically disabled when stdout is not a TTY.

**Demo:** side-by-side: piped `wcaw ... --json` parses cleanly with `jq`; bare `wcaw ...` renders a colored timeline.

### Phase 7 — Caching

- Bolt cache keyed by `(repo-root, commit-sha, file-path, symbol-name)`.
- Cache the things that are expensive and immutable: parsed ASTs per `(commit, file)`, forge lookups per commit, PR metadata.
- `--no-cache` bypasses reads and writes for the invocation.
- Cache file location: `$XDG_CACHE_HOME/wcaw/cache.db` (fall back to `~/.cache/wcaw/`).

**Demo:** a second run on a large repo is dramatically faster than the first; `--no-cache` reproduces first-run timing.

### Phase 8 — AI summarizer (optional)

- Cluster commits/PRs into "phases" with one-line summaries via the Claude API.
- Gated on `ANTHROPIC_API_KEY`; if missing, fall back silently to the raw PR list.
- Cache summaries by PR-set hash so re-runs don't re-spend tokens.

**Demo:** with `ANTHROPIC_API_KEY` set, output shows grouped phases with one-line summaries; without it, output is unchanged from Phase 6.

### Phase 9 — Performance pass

- Benchmark against a large public TS repo (VS Code, TypeScript itself).
- Parallelize per-commit re-parsing (bounded worker pool).
- Profile; fix the worst hotspots.
- Only consider swapping `go-git` for libgit2 if benchmarks demand it.

**Demo:** before/after numbers on the same target repo; recorded in `BENCH.md`.

## 6. Open questions

Revisit at the relevant phase, not now:

- **Phase 4:** How to handle squash-merged vs merge-commit PR resolution edge cases.
- **Phase 4:** Rate-limit strategy for the GitHub API on big repos.
- **Phase 8:** What defines a "phase" boundary for the summarizer? Time gap, topic shift, reviewer overlap, or some combination?

## 7. Working with this spec

Each phase is intended as one Claude Code session. A future-session prompt can be as short as:

> Implement Phase 3 of SPEC.md.

When a phase finishes, update the corresponding section in this file to reflect what actually shipped (and note any deviations) before starting the next one.
