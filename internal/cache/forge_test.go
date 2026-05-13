package cache

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/jackjakarta/what-changed-and-why/internal/forge"
)

// fakeForge counts PullsForCommit invocations and replays the configured
// response (or error) for each sha in order. Useful for asserting that the
// cache decorator removes work on a second pass.
type fakeForge struct {
	calls    int
	response map[string][]forge.PullRef // per-sha canned response (used if set)
	err      map[string]error           // per-sha canned error
	default_ []forge.PullRef            // fallback when no per-sha entry
}

func (f *fakeForge) PullsForCommit(ctx context.Context, sha string) ([]forge.PullRef, error) {
	f.calls++
	if e, ok := f.err[sha]; ok {
		return nil, e
	}
	if r, ok := f.response[sha]; ok {
		return r, nil
	}
	return f.default_, nil
}

func TestWrap_NilCacheOrNilInnerReturnsInner(t *testing.T) {
	inner := &fakeForge{}
	if got := Wrap(inner, nil, "o", "r"); got != forge.Forge(inner) {
		t.Errorf("Wrap with nil cache should return inner, got %T", got)
	}
	if got := Wrap(nil, &Cache{}, "o", "r"); got != nil {
		t.Errorf("Wrap with nil inner should return nil, got %T", got)
	}
}

func TestForgeCache_HitOnSecondCall(t *testing.T) {
	c := newCache(t)
	inner := &fakeForge{
		response: map[string][]forge.PullRef{
			"abc": {{Number: 142, Title: "harden token validation"}},
		},
	}
	f := Wrap(inner, c, "alice", "repo")

	first, err := f.PullsForCommit(context.Background(), "abc")
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	if inner.calls != 1 {
		t.Fatalf("first call did not reach inner: calls=%d", inner.calls)
	}

	second, err := f.PullsForCommit(context.Background(), "abc")
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if inner.calls != 1 {
		t.Errorf("second call should have been cached: calls=%d", inner.calls)
	}
	if !reflect.DeepEqual(first, second) {
		t.Errorf("cached result differs:\n got=%+v\nwant=%+v", second, first)
	}
}

func TestForgeCache_EmptyResultIsCached(t *testing.T) {
	c := newCache(t)
	inner := &fakeForge{default_: nil}
	f := Wrap(inner, c, "o", "r")

	if _, err := f.PullsForCommit(context.Background(), "abc"); err != nil {
		t.Fatalf("first: %v", err)
	}
	if _, err := f.PullsForCommit(context.Background(), "abc"); err != nil {
		t.Fatalf("second: %v", err)
	}
	if inner.calls != 1 {
		t.Errorf("empty result should have been cached: calls=%d", inner.calls)
	}
}

func TestForgeCache_ErrorsNotCached(t *testing.T) {
	c := newCache(t)
	bang := errors.New("boom")
	inner := &fakeForge{
		err:      map[string]error{"abc": bang},
		response: map[string][]forge.PullRef{},
	}
	f := Wrap(inner, c, "o", "r")

	if _, err := f.PullsForCommit(context.Background(), "abc"); !errors.Is(err, bang) {
		t.Fatalf("first call: want boom, got %v", err)
	}
	// Heal: clear the per-sha error so the next inner call succeeds.
	delete(inner.err, "abc")
	inner.response["abc"] = []forge.PullRef{{Number: 1}}

	got, err := f.PullsForCommit(context.Background(), "abc")
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if inner.calls != 2 {
		t.Errorf("error response should not have been cached: calls=%d", inner.calls)
	}
	if len(got) != 1 || got[0].Number != 1 {
		t.Errorf("post-heal result: %+v", got)
	}
}

func TestForgeCache_KeySegregationByRepo(t *testing.T) {
	c := newCache(t)
	inner := &fakeForge{
		response: map[string][]forge.PullRef{
			"abc": {{Number: 1}},
		},
	}
	a := Wrap(inner, c, "alice", "repo")
	b := Wrap(inner, c, "bob", "repo")

	if _, err := a.PullsForCommit(context.Background(), "abc"); err != nil {
		t.Fatalf("a: %v", err)
	}
	if _, err := b.PullsForCommit(context.Background(), "abc"); err != nil {
		t.Fatalf("b: %v", err)
	}
	if inner.calls != 2 {
		t.Errorf("repo segregation broken: calls=%d (each repo should miss separately)", inner.calls)
	}
}
