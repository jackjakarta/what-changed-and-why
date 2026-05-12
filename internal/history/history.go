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

func WalkFile(cwd, userPath string) ([]Commit, error) {
	absPath, err := filepath.Abs(filepath.Join(cwd, userPath))
	if err != nil {
		return nil, fmt.Errorf("resolve path: %w", err)
	}
	absPath = filepath.Clean(absPath)

	repoRoot, err := findRepoRoot(absPath)
	if err != nil {
		return nil, err
	}

	repo, err := git.PlainOpen(repoRoot)
	if err != nil {
		return nil, fmt.Errorf("open repo at %s: %w", repoRoot, err)
	}

	relPath, err := filepath.Rel(repoRoot, absPath)
	if err != nil {
		return nil, fmt.Errorf("compute repo-relative path: %w", err)
	}
	relPath = filepath.ToSlash(relPath)

	if err := ensureFileInHead(repo, relPath); err != nil {
		return nil, err
	}

	iter, err := repo.Log(&git.LogOptions{FileName: &relPath})
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

func ensureFileInHead(repo *git.Repository, relPath string) error {
	head, err := repo.Head()
	if err != nil {
		return fmt.Errorf("resolve HEAD: %w", err)
	}
	commit, err := repo.CommitObject(head.Hash())
	if err != nil {
		return fmt.Errorf("read HEAD commit: %w", err)
	}
	tree, err := commit.Tree()
	if err != nil {
		return fmt.Errorf("read HEAD tree: %w", err)
	}
	if _, err := tree.FindEntry(relPath); err != nil {
		if errors.Is(err, object.ErrEntryNotFound) || errors.Is(err, object.ErrDirectoryNotFound) {
			return fmt.Errorf("file not found in repo at HEAD: %s", relPath)
		}
		return fmt.Errorf("look up file in HEAD: %w", err)
	}
	return nil
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimRight(s[:i], "\r")
	}
	return s
}
