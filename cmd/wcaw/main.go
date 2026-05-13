package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-git/go-git/v5"

	"github.com/jackjakarta/what-changed-and-why/internal/cache"
	"github.com/jackjakarta/what-changed-and-why/internal/forge"
	"github.com/jackjakarta/what-changed-and-why/internal/history"
	"github.com/jackjakarta/what-changed-and-why/internal/locator"
	"github.com/jackjakarta/what-changed-and-why/internal/render"
)

const usage = `usage: wcaw [--json] [--no-cache] <path>:<symbol>

example:
  wcaw src/auth/login.ts:validateToken
`

func main() {
	jsonOut := flag.Bool("json", false, "emit JSON instead of human output")
	noCache := flag.Bool("no-cache", false, "bypass the local cache for this invocation")
	flag.Usage = func() { fmt.Fprint(os.Stderr, usage) }
	flag.Parse()

	if flag.NArg() != 1 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}

	path, symbol, err := splitArg(flag.Arg(0))
	if err != nil {
		fmt.Fprintf(os.Stderr, "wcaw: %v\n", err)
		os.Exit(2)
	}

	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "wcaw: %v\n", err)
		os.Exit(1)
	}

	resolved, err := history.Resolve(cwd, path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "wcaw: %v\n", err)
		os.Exit(1)
	}

	if ext := strings.ToLower(filepath.Ext(resolved.AbsPath)); ext != ".ts" {
		fmt.Fprintf(os.Stderr, "wcaw: unsupported file extension %q: only .ts is supported in v1\n", ext)
		os.Exit(1)
	}

	source, err := os.ReadFile(resolved.AbsPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "wcaw: read file: %v\n", err)
		os.Exit(1)
	}

	sym, err := locator.Locate(source, symbol)
	if err != nil {
		var nfe *locator.NotFoundError
		if errors.As(err, &nfe) {
			fmt.Fprintf(os.Stderr, "wcaw: %v\n", err)
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "wcaw: %v\n", err)
		os.Exit(1)
	}

	render.ResetColors(stdoutIsTTY())

	c := openCache(*noCache)
	defer c.Close()

	var enumerator history.SymbolEnumerator
	if c != nil {
		enumerator = &cache.ASTEnumerator{C: c, RepoRoot: resolved.RepoRoot}
	}

	commits, err := history.Track(resolved, sym, enumerator)
	if err != nil {
		fmt.Fprintf(os.Stderr, "wcaw: %v\n", err)
		os.Exit(1)
	}

	groups := enrichOrFallback(commits, resolved.Repo(), c)
	decorateTestFiles(resolved.Repo(), groups, resolved.RelPath)

	owner, hasOwner := history.EffectiveOwner(commits)
	in := render.Input{
		Symbol:   sym,
		Path:     resolved.RelPath,
		Groups:   groups,
		Commits:  commits,
		Owner:    owner,
		HasOwner: hasOwner,
		Now:      time.Now(),
	}

	if *jsonOut {
		if err := render.JSON(os.Stdout, in); err != nil {
			fmt.Fprintf(os.Stderr, "wcaw: render: %v\n", err)
			os.Exit(1)
		}
		return
	}

	fmt.Printf("resolved %s at %s:%d-%d (bytes %d-%d)\n\n",
		sym.Name, resolved.RelPath, sym.StartLine, sym.EndLine, sym.StartByte, sym.EndByte)
	if err := render.Human(os.Stdout, in); err != nil {
		fmt.Fprintf(os.Stderr, "wcaw: render: %v\n", err)
		os.Exit(1)
	}
}

// stdoutIsTTY reports whether os.Stdout is attached to a terminal. Uses the
// character-device bit on the file's Stat() — no extra dependency, and good
// enough for the color-on/off decision (pipes and redirects both fail it).
func stdoutIsTTY() bool {
	fi, err := os.Stdout.Stat()
	if err != nil || fi == nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

// decorateTestFiles populates Group.TestFiles for each group via
// history.CollectTestFiles. On failure we degrade silently (empty test lists)
// with one stderr warning, matching the Phase 4 forge fallback pattern.
func decorateTestFiles(repo *git.Repository, groups []forge.Group, trackedRel string) {
	for i := range groups {
		tests, err := history.CollectTestFiles(repo, groups[i].Commits, trackedRel)
		if err != nil {
			fmt.Fprintf(os.Stderr, "wcaw: test-file enrichment failed: %v\n", err)
			return
		}
		groups[i].TestFiles = tests
	}
}

// enrichOrFallback tries to enrich the flat commit list with PR metadata via
// the GitHub forge. Any failure (no remote, init error, mid-walk abort)
// degrades to a single no-PR group containing all commits, with a one-line
// stderr warning so the user knows what happened. When c is non-nil the
// concrete *GitHubForge is wrapped with a read-through cache before being
// passed to GroupCommits.
func enrichOrFallback(commits []history.Commit, repo *git.Repository, c *cache.Cache) []forge.Group {
	flat := []forge.Group{{Pull: nil, Commits: commits}}

	ctx := context.Background()
	fg, ferr := forge.NewGitHubFromRepo(ctx, repo)
	switch {
	case errors.Is(ferr, forge.ErrNoGitHubRemote):
		fmt.Fprintln(os.Stderr, "wcaw: no github remote; showing unenriched history")
		return flat
	case ferr != nil:
		fmt.Fprintf(os.Stderr, "wcaw: forge init failed: %v; showing unenriched history\n", ferr)
		return flat
	}

	var f forge.Forge = fg
	if c != nil {
		f = cache.Wrap(fg, c, fg.Owner(), fg.Repo())
	}

	gs, gerr := forge.GroupCommits(ctx, f, commits)
	if gerr != nil {
		fmt.Fprintf(os.Stderr, "wcaw: github enrichment aborted: %v; showing unenriched history\n", gerr)
		return flat
	}
	return gs
}

// openCache resolves the default cache path and opens it, returning nil
// (caching disabled) when --no-cache is set or anything goes wrong. The
// degradation pattern matches enrichOrFallback / decorateTestFiles: one
// stderr line, continue without the feature. Callers can `defer c.Close()`
// unconditionally — Close on a nil receiver is a no-op.
func openCache(disabled bool) *cache.Cache {
	if disabled {
		return nil
	}
	path, err := cache.DefaultPath()
	if err != nil {
		fmt.Fprintf(os.Stderr, "wcaw: cache disabled: %v\n", err)
		return nil
	}
	c, err := cache.Open(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "wcaw: cache disabled: %v\n", err)
		return nil
	}
	return c
}

func splitArg(arg string) (path, symbol string, err error) {
	i := strings.LastIndex(arg, ":")
	if i < 0 {
		return "", "", fmt.Errorf("invalid argument %q: expected <path>:<symbol>", arg)
	}
	path, symbol = arg[:i], arg[i+1:]
	if path == "" || symbol == "" {
		return "", "", fmt.Errorf("invalid argument %q: expected <path>:<symbol>", arg)
	}
	return path, symbol, nil
}
