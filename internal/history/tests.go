package history

import (
	"errors"
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// CollectTestFiles returns the deduped, sorted union of repo-relative paths
// that look like TypeScript tests (see isTestPath) and that were touched by
// any commit in `commits`, compared against each commit's first parent (root
// commits enumerate their whole tree). `exclude` is dropped from the result,
// typically the tracked file itself.
//
// A missing commit is treated as fatal — the input should come straight from
// Track/WalkResolved, whose hashes are guaranteed present in the repo.
func CollectTestFiles(repo *git.Repository, commits []Commit, exclude string) ([]string, error) {
	if repo == nil {
		return nil, errors.New("history.CollectTestFiles: nil repo")
	}
	seen := make(map[string]struct{})

	for _, c := range commits {
		commit, err := repo.CommitObject(plumbing.NewHash(c.Hash))
		if err != nil {
			return nil, fmt.Errorf("commit %s: %w", shortHashStr(c.Hash), err)
		}
		paths, err := commitChangedPaths(commit)
		if err != nil {
			return nil, err
		}
		for _, p := range paths {
			if p == exclude {
				continue
			}
			if !isTestPath(p) {
				continue
			}
			seen[p] = struct{}{}
		}
	}

	if len(seen) == 0 {
		return nil, nil
	}
	out := make([]string, 0, len(seen))
	for p := range seen {
		out = append(out, p)
	}
	sort.Strings(out)
	return out, nil
}

// commitChangedPaths returns the paths touched between commit and its first
// parent. For a root commit (no parent), every path in the tree is reported
// as added.
func commitChangedPaths(commit *object.Commit) ([]string, error) {
	if commit.NumParents() == 0 {
		tree, err := commit.Tree()
		if err != nil {
			return nil, fmt.Errorf("root tree: %w", err)
		}
		var paths []string
		err = tree.Files().ForEach(func(f *object.File) error {
			paths = append(paths, f.Name)
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("enumerate root tree: %w", err)
		}
		return paths, nil
	}
	parent, err := commit.Parent(0)
	if err != nil {
		return nil, fmt.Errorf("first parent: %w", err)
	}
	return changedPaths(parent, commit)
}

// isTestPath matches the v1 test-file glob set: basename ends in `.test.ts`
// or `.spec.ts`, or any segment of the path is `__tests__` and the file is a
// `.ts`. `.tsx` is deliberately excluded (the .tsx grammar is not wired up
// in v1).
func isTestPath(p string) bool {
	if p == "" {
		return false
	}
	base := path.Base(p)
	if strings.HasSuffix(base, ".test.ts") || strings.HasSuffix(base, ".spec.ts") {
		return true
	}
	if !strings.HasSuffix(base, ".ts") {
		return false
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == "__tests__" {
			return true
		}
	}
	return false
}

func shortHashStr(s string) string {
	if len(s) > 7 {
		return s[:7]
	}
	return s
}
