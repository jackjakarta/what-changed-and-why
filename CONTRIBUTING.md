# Contributing to wcaw

Thanks for your interest in improving `wcaw`. This guide covers the build, test,
and style conventions for contributions. For the deeper architecture — the
tracking pipeline, package responsibilities, and the graceful-degradation
contract — see [`CLAUDE.md`](CLAUDE.md).

## Prerequisites

- **Go 1.26+**
- **A C toolchain.** `smacker/go-tree-sitter` wraps the C tree-sitter library via
  CGO. macOS clang and Linux gcc work out of the box; cross-compiling without a C
  toolchain will fail.

## Building & running

```
go build -o wcaw ./cmd/wcaw      # build the binary (./wcaw is gitignored)
go run ./cmd/wcaw <path>:<sym>   # run without building
```

`wcaw` must be invoked inside a git repository; `<path>` is resolved relative to
the cwd.

## Testing

```
go test ./...                    # run all tests (offline)
go test ./internal/locator       # test a single package
go test -run TestName ./...      # run a single test by name
```

`internal/forge/github_live_test.go` hits the real GitHub API and is gated to skip
unless explicitly enabled, so the default `go test ./...` run stays offline. A
clean offline run is the expectation for every PR.

## Formatting & vetting

Pull requests to `main` run a static-checks gate (`.github/workflows/static-checks.yml`)
that fails on unformatted code (`gofmt`), vet findings (`go vet`), a build error
(`go build`), or a failing test (`go test`). The suite runs offline — the
live GitHub test stays gated off in CI. Run these locally before submitting to
catch problems early:

```
gofmt -w .
go vet ./...
go test ./...
```

## Code conventions

- **Error style.** User-facing errors are lowercased and prefixed with `wcaw:` in
  `cmd/wcaw/main.go`; internal errors wrap with `fmt.Errorf("...: %w", err)` and
  let `main` add the prefix.
- **Keep `main.go` thin.** `cmd/wcaw/main.go` is a thin orchestrator — argument
  parsing, exit codes, and pipeline wiring. Keep new business logic in the
  `internal/*` packages.
- **Degrade gracefully.** Enrichment steps (forge, test files, summaries, cache)
  must degrade with a single stderr line and a continuation, never a hard error.
  The user should always get the unenriched timeline rather than a crash.
- **Bump schema versions.** When you change `locator.Symbol` shape / `Kind`
  values, bump `locator.SchemaVersion`; when you change the summarizer prompt,
  bump `summarize.PromptVersion`. The cache bucket names depend on these, and
  stale entries will silently corrupt output otherwise.

## Scope

v1 supports TypeScript (`.ts`) source files and GitHub-hosted repositories.
Adding a language means updating both the extension gate in `cmd/wcaw/main.go`
and the `.ts` filter in `history.Track`'s `scanCrossFileMove`, plus picking a
tree-sitter grammar in `internal/locator`.

## Submitting changes

1. Fork the repo and create a topic branch.
2. Make your change with tests where it makes sense.
3. Ensure `go test ./...`, `go vet ./...`, and `gofmt -w .` are all clean.
4. Open a pull request against `main` with a clear description of what changed
   and why.
