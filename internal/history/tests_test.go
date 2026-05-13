package history

import (
	"reflect"
	"testing"
)

func TestIsTestPath(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"src/foo.test.ts", true},
		{"src/foo.spec.ts", true},
		{"foo.test.ts", true},
		{"foo.spec.ts", true},
		{"src/__tests__/foo.ts", true},
		{"__tests__/foo.ts", true},
		{"src/__tests__/nested/foo.ts", true},

		{"src/foo.ts", false},
		{"foo.ts", false},
		{"src/foo.test.tsx", false}, // .tsx deferred
		{"src/foo.spec.js", false},
		{"src/__tests__/foo.css", false}, // non-ts inside __tests__
		{"src/__tests__/foo.tsx", false}, // .tsx deferred
		{"src/tests/foo.ts", false},      // not __tests__
		{"", false},
		{"package.json", false},
	}
	for _, tc := range cases {
		if got := isTestPath(tc.path); got != tc.want {
			t.Errorf("isTestPath(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}

func tracked(t *testing.T, dir, rel, name string) ([]Commit, *Resolved) {
	t.Helper()
	resolved, err := Resolve(dir, rel)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	commits := track(t, dir, rel, name)
	return commits, &resolved
}

func TestCollectTestFiles_GlobsAndExclude(t *testing.T) {
	_, wt, dir := newTestRepo(t)

	// Root commit: source + one matching test + one non-test.
	writeFile(t, dir, "src/foo.ts", "function bar() {\n    return 1;\n}\n")
	writeFile(t, dir, "src/foo.test.ts", "// initial test\n")
	writeFile(t, dir, "src/unrelated.ts", "// not a test\n")
	commitAll(t, wt, "init")

	// Second commit: modify the source, add a spec sibling, add a __tests__ file.
	writeFile(t, dir, "src/foo.ts", "function bar() {\n    return 2;\n}\n")
	writeFile(t, dir, "src/foo.spec.ts", "// spec\n")
	writeFile(t, dir, "src/__tests__/foo.ts", "// __tests__\n")
	writeFile(t, dir, "src/__tests__/foo.tsx", "// .tsx ignored\n")
	commitAll(t, wt, "tweak + tests")

	commits, resolved := tracked(t, dir, "src/foo.ts", "bar")
	if len(commits) != 2 {
		t.Fatalf("got %d commits, want 2", len(commits))
	}

	got, err := CollectTestFiles(resolved.Repo(), commits, resolved.RelPath)
	if err != nil {
		t.Fatalf("CollectTestFiles: %v", err)
	}
	want := []string{
		"src/__tests__/foo.ts",
		"src/foo.spec.ts",
		"src/foo.test.ts",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("CollectTestFiles =\n  %v\nwant\n  %v", got, want)
	}
}

func TestCollectTestFiles_ExcludesTrackedFile(t *testing.T) {
	// If the tracked file itself ends in `.test.ts`, it should not appear in
	// its own test-file list.
	_, wt, dir := newTestRepo(t)

	writeFile(t, dir, "src/foo.test.ts", "function bar() {\n    return 1;\n}\n")
	commitAll(t, wt, "init")

	writeFile(t, dir, "src/foo.test.ts", "function bar() {\n    return 2;\n}\n")
	commitAll(t, wt, "tweak")

	commits, resolved := tracked(t, dir, "src/foo.test.ts", "bar")
	got, err := CollectTestFiles(resolved.Repo(), commits, resolved.RelPath)
	if err != nil {
		t.Fatalf("CollectTestFiles: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %v, want empty (tracked file excluded)", got)
	}
}

func TestCollectTestFiles_RootCommitOnly(t *testing.T) {
	_, wt, dir := newTestRepo(t)

	writeFile(t, dir, "src/foo.ts", "function bar() {\n    return 1;\n}\n")
	writeFile(t, dir, "src/foo.test.ts", "// test\n")
	commitAll(t, wt, "init")

	commits, resolved := tracked(t, dir, "src/foo.ts", "bar")
	if len(commits) != 1 {
		t.Fatalf("got %d commits, want 1", len(commits))
	}

	got, err := CollectTestFiles(resolved.Repo(), commits, resolved.RelPath)
	if err != nil {
		t.Fatalf("CollectTestFiles: %v", err)
	}
	want := []string{"src/foo.test.ts"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestCollectTestFiles_DedupsAcrossCommits(t *testing.T) {
	_, wt, dir := newTestRepo(t)

	writeFile(t, dir, "src/foo.ts", "function bar() {\n    return 1;\n}\n")
	writeFile(t, dir, "src/foo.test.ts", "// v1\n")
	commitAll(t, wt, "init")

	// Touch both files again.
	writeFile(t, dir, "src/foo.ts", "function bar() {\n    return 2;\n}\n")
	writeFile(t, dir, "src/foo.test.ts", "// v2\n")
	commitAll(t, wt, "tweak")

	commits, resolved := tracked(t, dir, "src/foo.ts", "bar")
	got, err := CollectTestFiles(resolved.Repo(), commits, resolved.RelPath)
	if err != nil {
		t.Fatalf("CollectTestFiles: %v", err)
	}
	want := []string{"src/foo.test.ts"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestCollectTestFiles_EmptyWhenNoTests(t *testing.T) {
	_, wt, dir := newTestRepo(t)

	writeFile(t, dir, "src/foo.ts", "function bar() {\n    return 1;\n}\n")
	writeFile(t, dir, "src/helper.ts", "// helper\n")
	commitAll(t, wt, "init")

	commits, resolved := tracked(t, dir, "src/foo.ts", "bar")
	got, err := CollectTestFiles(resolved.Repo(), commits, resolved.RelPath)
	if err != nil {
		t.Fatalf("CollectTestFiles: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %v, want empty", got)
	}
}
