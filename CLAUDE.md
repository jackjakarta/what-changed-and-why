# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

`wcaw` ("what changed and why") is a CLI that takes `<path>:<symbolName>` and reconstructs the full history of that symbol — every PR that touched it, commit messages, related test changes, the de facto owner. It wraps `git` but presents history as a narrative. Target language for v1 is TypeScript; target forge is GitHub.

`SPEC.md` is the authoritative roadmap. It defines nine implementation phases, sized to fit one session each. **When a phase finishes, update its section in `SPEC.md` to record what actually shipped (and any deviations) before starting the next.** A future-session prompt is intended to be as short as "Implement Phase N of SPEC.md."

Phase 1 is shipped. Phase 2 (tree-sitter symbol resolution in the working tree) is the next slice. Do not stub out packages from later phases up front — add `internal/locator`, `internal/forge`, etc. when their phase arrives.

## Commands

```
go build -o wcaw ./cmd/wcaw      # build the binary (./wcaw is gitignored)
go run ./cmd/wcaw <path>:<sym>   # run without building
go test ./...                    # run all tests
go test ./internal/history       # test a single package
go test -run TestName ./...      # run a single test
go vet ./...                     # vet
gofmt -w .                       # format
```

The binary expects to be invoked inside a git repository; `<path>` is resolved relative to the cwd and the enclosing repo root is found by walking up for `.git`.

## Architecture

Layout mirrors SPEC.md §3. The big picture:

- `cmd/wcaw` — CLI entrypoint. Argument parsing splits on the **last** `:` (so Windows paths and colons inside symbol names don't break); both halves must be non-empty. Exit codes: **0** success, **1** runtime errors (no repo, file not in HEAD, git failure), **2** usage errors.
- `internal/history` — git walker. `WalkFile` resolves the user path against cwd, finds the repo root, opens it with `go-git`, runs an explicit `ensureFileInHead` check (fail fast on typos *before* iterating history), then walks `repo.Log` filtered by file name.
- `internal/locator`, `internal/forge`, `internal/render`, `internal/cache`, `internal/summarize` — **not yet created**. Each arrives with its SPEC phase.

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
