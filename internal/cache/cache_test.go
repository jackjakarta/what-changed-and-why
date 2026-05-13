package cache

import (
	"os"
	"path/filepath"
	"testing"

	bolt "go.etcd.io/bbolt"
)

func tempCachePath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "nested", "cache.db")
}

func TestOpenClose_AutoCreatesParent(t *testing.T) {
	path := tempCachePath(t)
	c, err := Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("cache file not created: %v", err)
	}
	if err := c.Close(); err != nil {
		t.Errorf("close: %v", err)
	}
}

func TestClose_NilReceiver(t *testing.T) {
	var c *Cache
	if err := c.Close(); err != nil {
		t.Errorf("nil Close returned: %v", err)
	}
}

func TestOpen_BucketsExist(t *testing.T) {
	c, err := Open(tempCachePath(t))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer c.Close()

	if err := c.db.View(func(tx *bolt.Tx) error {
		for _, b := range [][]byte{metaBucket, astBucket, forgeBucket, summarizeBucket} {
			if tx.Bucket(b) == nil {
				t.Errorf("missing bucket %s", string(b))
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("view: %v", err)
	}
}

func TestOpen_SchemaMismatchResetsDataBuckets(t *testing.T) {
	path := tempCachePath(t)
	c, err := Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	// Seed an entry in the ast bucket so we can prove it gets wiped.
	if err := c.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(astBucket).Put([]byte("k"), []byte("v"))
	}); err != nil {
		t.Fatalf("seed ast: %v", err)
	}

	// Force a version mismatch on the same file.
	if err := c.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(metaBucket).Put([]byte("version"), []byte("0"))
	}); err != nil {
		t.Fatalf("rewrite version: %v", err)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	c, err = Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer c.Close()

	var raw []byte
	if err := c.db.View(func(tx *bolt.Tx) error {
		raw = tx.Bucket(astBucket).Get([]byte("k"))
		return nil
	}); err != nil {
		t.Fatalf("view: %v", err)
	}
	if raw != nil {
		t.Errorf("ast entry survived schema bump: %q", raw)
	}

	// Version should have been rewritten to current.
	var ver []byte
	_ = c.db.View(func(tx *bolt.Tx) error {
		ver = tx.Bucket(metaBucket).Get([]byte("version"))
		return nil
	})
	if string(ver) != schemaVersion {
		t.Errorf("version after reopen = %q, want %q", ver, schemaVersion)
	}
}

func TestDefaultPath_XDG(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", "/tmp/xdg-set")
	got, err := DefaultPath()
	if err != nil {
		t.Fatalf("default path: %v", err)
	}
	want := filepath.Join("/tmp/xdg-set", "wcaw", "cache.db")
	if got != want {
		t.Errorf("path = %q, want %q", got, want)
	}
}

func TestDefaultPath_FallbackToHome(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", "")
	// Force HOME to a known value so the assertion is portable.
	t.Setenv("HOME", "/tmp/fake-home")
	got, err := DefaultPath()
	if err != nil {
		t.Fatalf("default path: %v", err)
	}
	want := filepath.Join("/tmp/fake-home", ".cache", "wcaw", "cache.db")
	if got != want {
		t.Errorf("path = %q, want %q", got, want)
	}
}
