package cache

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"testing"

	bolt "go.etcd.io/bbolt"

	"github.com/jackjakarta/what-changed-and-why/internal/locator"
)

const tsSource = `function bar() {
    return 1;
}

function baz() {
    return 2;
}
`

func newCache(t *testing.T) *Cache {
	t.Helper()
	c, err := Open(filepath.Join(t.TempDir(), "cache.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

func TestASTEnumerator_RoundTrip(t *testing.T) {
	c := newCache(t)
	e := &ASTEnumerator{C: c, RepoRoot: "/repo"}

	first, err := e.Enumerate("sha1", "foo.ts", []byte(tsSource))
	if err != nil {
		t.Fatalf("first enumerate: %v", err)
	}
	if len(first) != 2 || first[0].Name != "bar" || first[1].Name != "baz" {
		t.Fatalf("unexpected first parse: %+v", first)
	}

	// Same key, no blob — proves the second call reads from cache.
	second, err := e.Enumerate("sha1", "foo.ts", nil)
	if err != nil {
		t.Fatalf("second enumerate: %v", err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Errorf("cached result differs:\n got=%+v\nwant=%+v", second, first)
	}
}

func TestASTEnumerator_NilCacheFallsThrough(t *testing.T) {
	e := &ASTEnumerator{C: nil, RepoRoot: "/repo"}
	got, err := e.Enumerate("sha1", "foo.ts", []byte(tsSource))
	if err != nil {
		t.Fatalf("enumerate: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("nil-cache fallthrough produced %d syms, want 2", len(got))
	}
}

func TestASTEnumerator_EmptyCommitSHAFallsThrough(t *testing.T) {
	c := newCache(t)
	e := &ASTEnumerator{C: c, RepoRoot: "/repo"}
	got, err := e.Enumerate("", "foo.ts", []byte(tsSource))
	if err != nil {
		t.Fatalf("enumerate: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("empty-sha produced %d syms, want 2", len(got))
	}
}

func TestASTEnumerator_RepoRootScoping(t *testing.T) {
	c := newCache(t)
	a := &ASTEnumerator{C: c, RepoRoot: "/repo-a"}
	b := &ASTEnumerator{C: c, RepoRoot: "/repo-b"}

	if _, err := a.Enumerate("sha1", "foo.ts", []byte(tsSource)); err != nil {
		t.Fatalf("seed a: %v", err)
	}

	// Repo B must miss even though (sha, path) match — verified by writing a
	// distinct entry under the same logical key and observing it persists.
	if _, err := b.Enumerate("sha1", "foo.ts", []byte("function only() {}\n")); err != nil {
		t.Fatalf("seed b: %v", err)
	}
	got, err := b.Enumerate("sha1", "foo.ts", nil)
	if err != nil {
		t.Fatalf("read b: %v", err)
	}
	if len(got) != 1 || got[0].Name != "only" {
		t.Errorf("repo B leaked repo A's entry: %+v", got)
	}
}

func TestASTEnumerator_CorruptValueFallsThrough(t *testing.T) {
	c := newCache(t)
	e := &ASTEnumerator{C: c, RepoRoot: "/repo"}

	// Inject garbage under the exact key the enumerator will look for.
	key := astKey("/repo", "sha1", "foo.ts")
	if err := c.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(astBucket).Put(key, []byte("not json"))
	}); err != nil {
		t.Fatalf("seed corrupt: %v", err)
	}

	got, err := e.Enumerate("sha1", "foo.ts", []byte(tsSource))
	if err != nil {
		t.Fatalf("enumerate: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("after corrupt entry, parse produced %d syms, want 2", len(got))
	}

	// The overwrite path should have replaced the garbage with a real entry.
	var raw []byte
	_ = c.db.View(func(tx *bolt.Tx) error {
		raw = tx.Bucket(astBucket).Get(key)
		return nil
	})
	var decoded []locator.Symbol
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Errorf("after fallthrough the cache still holds garbage: %v", err)
	}
}
