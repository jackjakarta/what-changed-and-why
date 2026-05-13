# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

`wcaw` ("what changed and why") is a CLI that takes `<path>:<symbolName>` and reconstructs the full history of that symbol — every PR that touched it, commit messages, related test changes, the de facto owner. It wraps `git` but presents history as a narrative. Target language for v1 is TypeScript; target forge is GitHub.

`SPEC.md` is the authoritative roadmap. It defines nine implementation phases, sized to fit one session each. **When a phase finishes, update its section in `SPEC.md` to record what actually shipped (and any deviations) before starting the next.** A future-session prompt is intended to be as short as "Implement Phase N of SPEC.md."

Phases 1 and 2 are shipped. Phase 3 (per-commit symbol tracking across moves and renames) is the next slice. Do not stub out packages from later phases up front — add `internal/forge`, `internal/render`, etc. when their phase arrives.

## Commands

```
go build -o wcaw ./cmd/wcaw      # build the binary (./wcaw is gitignored)
go run ./cmd/wcaw <path>:<sym>   # run without building
go test ./...                    # run all tests
go test ./internal/locator       # test a single package
go test -run TestName ./...      # run a single test
go vet ./...                     # vet
gofmt -w .                       # format
```

The binary expects to be invoked inside a git repository; `<path>` is resolved relative to the cwd and the enclosing repo root is found by walking up for `.git`.

The build requires CGO (since Phase 2) because `smacker/go-tree-sitter` wraps the C tree-sitter library. macOS clang and Linux gcc both work out of the box; cross-compiling without a C toolchain will fail.

## Architecture

Layout mirrors SPEC.md §3. The big picture:

- `cmd/wcaw` — CLI entrypoint. Argument parsing splits on the **last** `:` (so Windows paths and colons inside symbol names don't break); both halves must be non-empty. Flow: `Resolve` path → reject non-`.ts` → read file → `locator.Locate` → print "resolved …" header → `WalkResolved` for the history table. Exit codes: **0** success, **1** runtime errors (no repo, file unreadable, symbol not found, unsupported extension, git failure), **2** usage errors.
- `internal/history` — git walker. `Resolve(cwd, userPath)` returns a `Resolved` struct (`AbsPath`, `RepoRoot`, `RelPath`, plus a cached `*git.Repository`). `WalkResolved` walks `repo.Log` filtered by file name; an untracked working-tree file yields an **empty slice, not an error**. `WalkFile` remains as a convenience wrapping the two.
- `internal/locator` — tree-sitter symbol resolver. `Locate(source []byte, name string)` returns a `Symbol` (name, kind, byte range, 1-indexed line range) or `*NotFoundError` with up to 3 Levenshtein-ranked suggestions. Supports function declarations, class methods, and arrow-function consts, with `export …` shapes returning the **outer statement** range. First occurrence wins on duplicate names — Phase 3 owns disambiguation.
- `internal/forge`, `internal/render`, `internal/cache`, `internal/summarize` — **not yet created**. Each arrives with its SPEC phase.

The architecturally interesting piece (Phase 3) will live in `internal/history`: tracking a function across moves and renames. `git log -L` is too brittle, so the plan is to parse each historical revision with tree-sitter and match by symbol name plus AST shape. Keep this in mind when shaping `history`'s public API now — the current `Commit` struct will need to grow per-commit classification (`introduced`, `modified`, `moved-from`, `renamed`, `unrelated`) and likely a symbol-range field.

## Library choices (from SPEC §4)

Starting points; revisit during a phase if any prove painful:

- **Git:** `go-git` (pure Go, no CGO). Only swap for `git2go`/libgit2 if Phase 9 benchmarks demand it.
- **Tree-sitter:** `github.com/smacker/go-tree-sitter` with the TypeScript grammar.
- **CLI parsing:** stdlib `flag`. Promote to `cobra` only when subcommands appear.
- **GitHub API:** `github.com/google/go-github`. Auth via `GITHUB_TOKEN`, falling back to shelling out to `gh auth token`.
- **Cache:** `go.etcd.io/bbolt`, file at `$XDG_CACHE_HOME/wcaw/cache.db` (fall back to `~/.cache/wcaw/`).
- **Color:** `github.com/fatih/color`, auto-disabled when stdout isn't a TTY.

## Conventions worth knowing

- The `chat_with_claude.md` file is the original brainstorm and contains the target output mockup for Phase 6 — consult it when shaping the human renderer, but treat `SPEC.md` as the source of truth for scope.
- Errors surfaced to the user are lowercased and prefixed with `wcaw:` in `main.go`; internal errors wrap with `fmt.Errorf("...: %w", err)` and let `main` add the prefix.
