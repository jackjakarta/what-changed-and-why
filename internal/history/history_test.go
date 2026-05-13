package history

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"

	"github.com/jackjakarta/what-changed-and-why/internal/locator"
)

// newTestRepo initialises an empty git repository in t.TempDir() and returns
// the repo, its worktree, and the on-disk path. Test files are written under
// this path.
func newTestRepo(t *testing.T) (*git.Repository, *git.Worktree, string) {
	t.Helper()
	dir := t.TempDir()
	repo, err := git.PlainInit(dir, false)
	if err != nil {
		t.Fatalf("git init: %v", err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatalf("worktree: %v", err)
	}
	return repo, wt, dir
}

func writeFile(t *testing.T, root, rel, contents string) {
	t.Helper()
	full := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(full, []byte(contents), 0o644); err != nil {
		t.Fatalf("write %s: %v", rel, err)
	}
}

func removeFile(t *testing.T, root, rel string) {
	t.Helper()
	if err := os.Remove(filepath.Join(root, rel)); err != nil {
		t.Fatalf("remove %s: %v", rel, err)
	}
}

// commitAll stages all paths and creates a commit. go-git requires an explicit
// Author signature, so we pin one here for determinism.
func commitAll(t *testing.T, wt *git.Worktree, msg string) {
	t.Helper()
	if err := wt.AddGlob("."); err != nil {
		t.Fatalf("add: %v", err)
	}
	_, err := wt.Commit(msg, &git.CommitOptions{
		Author: &object.Signature{
			Name:  "Test",
			Email: "test@example.com",
			When:  time.Now(),
		},
		AllowEmptyCommits: false,
	})
	if err != nil {
		t.Fatalf("commit %q: %v", msg, err)
	}
}

func track(t *testing.T, dir, rel, name string) []Commit {
	t.Helper()
	resolved, err := Resolve(dir, rel)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	src, err := os.ReadFile(resolved.AbsPath)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	sym, err := locator.Locate(src, name)
	if err != nil {
		t.Fatalf("locate %s: %v", name, err)
	}
	commits, err := Track(resolved, sym, nil)
	if err != nil {
		t.Fatalf("track: %v", err)
	}
	return commits
}

func classes(commits []Commit) []Classification {
	out := make([]Classification, len(commits))
	for i, c := range commits {
		out[i] = c.Class
	}
	return out
}

func TestTrackLifecycle_IntroduceThenModify(t *testing.T) {
	_, wt, dir := newTestRepo(t)

	writeFile(t, dir, "foo.ts", "function bar() {\n    return 1;\n}\n")
	commitAll(t, wt, "add bar")

	writeFile(t, dir, "foo.ts", "function bar() {\n    return 2;\n}\n")
	commitAll(t, wt, "tweak bar")

	got := track(t, dir, "foo.ts", "bar")
	want := []Classification{ClassModified, ClassIntroduced}
	if !equalClasses(got, want) {
		t.Fatalf("classes = %v, want %v", classes(got), want)
	}
	if got[0].Symbol == nil || got[0].Symbol.Name != "bar" {
		t.Errorf("modified row missing Symbol or Name: %+v", got[0].Symbol)
	}
	if got[1].Symbol == nil || got[1].Symbol.Name != "bar" {
		t.Errorf("introduced row missing Symbol or Name: %+v", got[1].Symbol)
	}
}

func TestTrackUnrelated_FileTouchedSymbolUnchanged(t *testing.T) {
	_, wt, dir := newTestRepo(t)

	writeFile(t, dir, "foo.ts", "function bar() {\n    return 1;\n}\n\nfunction baz() {\n    return 10;\n}\n")
	commitAll(t, wt, "add bar and baz")

	// Touch baz, leave bar alone.
	writeFile(t, dir, "foo.ts", "function bar() {\n    return 1;\n}\n\nfunction baz() {\n    return 999;\n}\n")
	commitAll(t, wt, "tweak baz")

	got := track(t, dir, "foo.ts", "bar")
	want := []Classification{ClassUnrelated, ClassIntroduced}
	if !equalClasses(got, want) {
		t.Fatalf("classes = %v, want %v", classes(got), want)
	}
	if got[0].Symbol != nil {
		t.Errorf("unrelated row should have nil Symbol, got %+v", got[0].Symbol)
	}
}

func TestTrackRename_NameOnly(t *testing.T) {
	_, wt, dir := newTestRepo(t)

	writeFile(t, dir, "foo.ts", "function validateToken() {\n    return checkSig() && checkExp();\n}\n")
	commitAll(t, wt, "add validateToken")

	writeFile(t, dir, "foo.ts", "function validateAuthToken() {\n    return checkSig() && checkExp();\n}\n")
	commitAll(t, wt, "rename validateToken -> validateAuthToken")

	got := track(t, dir, "foo.ts", "validateAuthToken")
	want := []Classification{ClassRenamed, ClassIntroduced}
	if !equalClasses(got, want) {
		t.Fatalf("classes = %v, want %v", classes(got), want)
	}
	if got[0].Symbol == nil || got[0].Symbol.PrevName != "validateToken" {
		t.Errorf("renamed row Symbol.PrevName = %v, want validateToken", got[0].Symbol)
	}
}

func TestTrackRename_WithBodyTweak(t *testing.T) {
	_, wt, dir := newTestRepo(t)

	writeFile(t, dir, "foo.ts", "function validateToken() {\n    return checkSig() && checkExp();\n}\n")
	commitAll(t, wt, "add validateToken")

	// Rename + small body tweak.
	writeFile(t, dir, "foo.ts", "function validateAuthToken() {\n    return checkSig() && checkExpiry();\n}\n")
	commitAll(t, wt, "rename and tweak")

	got := track(t, dir, "foo.ts", "validateAuthToken")
	want := []Classification{ClassRenamed, ClassIntroduced}
	if !equalClasses(got, want) {
		t.Fatalf("classes = %v, want %v", classes(got), want)
	}
	if got[0].Symbol == nil || got[0].Symbol.PrevName != "validateToken" {
		t.Errorf("renamed row Symbol.PrevName = %v, want validateToken", got[0].Symbol)
	}
}

func TestTrackCrossFileMove(t *testing.T) {
	_, wt, dir := newTestRepo(t)

	writeFile(t, dir, "a.ts", "function bar() {\n    return 42;\n}\n")
	commitAll(t, wt, "add bar in a.ts")

	// Move bar from a.ts to b.ts in one commit.
	removeFile(t, dir, "a.ts")
	writeFile(t, dir, "b.ts", "function bar() {\n    return 42;\n}\n")
	commitAll(t, wt, "move bar to b.ts")

	got := track(t, dir, "b.ts", "bar")
	want := []Classification{ClassMovedFrom, ClassIntroduced}
	if !equalClasses(got, want) {
		t.Fatalf("classes = %v, want %v", classes(got), want)
	}
	if got[0].Symbol == nil || got[0].Symbol.SourceFile != "a.ts" {
		t.Errorf("moved-from row Symbol.SourceFile = %v, want a.ts", got[0].Symbol)
	}
}

func equalClasses(commits []Commit, want []Classification) bool {
	if len(commits) != len(want) {
		return false
	}
	for i, c := range commits {
		if c.Class != want[i] {
			return false
		}
	}
	return true
}

// countingEnumerator wraps locator.Enumerate so a test can assert how many
// blobs the second Track pass had to re-parse.
type countingEnumerator struct {
	cache map[string][]locator.Symbol
	calls int
}

func (e *countingEnumerator) Enumerate(commitSHA, filePath string, blob []byte) ([]locator.Symbol, error) {
	key := commitSHA + "|" + filePath
	if syms, ok := e.cache[key]; ok {
		return syms, nil
	}
	e.calls++
	syms, err := locator.Enumerate(blob)
	if err != nil {
		return nil, err
	}
	if e.cache == nil {
		e.cache = make(map[string][]locator.Symbol)
	}
	e.cache[key] = syms
	return syms, nil
}

func TestTrack_SymbolEnumeratorCachesAcrossRuns(t *testing.T) {
	_, wt, dir := newTestRepo(t)

	writeFile(t, dir, "foo.ts", "function bar() {\n    return 1;\n}\n")
	commitAll(t, wt, "add bar")
	writeFile(t, dir, "foo.ts", "function bar() {\n    return 2;\n}\n")
	commitAll(t, wt, "tweak bar")
	writeFile(t, dir, "foo.ts", "function bar() {\n    return 3;\n}\n")
	commitAll(t, wt, "tweak bar again")

	resolved, err := Resolve(dir, "foo.ts")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	src, err := os.ReadFile(resolved.AbsPath)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	sym, err := locator.Locate(src, "bar")
	if err != nil {
		t.Fatalf("locate: %v", err)
	}

	enum := &countingEnumerator{}

	first, err := Track(resolved, sym, enum)
	if err != nil {
		t.Fatalf("first track: %v", err)
	}
	firstCalls := enum.calls
	if firstCalls == 0 {
		t.Fatalf("first run should have parsed at least once, got 0")
	}

	second, err := Track(resolved, sym, enum)
	if err != nil {
		t.Fatalf("second track: %v", err)
	}
	if enum.calls != firstCalls {
		t.Errorf("second run reparsed %d blobs (total calls %d, expected %d)", enum.calls-firstCalls, enum.calls, firstCalls)
	}
	if len(first) != len(second) {
		t.Fatalf("commit count drift: first=%d second=%d", len(first), len(second))
	}
	for i := range first {
		if first[i].Hash != second[i].Hash || first[i].Class != second[i].Class {
			t.Errorf("row %d drifted: first=%+v second=%+v", i, first[i], second[i])
		}
	}
}
