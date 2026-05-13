package cache

import (
	"context"
	"errors"
	"testing"

	"github.com/jackjakarta/what-changed-and-why/internal/history"
	"github.com/jackjakarta/what-changed-and-why/internal/summarize"
)

// fakeSummarizer is the cache-test counterpart of internal/summarize's stub.
// We can't reuse that one because it lives in a separate test package.
type fakeSummarizer struct {
	calls   int
	summary string
	err     error
}

func (f *fakeSummarizer) Summarize(_ context.Context, _ summarize.GroupBrief) (string, error) {
	f.calls++
	if f.err != nil {
		return "", f.err
	}
	return f.summary, nil
}

func prBrief(num int, sym string) summarize.GroupBrief {
	return summarize.GroupBrief{
		PRNumber:   num,
		Title:      "t",
		Commits:    []history.Commit{{Hash: "abc"}},
		SymbolName: sym,
	}
}

func noPRBrief(sym string, hashes ...string) summarize.GroupBrief {
	cs := make([]history.Commit, 0, len(hashes))
	for _, h := range hashes {
		cs = append(cs, history.Commit{Hash: h})
	}
	return summarize.GroupBrief{Commits: cs, SymbolName: sym}
}

func TestWrapSummarizer_NilCases(t *testing.T) {
	inner := &fakeSummarizer{}
	if got := WrapSummarizer(inner, nil, "o", "r"); got != summarize.Summarizer(inner) {
		t.Errorf("nil cache should return inner")
	}
	if got := WrapSummarizer(nil, &Cache{}, "o", "r"); got != nil {
		t.Errorf("nil inner should return nil")
	}
}

func TestSummarizeCache_HitOnSecondCall(t *testing.T) {
	c := newCache(t)
	inner := &fakeSummarizer{summary: "tightened expiry"}
	s := WrapSummarizer(inner, c, "alice", "repo")

	first, err := s.Summarize(context.Background(), prBrief(142, "validateToken"))
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	if inner.calls != 1 {
		t.Fatalf("first call didn't reach inner: %d", inner.calls)
	}

	second, err := s.Summarize(context.Background(), prBrief(142, "validateToken"))
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if inner.calls != 1 {
		t.Errorf("second call should be cached: calls=%d", inner.calls)
	}
	if first != second {
		t.Errorf("cached result differs: first=%q second=%q", first, second)
	}
}

func TestSummarizeCache_EmptyResultCached(t *testing.T) {
	c := newCache(t)
	inner := &fakeSummarizer{summary: ""}
	s := WrapSummarizer(inner, c, "o", "r")
	if _, err := s.Summarize(context.Background(), prBrief(1, "f")); err != nil {
		t.Fatalf("first: %v", err)
	}
	if _, err := s.Summarize(context.Background(), prBrief(1, "f")); err != nil {
		t.Fatalf("second: %v", err)
	}
	if inner.calls != 1 {
		t.Errorf("empty summary should be cached: calls=%d", inner.calls)
	}
}

func TestSummarizeCache_ErrorsNotCached(t *testing.T) {
	c := newCache(t)
	bang := errors.New("boom")
	inner := &fakeSummarizer{err: bang}
	s := WrapSummarizer(inner, c, "o", "r")

	if _, err := s.Summarize(context.Background(), prBrief(1, "f")); !errors.Is(err, bang) {
		t.Fatalf("first: %v", err)
	}
	// Heal.
	inner.err = nil
	inner.summary = "ok"
	got, err := s.Summarize(context.Background(), prBrief(1, "f"))
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if inner.calls != 2 {
		t.Errorf("error should not be cached: calls=%d", inner.calls)
	}
	if got != "ok" {
		t.Errorf("post-heal summary = %q, want %q", got, "ok")
	}
}

func TestSummarizeCache_SymbolKeySegregation(t *testing.T) {
	c := newCache(t)
	inner := &fakeSummarizer{summary: "s"}
	s := WrapSummarizer(inner, c, "o", "r")

	if _, err := s.Summarize(context.Background(), prBrief(1, "alpha")); err != nil {
		t.Fatalf("alpha: %v", err)
	}
	if _, err := s.Summarize(context.Background(), prBrief(1, "beta")); err != nil {
		t.Fatalf("beta: %v", err)
	}
	if inner.calls != 2 {
		t.Errorf("different symbols on same PR should be separate cache entries: calls=%d", inner.calls)
	}
}

func TestSummarizeCache_RepoKeySegregation(t *testing.T) {
	c := newCache(t)
	inner := &fakeSummarizer{summary: "s"}
	a := WrapSummarizer(inner, c, "alice", "repo")
	b := WrapSummarizer(inner, c, "bob", "repo")

	if _, err := a.Summarize(context.Background(), prBrief(1, "f")); err != nil {
		t.Fatalf("a: %v", err)
	}
	if _, err := b.Summarize(context.Background(), prBrief(1, "f")); err != nil {
		t.Fatalf("b: %v", err)
	}
	if inner.calls != 2 {
		t.Errorf("repo segregation broken: calls=%d", inner.calls)
	}
}

func TestSummarizeCache_NoPRBucketKeyedByCommits(t *testing.T) {
	c := newCache(t)
	inner := &fakeSummarizer{summary: "s"}
	s := WrapSummarizer(inner, c, "o", "r")

	b1 := noPRBrief("f", "aaa", "bbb")
	b2 := noPRBrief("f", "bbb", "aaa") // same commits, different order
	b3 := noPRBrief("f", "aaa", "ccc") // different commit set

	if _, err := s.Summarize(context.Background(), b1); err != nil {
		t.Fatalf("b1: %v", err)
	}
	if _, err := s.Summarize(context.Background(), b2); err != nil {
		t.Fatalf("b2: %v", err)
	}
	if inner.calls != 1 {
		t.Errorf("commit order shouldn't matter for no-PR key: calls=%d", inner.calls)
	}
	if _, err := s.Summarize(context.Background(), b3); err != nil {
		t.Fatalf("b3: %v", err)
	}
	if inner.calls != 2 {
		t.Errorf("different commit set should miss cache: calls=%d", inner.calls)
	}
}
