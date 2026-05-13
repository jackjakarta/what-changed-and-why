package summarize

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/jackjakarta/what-changed-and-why/internal/forge"
	"github.com/jackjakarta/what-changed-and-why/internal/history"
	"github.com/jackjakarta/what-changed-and-why/internal/locator"
)

func TestBuildPromptPR(t *testing.T) {
	b := GroupBrief{
		PRNumber:   142,
		Title:      "Initial JWT validation",
		Body:       "Adds the first cut of validateToken.\n\nCloses SEC-44.",
		Author:     "maria",
		Issues:     []forge.IssueRef{{Raw: "SEC-44", Project: "SEC", Number: 44}},
		Commits:    []history.Commit{{Subject: "feat: introduce validateToken"}, {Subject: "fix: clock skew"}},
		TestFiles:  []string{"src/auth/login.test.ts"},
		SymbolName: "validateToken",
		SymbolKind: "function",
	}
	sys, user := buildPrompt(b)
	if !strings.Contains(sys, "validateToken") {
		t.Errorf("system prompt missing symbol name: %s", sys)
	}
	if !strings.Contains(sys, "function") {
		t.Errorf("system prompt missing kind: %s", sys)
	}
	if !strings.Contains(user, "PR #142 \"Initial JWT validation\"") {
		t.Errorf("user prompt missing PR header: %s", user)
	}
	if !strings.Contains(user, "by @maria") {
		t.Errorf("user prompt missing author: %s", user)
	}
	if !strings.Contains(user, "Adds the first cut of validateToken") {
		t.Errorf("user prompt missing body: %s", user)
	}
	if !strings.Contains(user, "- feat: introduce validateToken") {
		t.Errorf("user prompt missing commit subjects: %s", user)
	}
	if !strings.Contains(user, "Tests touched: src/auth/login.test.ts") {
		t.Errorf("user prompt missing tests: %s", user)
	}
	if !strings.Contains(user, "Linked issues: SEC-44") {
		t.Errorf("user prompt missing issues: %s", user)
	}
}

func TestBuildPromptNoPR(t *testing.T) {
	b := GroupBrief{
		Commits:    []history.Commit{{Subject: "drive-by typo fix"}},
		SymbolName: "validateToken",
		SymbolKind: "function",
	}
	sys, user := buildPrompt(b)
	if !strings.Contains(sys, "not attached to any pull request") {
		t.Errorf("no-PR system prompt missing distinguishing text: %s", sys)
	}
	if strings.Contains(user, "PR #") {
		t.Errorf("no-PR user prompt should not name a PR: %s", user)
	}
	if !strings.Contains(user, "- drive-by typo fix") {
		t.Errorf("no-PR user prompt missing commit: %s", user)
	}
}

func TestBuildPromptTruncatesBody(t *testing.T) {
	big := strings.Repeat("x", maxBodyChars*2)
	b := GroupBrief{
		PRNumber:   1,
		Title:      "t",
		Body:       big,
		SymbolName: "f",
	}
	_, user := buildPrompt(b)
	xs := strings.Count(user, "x")
	if xs > maxBodyChars {
		t.Errorf("body not truncated: %d x's in prompt", xs)
	}
}

func TestBuildPromptCapsCommitList(t *testing.T) {
	commits := make([]history.Commit, maxCommitSubjects+5)
	for i := range commits {
		commits[i] = history.Commit{Subject: "commit"}
	}
	b := GroupBrief{PRNumber: 1, Title: "t", Commits: commits, SymbolName: "f"}
	_, user := buildPrompt(b)
	if !strings.Contains(user, "...and 5 more") {
		t.Errorf("expected overflow marker, got: %s", user)
	}
}

func TestPostProcess(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"", ""},
		{"   ", ""},
		{"a clean line", "a clean line"},
		{"\"quoted summary\"", "quoted summary"},
		{"'single quoted'", "single quoted"},
		{"`backticked`", "backticked"},
		{"“curly quoted”", "curly quoted"},
		{"first line\nsecond line", "first line"},
		{"trailing dot.", "trailing dot"},
		{"trailing dots...", "trailing dots"},
		{"   spaced   ", "spaced"},
	}
	for _, tc := range cases {
		got := postProcess(tc.in)
		if got != tc.want {
			t.Errorf("postProcess(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestPostProcessLengthCap(t *testing.T) {
	long := strings.Repeat("a", maxSummaryChars+10)
	got := postProcess(long)
	if len([]rune(got)) != maxSummaryChars {
		t.Errorf("expected rune length %d, got %d (%q)", maxSummaryChars, len([]rune(got)), got)
	}
	if !strings.HasSuffix(got, "…") {
		t.Errorf("expected ellipsis suffix, got %q", got)
	}
}

// fakeSummarizer is a test stub recording calls and returning canned results.
type fakeSummarizer struct {
	calls    int
	err      error
	errUntil int  // first errUntil calls error, the rest succeed
	summary  string
}

func (f *fakeSummarizer) Summarize(_ context.Context, _ GroupBrief) (string, error) {
	f.calls++
	if f.err != nil && f.calls <= f.errUntil {
		return "", f.err
	}
	return f.summary, nil
}

func TestDecorateGroupsHappyPath(t *testing.T) {
	fake := &fakeSummarizer{summary: "tightened expiry"}
	groups := []forge.Group{
		{Pull: &forge.Pull{PullRef: forge.PullRef{Number: 1, Title: "x"}}, Commits: []history.Commit{{Hash: "a"}}},
		{Pull: &forge.Pull{PullRef: forge.PullRef{Number: 2, Title: "y"}}, Commits: []history.Commit{{Hash: "b"}}},
		{Pull: nil, Commits: []history.Commit{{Hash: "c"}}},
	}
	captureStderr(t, func() {
		DecorateGroups(context.Background(), fake, groups, locator.Symbol{Name: "validateToken", Kind: locator.KindFunction})
	})
	if fake.calls != 3 {
		t.Fatalf("expected 3 summarize calls, got %d", fake.calls)
	}
	for i, g := range groups {
		if g.Summary != "tightened expiry" {
			t.Errorf("group %d Summary = %q, want %q", i, g.Summary, "tightened expiry")
		}
	}
}

func TestDecorateGroupsAbortsAfterThreeConsecutiveErrors(t *testing.T) {
	fake := &fakeSummarizer{err: errors.New("boom"), errUntil: 100}
	groups := make([]forge.Group, 6)
	for i := range groups {
		groups[i] = forge.Group{
			Pull:    &forge.Pull{PullRef: forge.PullRef{Number: i + 1, Title: "p"}},
			Commits: []history.Commit{{Hash: "x"}},
		}
	}
	stderr := captureStderr(t, func() {
		DecorateGroups(context.Background(), fake, groups, locator.Symbol{Name: "f"})
	})
	if fake.calls != 3 {
		t.Fatalf("expected exactly 3 calls before abort, got %d", fake.calls)
	}
	for i, g := range groups {
		if g.Summary != "" {
			t.Errorf("group %d should have empty Summary, got %q", i, g.Summary)
		}
	}
	if !strings.Contains(stderr, "3 consecutive summary failures") {
		t.Errorf("expected abort message in stderr, got: %s", stderr)
	}
}

func TestDecorateGroupsResetsCounterOnSuccess(t *testing.T) {
	// 2 errors, then success, then more errors — abort should NOT fire.
	fake := &fakeSummarizer{err: errors.New("boom"), errUntil: 2, summary: "ok"}
	groups := make([]forge.Group, 5)
	for i := range groups {
		groups[i] = forge.Group{
			Pull:    &forge.Pull{PullRef: forge.PullRef{Number: i + 1, Title: "p"}},
			Commits: []history.Commit{{Hash: "x"}},
		}
	}
	captureStderr(t, func() {
		DecorateGroups(context.Background(), fake, groups, locator.Symbol{Name: "f"})
	})
	if fake.calls != 5 {
		t.Fatalf("expected 5 calls (no abort), got %d", fake.calls)
	}
}

func TestDecorateGroupsNilSummarizer(t *testing.T) {
	groups := []forge.Group{{Pull: nil, Commits: []history.Commit{{Hash: "x"}}}}
	DecorateGroups(context.Background(), nil, groups, locator.Symbol{Name: "f"})
	if groups[0].Summary != "" {
		t.Errorf("expected empty Summary, got %q", groups[0].Summary)
	}
}

func TestBuildBrief(t *testing.T) {
	p := &forge.Pull{
		PullRef: forge.PullRef{Number: 7, Title: "t", Body: "b", Author: "a"},
		Issues:  []forge.IssueRef{{Raw: "#3"}},
	}
	g := forge.Group{
		Pull:      p,
		Commits:   []history.Commit{{Hash: "x"}},
		TestFiles: []string{"t.test.ts"},
	}
	sym := locator.Symbol{Name: "fn", Kind: locator.KindFunction}
	b := BuildBrief(g, sym)
	if b.PRNumber != 7 || b.Title != "t" || b.Body != "b" || b.Author != "a" {
		t.Errorf("brief PR fields wrong: %+v", b)
	}
	if b.SymbolName != "fn" || b.SymbolKind != "function" {
		t.Errorf("brief symbol fields wrong: name=%q kind=%q", b.SymbolName, b.SymbolKind)
	}
	if len(b.Commits) != 1 || len(b.TestFiles) != 1 || len(b.Issues) != 1 {
		t.Errorf("brief slice fields wrong: %+v", b)
	}
}

// captureStderr redirects os.Stderr around fn and returns whatever was
// written. Tests rely on this to assert on degradation warnings.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	orig := os.Stderr
	os.Stderr = w
	done := make(chan string, 1)
	go func() {
		bytes, _ := io.ReadAll(r)
		done <- string(bytes)
	}()
	fn()
	w.Close()
	os.Stderr = orig
	return <-done
}
