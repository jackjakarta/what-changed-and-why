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

### Phase 3 — Per-commit symbol tracking (the hard, interesting part) ✅ shipped

- For each commit that touched the file, read the file blob at that commit and re-parse it.
- Match by symbol name first.
- On miss, fall back to AST-shape similarity to detect renames.
- Detect cross-file moves: when the symbol disappears from file A at commit X, scan other files modified in that commit for a matching AST node and follow it.
- Classify each commit as `introduced`, `modified`, `moved-from`, `renamed`, or `unrelated` (file touched but symbol unchanged).

**Demo:** synthetic git history in a test fixture exercising rename + cross-file move; output shows the correct classification per commit.

Notes from shipping:
- `internal/history` gained `Classification`, `SymbolRef`, `Track(r Resolved, sym locator.Symbol) ([]Commit, error)`. `Commit` grew `Class` + `Symbol *SymbolRef`; `SymbolRef` is nil on `ClassUnrelated` rows. `WalkResolved`/`WalkFile` remain untouched as the unclassified flat walk.
- `internal/locator` exports two new entries used by `history`: `Enumerate(source) ([]Symbol, error)` (every supported symbol in source order) and `Levenshtein(a, b string) int` (the previously-internal edit-distance, promoted to avoid duplication).
- `Track` takes the already-located `locator.Symbol` rather than just a name so it cannot disagree with the CLI's "resolved …" header about which first-wins occurrence was picked.
- Merge commits use **first-parent only** for both the symbol diff and the changed-paths diff. The two must agree, so the implementation looks up `c.Parent(0)` once and reuses it.
- **Body-comparison rule** (`ClassModified` vs `ClassUnrelated`): per-line trim of trailing whitespace on `source[StartByte:EndByte]`, then trim a single trailing `\n` from the whole slice (tree-sitter ranges include trailing newlines inconsistently between `export`-wrapped and bare shapes). Byte-equal otherwise. Comment-only edits inside the body count as `ClassModified` by design.
- **Rename gates** (both must pass, plus same `Kind`): name `Levenshtein ≤ longerName/2`, and body `Levenshtein ≤ min(20, max(4, longerLen/8))`. Cap/floor avoids the body-length-proportional pathology at both ends.
- **Cross-file move** scan: diff `parent.Tree()` vs `commit.Tree()` for changed paths; for each `.ts` path other than the tracked file, parse the parent-side blob and collect candidates (same-name + same-Kind = `exact-name`; same-Kind + body-similar = `shape-only` for rename-during-move). Rank `exact-name` > `shape-only`; among ties, prefer files **deleted at the commit**, then files where the candidate symbol **disappeared** parent→commit. If still tied, classify the commit `ClassIntroduced` and emit `wcaw: ambiguous move at <hash>: candidates …` on stderr — v1 surfaces the ambiguity rather than guessing.
- Copy-not-move (symbol exists in both old + new locations at the commit) is reported as `ClassMovedFrom` following the old location — accepted v1 limitation, not worth special-casing yet.
- The walk is implemented as a flip-state loop in `Track`: when `ClassMovedFrom` fires, the tracked `(file, name, kind)` is swapped to the source and `repo.Log` is re-opened from `parent.Hash` backward. No actual recursion.
- New `internal/history/history_test.go` covers five scenarios on synthetic repos built in `t.TempDir()`: introduce-then-modify lifecycle, touched-but-unrelated, clean rename, rename + body tweak, cross-file move. Helper `commitAll` pins an explicit `Author` signature because go-git's `Worktree.Commit` requires one.
- Output adds a single classification column; renames append ` (from <PrevName>)` and moves append ` <SourceFile>` (or `<SourceFile> (as <PrevName>)` if renamed-during-move). Phase 6 owns prettifying.

### Phase 4 — GitHub PR enrichment ✅ shipped

- `internal/forge.Forge` interface; GitHub implementation behind it.
- For each commit, resolve the merging PR (commit-to-PR endpoint; fall back to search).
- Group commits by PR; pull title, author, linked issues (`#123`, `FOO-44` patterns).
- Auth: `GITHUB_TOKEN` env, then `gh auth token` shell-out fallback.

**Demo:** run against a public TS repo; output groups commits under PR titles with authors and issue refs.

Notes from shipping:
- New package `internal/forge` is split across five files: `forge.go` (types + `Forge` interface + `GroupCommits` orchestrator + `ErrNoGitHubRemote`), `github.go` (`GitHubForge` + `NewGitHubFromRepo` + `PullsForCommit`), `remote.go` (URL parsing + `discoverGitHubRemote`), `issues.go` (regexes + `extractIssueRefs`), `auth.go` (`resolveToken` + `bearerTransport`).
- **Function renamed to `GroupCommits`** because Go forbids `func Group` colliding with `type Group struct` in the same package. The struct kept the simpler name since it's the noun in the public API (`[]forge.Group`).
- `PullRef` gained a `Body` field that the plan placed only on `Pull`. Rationale: the primary `ListPullRequestsWithCommit` endpoint already returns full `*github.PullRequest` objects with body inline, so a separate `PullRequests.Get` call would be wasted work. `Pull` is now just `PullRef` + extracted `Issues`. Issue extraction runs on `Title + Body` from the PullRef.
- **Auth chain** matches the plan: `GITHUB_TOKEN` → `GH_TOKEN` → `gh auth token` → anonymous. Anonymous mode prints a single one-line stderr warning (`wcaw: no GitHub token …; using anonymous API (60 req/hr)`) at forge init time, not per request. The bearer-auth `http.RoundTripper` is hand-rolled to avoid pulling in `golang.org/x/oauth2`.
- **Remote discovery** tries `origin` then `upstream`. Three URL shapes parse cleanly: `https://github.com/o/r(.git)?`, `ssh://git@github.com/o/r(.git)?`, `git@github.com:o/r(.git)?` (scp-like). Trailing slash and case-insensitive host are tolerated. Anything else returns `ErrNoGitHubRemote`.
- **Issue regexes** (`internal/forge/issues.go`):
  - Hash: `(?:^|[^A-Za-z0-9_&#])#(\d+)\b` — leading char class excludes `&` (HTML entities), `#` (markdown headers), and word chars (so `abc#123` doesn't match). RE2 has no lookbehind so the prefix is a non-capturing alternation between start-of-string and the excluded char class.
  - Jira: `\b([A-Z][A-Z0-9]{1,9})-(\d+)\b` — project key 2–10 chars, first must be a capital letter, rest uppercase alphanumeric.
  - Both run only on PR `Title` + `Body`, deduped by `Raw` value, first-occurrence order. Number==0 is rejected.
- **PR tie-break** when one SHA maps to multiple PRs (`chooseRef`): merged PRs preferred over unmerged; among ties, smallest PR number wins (favours the original PR over later cherry-pick/revert PRs).
- **Search-API fallback** triggers only when the primary endpoint returns an empty list (not on error). Query is `<sha> type:pr is:merged repo:<owner>/<repo>`. Search returns `*Issue` not `*PullRequest`, so `MergeSHA` is dropped on this path and `MergedAt` comes via `PullRequestLinks`.
- **Degradation rules** in `GroupCommits`: 5 consecutive errors abort the walk; >50% failure rate aborts once at least 5 lookups have been attempted. Individual "no PRs for this commit" is expected and silently bucketed into a `Pull == nil` group. `cmd/wcaw/main.go` catches `GroupCommits` errors and falls back to a single no-PR group containing the unenriched commits, with a one-line stderr warning.
- `internal/history.Resolved` gained a one-line `Repo() *git.Repository` accessor so `forge.NewGitHubFromRepo` doesn't have to re-open the repo from `RepoRoot`. First time `internal/history` exposes the go-git handle outside the package.
- `cmd/wcaw/main.go` flow added two helpers: `enrichOrFallback(commits, repo)` runs the forge (or stays flat on any failure), `renderGroups(w, groups)` emits a header per group followed by indented tab-separated commit lines. Output keeps the Phase-3 commit-line shape unchanged; Phase 6 owns the timeline mockup.
- **Output shape** (example with a single matched PR):
  ```
  resolved bar at fixtures/foo.ts:1-3 (bytes 0-32)

  PR #1 "feat: implement v1"  @jackjakarta
    a709906	2026-05-13	jackjakarta	introduced	feat: phase 2
  ```
  When no PR matches: `(no PR)` header followed by the same commit lines.
- **New deps:** `github.com/google/go-github/v66 v66.0.0` (direct), `github.com/google/go-querystring v1.1.0` (transitive). `go.mod` still doesn't require `golang.org/x/oauth2`.
- **Tests:** `internal/forge/forge_test.go` covers URL parse (14 cases), issue regex (12 cases), and `GroupCommits` behavior (ordering, no-PR bucketing, dedup, consecutive-error abort, single-error tolerance, tie-break). A live opt-in smoke test lives in `github_live_test.go` behind `//go:build forge_live` and a `GITHUB_TOKEN` env-var check.
- **Open questions parked for later phases:** squash-vs-merge edge cases never surfaced in the demo (commit-to-PR endpoint handles squashes correctly out of the box); rate-limit strategy on big repos is Phase 7's caching problem, not Phase 4's. The plan's flag for `--no-forge` was deliberately not added — there's no flag scaffolding yet and degradation is automatic.

### Phase 5 — Ownership and related test changes ✅ shipped

- "Effective owner" = (% of touching commits, last-touched date). Show both.
- For each PR, list co-modified files matching `*.test.ts`, `*.spec.ts`, `__tests__/**`.

**Demo:** output ends with an "Effective owner" line and each PR notes the test files it touched.

Notes from shipping:
- New file `internal/history/ownership.go`: `Owner{Name, Commits, Total, LastTouched}` + `EffectiveOwner(commits []Commit) (Owner, bool)` + `Owner.Percent() int`. Counts only commits where `Class` is neither `ClassUnrelated` nor `ClassUnknown`. Tie-break (deterministic): highest commit count → most recent `LastTouched` → lexicographically smallest name. Returns `(_, false)` on empty input or all-unrelated history so the caller suppresses the footer entirely rather than printing "unknown".
- `Owner.Percent()` rounds to the nearest integer (`(commits*100 + total/2)/total`); zero-`Total` Owners return 0. Phase 6 owns any prettier formatting.
- New file `internal/history/tests.go`: `CollectTestFiles(repo, commits, exclude) ([]string, error)` returning the deduped, alphabetically sorted union of repo-relative test paths touched across the input commits. Uses `commit.Parent(0)` (first-parent only, same merge rule as Phase 3); root commits enumerate their whole tree via `commit.Tree().Files()` so an "introduced + tests added in the same root commit" lifecycle still surfaces. `exclude` is dropped from the result (typically the tracked file's `RelPath`).
- `isTestPath(p)` rules: basename ends in `.test.ts` or `.spec.ts` → match; otherwise must end in `.ts` AND have a `__tests__` path segment. `.tsx` is deliberately excluded (Phase 2 deferred the `.tsx` grammar), and so is anything with a non-`.ts` extension inside `__tests__/` (no `.css`, `.json`, etc.).
- `forge.Group` gained `TestFiles []string` — zero-value safe, populated by the new `cmd/wcaw/main.go` `decorateTestFiles` step, untouched by `GroupCommits`. Adding a slot here (rather than a parallel map) anticipates Phase 6's renderer rewrite.
- `cmd/wcaw/main.go` orchestration: after `enrichOrFallback`, `decorateTestFiles(repo, groups, resolved.RelPath)` fills `Group.TestFiles`; `renderGroups` emits `  tests: a, b, c` after the commit rows when the slice is non-empty; `renderOwner(commits)` then prints the footer using the **flat** `commits` slice (not per-group) so renames/moves don't fragment the denominator.
- Test-file enrichment failures are silent-degrade: one stderr warning + empty `TestFiles` for the remaining groups. Ownership computation never touches git tree state, so it can't fail this way.
- `commitChangedPaths` lives next to `CollectTestFiles` in `internal/history/tests.go` rather than being promoted to a public history-package helper — its only caller is `CollectTestFiles`, and the existing private `changedPaths` it wraps is already used by Phase 3's `scanCrossFileMove`. Added a small `shortHashStr(s string)` because the existing `shortHash` helper takes `plumbing.Hash`, not a string.
- New tests: `internal/history/ownership_test.go` (9 pure-func cases covering single-author, mixed majority, unrelated-exclusion, both tie-breaks, all-unrelated, empty, last-touched-is-author-not-global-max, percent rounding) and `internal/history/tests_test.go` (5 synthetic-repo cases + 10-row `isTestPath` table). Reuses `commitAll`/`writeFile`/`track` helpers from `history_test.go`; introduces a `tracked()` wrapper that returns both commits and `*Resolved` so test-file collection can re-use the cached `repo` handle.
- `cmd/wcaw` self-demo (single-commit repo with no tests) now prints `Effective owner: jackjakarta (100% of commits, last-touched 2026-05-13)` as the trailing line; no `tests:` line because no test files matched. Behavior on richer histories is covered by `tests_test.go` rather than a checked-in fixture (the synthetic git repos run in `t.TempDir()`).
- Output shape after Phase 5 (with both new pieces present):
  ```
  resolved validateToken at src/auth/login.ts:14-32 (bytes 220-612)

  PR #142 "harden token validation"  @alice  (issues: #91)
    3a93a2a	2026-04-12	alice	modified	tighten signature check
    5689668	2026-04-11	alice	modified	add expiry tolerance
    tests: src/auth/__tests__/login.ts, src/auth/login.test.ts

  Effective owner: alice (67% of commits, last-touched 2026-04-12)
  ```

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
