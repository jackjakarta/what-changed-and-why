// Package cache is the Phase 7 persistence layer for wcaw. It backs the
// expensive, deterministic computations — per-commit tree-sitter parses and
// per-commit forge (GitHub) lookups — with a bbolt key/value store on disk so
// repeat invocations against the same repo amortise their cost.
//
// The package is structured so callers can ignore it entirely: a nil
// *Cache (or a nil history.SymbolEnumerator built from one) parses directly,
// and the forge decorator falls through to the inner forge unchanged. The CLI
// opens a cache when --no-cache is not set; everything downstream is
// transparent.
//
// Errors at the cache layer are deliberately swallowed: a read miss, a decode
// failure, or a write that loses the race against bbolt is treated as if the
// cache simply weren't there. Caching must never alter program output.
package cache

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	bolt "go.etcd.io/bbolt"

	"github.com/jackjakarta/what-changed-and-why/internal/locator"
	"github.com/jackjakarta/what-changed-and-why/internal/summarize"
)

// schemaVersion is the cache file's own format version. Bump only when the
// bucket layout or key encoding changes incompatibly; field-level changes to
// cached values are versioned via the per-bucket suffixes (see astBucket and
// forgeBucket below).
const schemaVersion = "1"

var (
	metaBucket = []byte("meta")
	// astBucket embeds the locator schema version: a locator change that
	// alters Symbol shapes invalidates AST entries without needing a full
	// schemaVersion bump.
	astBucket       = []byte(fmt.Sprintf("ast/v1/loc%d", locator.SchemaVersion))
	forgeBucket     = []byte("forge/v1")
	summarizeBucket = []byte(fmt.Sprintf("summarize/v1/p%d", summarize.PromptVersion))
)

// Cache wraps a bbolt database. The zero value is not usable; callers must
// obtain a *Cache via Open and call Close when finished. Cache is safe to
// share across goroutines (bbolt has its own locking) but is intended for use
// from a single CLI invocation.
type Cache struct {
	db *bolt.DB
}

// Open returns a Cache backed by the bbolt file at path, creating the file's
// parent directory if needed. If the file exists with a different
// schemaVersion, the AST and forge data buckets are reset (the file itself is
// preserved so concurrent open attempts don't trip over a deleted file).
//
// Open acquires bbolt's file lock with a one-second timeout — long enough to
// tolerate a sibling wcaw process briefly contending, short enough that a
// stuck lock surfaces quickly. On timeout (or any other error), Open returns
// the underlying error; callers should treat that as "disable caching".
func Open(path string) (*Cache, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create cache dir: %w", err)
	}
	db, err := bolt.Open(path, 0o644, &bolt.Options{Timeout: time.Second})
	if err != nil {
		return nil, fmt.Errorf("open cache %s: %w", path, err)
	}

	if err := initBuckets(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &Cache{db: db}, nil
}

// Close releases the bbolt handle. It is safe to call Close on a nil receiver
// so callers can `defer c.Close()` immediately after Open without nil-checks.
func (c *Cache) Close() error {
	if c == nil || c.db == nil {
		return nil
	}
	return c.db.Close()
}

// initBuckets creates the meta/ast/forge buckets if missing and resets the
// data buckets when the on-disk schemaVersion doesn't match this build's. A
// fresh file has no meta entry yet, so we treat "missing" as "match" — the
// version is written in the same transaction.
func initBuckets(db *bolt.DB) error {
	return db.Update(func(tx *bolt.Tx) error {
		meta, err := tx.CreateBucketIfNotExists(metaBucket)
		if err != nil {
			return fmt.Errorf("create meta bucket: %w", err)
		}

		existing := meta.Get([]byte("version"))
		if existing != nil && string(existing) != schemaVersion {
			if err := tx.DeleteBucket(astBucket); err != nil && !errors.Is(err, bolt.ErrBucketNotFound) {
				return fmt.Errorf("reset ast bucket: %w", err)
			}
			if err := tx.DeleteBucket(forgeBucket); err != nil && !errors.Is(err, bolt.ErrBucketNotFound) {
				return fmt.Errorf("reset forge bucket: %w", err)
			}
			if err := tx.DeleteBucket(summarizeBucket); err != nil && !errors.Is(err, bolt.ErrBucketNotFound) {
				return fmt.Errorf("reset summarize bucket: %w", err)
			}
		}
		if err := meta.Put([]byte("version"), []byte(schemaVersion)); err != nil {
			return fmt.Errorf("write version: %w", err)
		}

		if _, err := tx.CreateBucketIfNotExists(astBucket); err != nil {
			return fmt.Errorf("create ast bucket: %w", err)
		}
		if _, err := tx.CreateBucketIfNotExists(forgeBucket); err != nil {
			return fmt.Errorf("create forge bucket: %w", err)
		}
		if _, err := tx.CreateBucketIfNotExists(summarizeBucket); err != nil {
			return fmt.Errorf("create summarize bucket: %w", err)
		}
		return nil
	})
}

// DefaultPath returns the path wcaw uses when no explicit location is given:
// $XDG_CACHE_HOME/wcaw/cache.db when XDG_CACHE_HOME is set, otherwise
// ~/.cache/wcaw/cache.db. macOS users get the same XDG-style path; matching
// SPEC §5 Phase 7 is more valuable than honoring darwin's ~/Library/Caches
// convention here.
func DefaultPath() (string, error) {
	if base := os.Getenv("XDG_CACHE_HOME"); base != "" {
		return filepath.Join(base, "wcaw", "cache.db"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("locate user home: %w", err)
	}
	return filepath.Join(home, ".cache", "wcaw", "cache.db"), nil
}
