package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-git/go-git/v5"

	"github.com/jackjakarta/what-changed-and-why/internal/forge"
	"github.com/jackjakarta/what-changed-and-why/internal/history"
	"github.com/jackjakarta/what-changed-and-why/internal/locator"
)

const usage = `usage: wcaw <path>:<symbol>

example:
  wcaw src/auth/login.ts:validateToken
`

func main() {
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

	fmt.Printf("resolved %s at %s:%d-%d (bytes %d-%d)\n\n",
		sym.Name, resolved.RelPath, sym.StartLine, sym.EndLine, sym.StartByte, sym.EndByte)

	commits, err := history.Track(resolved, sym)
	if err != nil {
		fmt.Fprintf(os.Stderr, "wcaw: %v\n", err)
		os.Exit(1)
	}

	groups := enrichOrFallback(commits, resolved.Repo())
	decorateTestFiles(resolved.Repo(), groups, resolved.RelPath)
	renderGroups(os.Stdout, groups)
	renderOwner(os.Stdout, commits)
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

// renderOwner prints the "Effective owner" footer. Suppressed when no commit
// in the flat list qualifies (e.g. all-ClassUnrelated history).
func renderOwner(w io.Writer, commits []history.Commit) {
	owner, ok := history.EffectiveOwner(commits)
	if !ok {
		return
	}
	fmt.Fprintf(w, "\nEffective owner: %s (%d%% of commits, last-touched %s)\n",
		owner.Name, owner.Percent(), owner.LastTouched.Format("2006-01-02"))
}

// enrichOrFallback tries to enrich the flat commit list with PR metadata via
// the GitHub forge. Any failure (no remote, init error, mid-walk abort)
// degrades to a single no-PR group containing all commits, with a one-line
// stderr warning so the user knows what happened.
func enrichOrFallback(commits []history.Commit, repo *git.Repository) []forge.Group {
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

	gs, gerr := forge.GroupCommits(ctx, fg, commits)
	if gerr != nil {
		fmt.Fprintf(os.Stderr, "wcaw: github enrichment aborted: %v; showing unenriched history\n", gerr)
		return flat
	}
	return gs
}

// renderGroups prints each Group on its own block: a PR header line (or
// "(no PR)") followed by the existing tab-separated commit lines indented
// two spaces, plus a "tests:" footer when the group touched any test files.
// Phase 6 will replace this with the polished timeline.
func renderGroups(w io.Writer, groups []forge.Group) {
	for _, g := range groups {
		fmt.Fprintln(w, headerLine(g.Pull))
		for _, c := range g.Commits {
			fmt.Fprintf(w, "  %s\t%s\t%s\t%s\t%s\n",
				c.Hash[:7],
				c.Date.Format("2006-01-02"),
				c.Author,
				classificationLabel(c),
				c.Subject,
			)
		}
		if len(g.TestFiles) > 0 {
			fmt.Fprintf(w, "  tests: %s\n", strings.Join(g.TestFiles, ", "))
		}
	}
}

func headerLine(p *forge.Pull) string {
	if p == nil {
		return "(no PR)"
	}
	parts := []string{fmt.Sprintf("PR #%d %q", p.Number, p.Title)}
	if p.Author != "" {
		parts = append(parts, "@"+p.Author)
	}
	if len(p.Issues) > 0 {
		raws := make([]string, 0, len(p.Issues))
		for _, ir := range p.Issues {
			raws = append(raws, ir.Raw)
		}
		parts = append(parts, "(issues: "+strings.Join(raws, ", ")+")")
	}
	return strings.Join(parts, "  ")
}

func classificationLabel(c history.Commit) string {
	label := c.Class.String()
	if c.Symbol == nil {
		return label
	}
	switch c.Class {
	case history.ClassRenamed:
		if c.Symbol.PrevName != "" {
			return fmt.Sprintf("%s (from %s)", label, c.Symbol.PrevName)
		}
	case history.ClassMovedFrom:
		if c.Symbol.SourceFile != "" {
			if c.Symbol.PrevName != "" && c.Symbol.PrevName != c.Symbol.Name {
				return fmt.Sprintf("%s %s (as %s)", label, c.Symbol.SourceFile, c.Symbol.PrevName)
			}
			return fmt.Sprintf("%s %s", label, c.Symbol.SourceFile)
		}
	}
	return label
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
