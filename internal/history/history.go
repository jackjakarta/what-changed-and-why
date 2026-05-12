package history

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"

	"github.com/jackjakarta/what-changed-and-why/internal/locator"
)

// Classification is how a commit related to the tracked symbol.
type Classification int

const (
	ClassUnknown Classification = iota
	ClassIntroduced
	ClassModified
	ClassRenamed
	ClassMovedFrom
	ClassUnrelated
)

func (c Classification) String() string {
	switch c {
	case ClassIntroduced:
		return "introduced"
	case ClassModified:
		return "modified"
	case ClassRenamed:
		return "renamed"
	case ClassMovedFrom:
		return "moved-from"
	case ClassUnrelated:
		return "unrelated"
	}
	return "unknown"
}

// SymbolRef carries per-commit positional data populated by Track. It is nil
// on a Commit whose Class is ClassUnrelated (the symbol itself didn't move).
type SymbolRef struct {
	Name       string
	PrevName   string // ClassRenamed: the parent-side name
	SourceFile string // ClassMovedFrom: repo-relative path of the originating file
	StartLine  uint32 // 1-indexed at this commit
	EndLine    uint32
}

type Commit struct {
	Hash    string
	Date    time.Time
	Author  string
	Subject string

	Class  Classification
	Symbol *SymbolRef
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

// Repo returns the underlying go-git repository handle. Exposed so callers
// such as internal/forge can issue their own queries without re-opening the
// repo from RepoRoot.
func (r Resolved) Repo() *git.Repository { return r.repo }

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

// Track walks the history of the given symbol in r, classifying each commit.
// It follows the symbol through in-file renames (AST-shape match) and cross-
// file moves (by scanning sibling files in the moving commit and recursing
// into the source file's history). Returns commits newest-first.
//
// The starting locator.Symbol is taken (rather than just a name) so we don't
// re-locate and risk a different first-wins disambiguation than the CLI's
// "resolved" header reported. Merge commits use first-parent only.
func Track(r Resolved, sym locator.Symbol) ([]Commit, error) {
	if r.repo == nil {
		return nil, errors.New("history.Track: zero Resolved (call Resolve first)")
	}
	inHead, err := fileInHead(r.repo, r.RelPath)
	if err != nil {
		return nil, err
	}
	if !inHead {
		return nil, nil
	}

	repo := r.repo
	file := r.RelPath
	name := sym.Name
	kind := sym.Kind

	var results []Commit
	from := plumbing.ZeroHash

	for {
		flip, terminated, err := trackInFile(repo, file, name, kind, from, &results)
		if err != nil {
			return nil, err
		}
		if terminated {
			break
		}
		if flip != nil {
			file = flip.file
			name = flip.name
			kind = flip.kind
			from = flip.fromHash
			continue
		}
		break
	}

	return results, nil
}

type flipInstruction struct {
	file     string
	name     string
	kind     locator.Kind
	fromHash plumbing.Hash
}

// trackInFile walks repo.Log(FileName: file) starting from `from` (HEAD when
// ZeroHash), classifying each commit and appending to *results. Returns:
//   - flip != nil, terminated == false: cross-file move detected; caller flips state.
//   - terminated == true: ClassIntroduced was emitted or we exhausted history.
func trackInFile(
	repo *git.Repository,
	file, name string, kind locator.Kind,
	from plumbing.Hash,
	results *[]Commit,
) (*flipInstruction, bool, error) {
	logOpts := &git.LogOptions{FileName: &file}
	if from != plumbing.ZeroHash {
		logOpts.From = from
	}
	iter, err := repo.Log(logOpts)
	if err != nil {
		return nil, false, fmt.Errorf("walk %s: %w", file, err)
	}
	defer iter.Close()

	for {
		c, err := iter.Next()
		if errors.Is(err, io.EOF) {
			return nil, true, nil
		}
		if err != nil {
			return nil, false, fmt.Errorf("walk %s: %w", file, err)
		}

		childSrc, childFound, err := readBlobAt(c, file)
		if err != nil {
			return nil, false, err
		}
		if !childFound {
			return nil, true, nil
		}

		childMatches, err := locator.Enumerate(childSrc)
		if err != nil {
			return nil, false, fmt.Errorf("parse %s at %s: %w", file, shortHash(c.Hash), err)
		}
		childSym := findSymbol(childMatches, name, kind)
		if childSym == nil {
			return nil, true, nil
		}
		childBody := normalizeBody(childSrc[childSym.StartByte:childSym.EndByte])

		rec := Commit{
			Hash:    c.Hash.String(),
			Date:    c.Author.When,
			Author:  c.Author.Name,
			Subject: firstLine(c.Message),
		}

		var parent *object.Commit
		if c.NumParents() > 0 {
			parent, err = c.Parent(0)
			if err != nil {
				return nil, false, fmt.Errorf("read parent of %s: %w", shortHash(c.Hash), err)
			}
		}

		if parent == nil {
			rec.Class = ClassIntroduced
			rec.Symbol = &SymbolRef{Name: name, StartLine: childSym.StartLine, EndLine: childSym.EndLine}
			*results = append(*results, rec)
			return nil, true, nil
		}

		parentSrc, parentFound, err := readBlobAt(parent, file)
		if err != nil {
			return nil, false, err
		}

		var parentSym *locator.Symbol
		var renameCand *locator.Symbol
		if parentFound {
			parentMatches, err := locator.Enumerate(parentSrc)
			if err != nil {
				return nil, false, fmt.Errorf("parse %s at %s: %w", file, shortHash(parent.Hash), err)
			}
			parentSym = findSymbol(parentMatches, name, kind)
			if parentSym == nil {
				for i := range parentMatches {
					if parentMatches[i].Kind != kind {
						continue
					}
					other := normalizeBody(parentSrc[parentMatches[i].StartByte:parentMatches[i].EndByte])
					if isRename(name, parentMatches[i].Name, childBody, other) {
						renameCand = &parentMatches[i]
						break
					}
				}
			}
		}

		switch {
		case parentSym != nil:
			parentBody := normalizeBody(parentSrc[parentSym.StartByte:parentSym.EndByte])
			if bytes.Equal(childBody, parentBody) {
				rec.Class = ClassUnrelated
			} else {
				rec.Class = ClassModified
				rec.Symbol = &SymbolRef{Name: name, StartLine: childSym.StartLine, EndLine: childSym.EndLine}
			}
			*results = append(*results, rec)
			continue

		case renameCand != nil:
			rec.Class = ClassRenamed
			rec.Symbol = &SymbolRef{
				Name:      name,
				PrevName:  renameCand.Name,
				StartLine: childSym.StartLine,
				EndLine:   childSym.EndLine,
			}
			*results = append(*results, rec)
			name = renameCand.Name
			continue
		}

		srcPath, srcName, ambiguous, err := scanCrossFileMove(c, parent, file, name, kind, childBody)
		if err != nil {
			return nil, false, err
		}

		if ambiguous {
			rec.Class = ClassIntroduced
			rec.Symbol = &SymbolRef{Name: name, StartLine: childSym.StartLine, EndLine: childSym.EndLine}
			*results = append(*results, rec)
			return nil, true, nil
		}

		if srcPath != "" {
			rec.Class = ClassMovedFrom
			rec.Symbol = &SymbolRef{
				Name:       name,
				SourceFile: srcPath,
				StartLine:  childSym.StartLine,
				EndLine:    childSym.EndLine,
			}
			nextName := name
			if srcName != name {
				rec.Symbol.PrevName = srcName
				nextName = srcName
			}
			*results = append(*results, rec)
			return &flipInstruction{
				file:     srcPath,
				name:     nextName,
				kind:     kind,
				fromHash: parent.Hash,
			}, false, nil
		}

		rec.Class = ClassIntroduced
		rec.Symbol = &SymbolRef{Name: name, StartLine: childSym.StartLine, EndLine: childSym.EndLine}
		*results = append(*results, rec)
		return nil, true, nil
	}
}

// scanCrossFileMove looks for a candidate source file in the changed paths
// between parent and child. Returns (sourcePath, sourceName, ambiguous, err).
// On ambiguity, sourcePath is "" and ambiguous is true; caller should classify
// the commit as Introduced and warn.
func scanCrossFileMove(
	child, parent *object.Commit,
	excludeFile, name string, kind locator.Kind,
	childBody []byte,
) (string, string, bool, error) {
	changed, err := changedPaths(parent, child)
	if err != nil {
		return "", "", false, err
	}

	type candidate struct {
		path        string
		sym         locator.Symbol
		exactName   bool
		deletedAtC  bool
		disappeared bool
	}
	var exact, shape []candidate

	for _, p := range changed {
		if p == excludeFile {
			continue
		}
		if !strings.HasSuffix(p, ".ts") {
			continue
		}

		parentBlob, parentBlobFound, err := readBlobAt(parent, p)
		if err != nil {
			return "", "", false, err
		}
		if !parentBlobFound {
			continue
		}

		matches, err := locator.Enumerate(parentBlob)
		if err != nil {
			continue
		}

		childBlob, childBlobFound, err := readBlobAt(child, p)
		if err != nil {
			return "", "", false, err
		}
		deletedAtC := !childBlobFound
		var childMatches []locator.Symbol
		if !deletedAtC {
			childMatches, _ = locator.Enumerate(childBlob)
		}

		stillIn := func(n string, k locator.Kind) bool {
			for _, cm := range childMatches {
				if cm.Name == n && cm.Kind == k {
					return true
				}
			}
			return false
		}

		for _, m := range matches {
			if m.Kind != kind {
				continue
			}
			if m.Name == name {
				disappeared := deletedAtC || !stillIn(name, kind)
				exact = append(exact, candidate{path: p, sym: m, exactName: true, deletedAtC: deletedAtC, disappeared: disappeared})
				continue
			}
			other := normalizeBody(parentBlob[m.StartByte:m.EndByte])
			if isRename(name, m.Name, childBody, other) {
				disappeared := deletedAtC || !stillIn(m.Name, kind)
				shape = append(shape, candidate{path: p, sym: m, deletedAtC: deletedAtC, disappeared: disappeared})
			}
		}
	}

	pool := exact
	if len(pool) == 0 {
		pool = shape
	}
	if len(pool) == 0 {
		return "", "", false, nil
	}

	winners := pool
	var filt []candidate
	for _, c := range winners {
		if c.deletedAtC {
			filt = append(filt, c)
		}
	}
	if len(filt) >= 1 {
		winners = filt
	} else {
		filt = nil
		for _, c := range winners {
			if c.disappeared {
				filt = append(filt, c)
			}
		}
		if len(filt) >= 1 {
			winners = filt
		}
	}

	if len(winners) > 1 {
		paths := make([]string, 0, len(winners))
		for _, c := range winners {
			paths = append(paths, c.path)
		}
		fmt.Fprintf(os.Stderr, "wcaw: ambiguous move at %s: candidates %s\n",
			shortHash(child.Hash), strings.Join(paths, ", "))
		return "", "", true, nil
	}

	return winners[0].path, winners[0].sym.Name, false, nil
}

func findSymbol(syms []locator.Symbol, name string, kind locator.Kind) *locator.Symbol {
	for i := range syms {
		if syms[i].Name == name && syms[i].Kind == kind {
			return &syms[i]
		}
	}
	return nil
}

func readBlobAt(commit *object.Commit, relPath string) ([]byte, bool, error) {
	f, err := commit.File(relPath)
	if err != nil {
		if errors.Is(err, object.ErrFileNotFound) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("read %s at %s: %w", relPath, shortHash(commit.Hash), err)
	}
	contents, err := f.Contents()
	if err != nil {
		return nil, false, fmt.Errorf("read %s at %s: %w", relPath, shortHash(commit.Hash), err)
	}
	return []byte(contents), true, nil
}

func changedPaths(parent, child *object.Commit) ([]string, error) {
	parentTree, err := parent.Tree()
	if err != nil {
		return nil, fmt.Errorf("parent tree: %w", err)
	}
	childTree, err := child.Tree()
	if err != nil {
		return nil, fmt.Errorf("child tree: %w", err)
	}
	diff, err := object.DiffTree(parentTree, childTree)
	if err != nil {
		return nil, fmt.Errorf("diff trees: %w", err)
	}

	seen := make(map[string]struct{})
	var paths []string
	add := func(name string) {
		if name == "" {
			return
		}
		if _, ok := seen[name]; ok {
			return
		}
		seen[name] = struct{}{}
		paths = append(paths, name)
	}
	for _, ch := range diff {
		add(ch.From.Name)
		add(ch.To.Name)
	}
	return paths, nil
}

func normalizeBody(b []byte) []byte {
	lines := bytes.Split(b, []byte("\n"))
	for i, l := range lines {
		lines[i] = bytes.TrimRight(l, " \t\r")
	}
	out := bytes.Join(lines, []byte("\n"))
	out = bytes.TrimSuffix(out, []byte("\n"))
	return out
}

// isRename gates a possible rename through both a name-similarity check and a
// body-similarity check against the originating commit's body.
func isRename(targetName, candName string, targetBody, candBody []byte) bool {
	nameLen := len(targetName)
	if len(candName) > nameLen {
		nameLen = len(candName)
	}
	if locator.Levenshtein(targetName, candName) > nameLen/2 {
		return false
	}
	longer := len(targetBody)
	if len(candBody) > longer {
		longer = len(candBody)
	}
	th := longer / 8
	if th < 4 {
		th = 4
	}
	if th > 20 {
		th = 20
	}
	return locator.Levenshtein(string(targetBody), string(candBody)) <= th
}

func shortHash(h plumbing.Hash) string {
	s := h.String()
	if len(s) >= 7 {
		return s[:7]
	}
	return s
}
