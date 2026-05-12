package history

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
)

type Commit struct {
	Hash    string
	Date    time.Time
	Author  string
	Subject string
}

// Resolved holds the result of locating a user-supplied path inside its
// enclosing git repository. WalkResolved consumes one of these so callers
// that already needed the absolute path (e.g. to read the file from disk)
// don't repeat the work.
type Resolved struct {
	AbsPath  string
	RepoRoot string
	RelPath  string

	repo *git.Repository
}

// Resolve turns a user-supplied path into an absolute path, repo root, and
// repo-relative path. It does not check whether the file is tracked at HEAD;
// the locator runs on the working tree, and history walks tolerate untracked
// working-tree files (they just produce an empty list).
func Resolve(cwd, userPath string) (Resolved, error) {
	absPath, err := filepath.Abs(filepath.Join(cwd, userPath))
	if err != nil {
		return Resolved{}, fmt.Errorf("resolve path: %w", err)
	}
	absPath = filepath.Clean(absPath)

	repoRoot, err := findRepoRoot(absPath)
	if err != nil {
		return Resolved{}, err
	}

	repo, err := git.PlainOpen(repoRoot)
	if err != nil {
		return Resolved{}, fmt.Errorf("open repo at %s: %w", repoRoot, err)
	}

	relPath, err := filepath.Rel(repoRoot, absPath)
	if err != nil {
		return Resolved{}, fmt.Errorf("compute repo-relative path: %w", err)
	}
	relPath = filepath.ToSlash(relPath)

	return Resolved{AbsPath: absPath, RepoRoot: repoRoot, RelPath: relPath, repo: repo}, nil
}

// WalkResolved walks every commit that touched the resolved file and returns
// them newest-first (matching `git log` order). If the file is not tracked at
// HEAD (e.g. a brand-new working-tree file), the returned slice is empty —
// that's not an error, just an absence of history.
func WalkResolved(r Resolved) ([]Commit, error) {
	if r.repo == nil {
		return nil, errors.New("history.WalkResolved: zero Resolved (call Resolve first)")
	}
	inHead, err := fileInHead(r.repo, r.RelPath)
	if err != nil {
		return nil, err
	}
	if !inHead {
		return nil, nil
	}
	relPath := r.RelPath
	iter, err := r.repo.Log(&git.LogOptions{FileName: &relPath})
	if err != nil {
		return nil, fmt.Errorf("walk history: %w", err)
	}
	defer iter.Close()

	var commits []Commit
	for {
		c, err := iter.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("walk history: %w", err)
		}
		commits = append(commits, Commit{
			Hash:    c.Hash.String(),
			Date:    c.Author.When,
			Author:  c.Author.Name,
			Subject: firstLine(c.Message),
		})
	}
	return commits, nil
}

// WalkFile is a convenience over Resolve + WalkResolved for callers that
// don't need the absolute path themselves.
func WalkFile(cwd, userPath string) ([]Commit, error) {
	r, err := Resolve(cwd, userPath)
	if err != nil {
		return nil, err
	}
	return WalkResolved(r)
}

func findRepoRoot(start string) (string, error) {
	dir := start
	info, err := os.Stat(dir)
	if err == nil && !info.IsDir() {
		dir = filepath.Dir(dir)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("not inside a git repository")
		}
		dir = parent
	}
}

// fileInHead reports whether relPath exists in the HEAD commit tree.
// Returns false (without error) when the file is simply untracked at HEAD;
// the caller can decide whether that's worth surfacing.
func fileInHead(repo *git.Repository, relPath string) (bool, error) {
	head, err := repo.Head()
	if err != nil {
		return false, fmt.Errorf("resolve HEAD: %w", err)
	}
	commit, err := repo.CommitObject(head.Hash())
	if err != nil {
		return false, fmt.Errorf("read HEAD commit: %w", err)
	}
	tree, err := commit.Tree()
	if err != nil {
		return false, fmt.Errorf("read HEAD tree: %w", err)
	}
	if _, err := tree.FindEntry(relPath); err != nil {
		if errors.Is(err, object.ErrEntryNotFound) || errors.Is(err, object.ErrDirectoryNotFound) {
			return false, nil
		}
		return false, fmt.Errorf("look up file in HEAD: %w", err)
	}
	return true, nil
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimRight(s[:i], "\r")
	}
	return s
}
