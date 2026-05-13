package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	bolt "go.etcd.io/bbolt"

	"github.com/jackjakarta/what-changed-and-why/internal/locator"
)

// ASTEnumerator satisfies history.SymbolEnumerator by reading parsed Symbol
// slices out of the cache (keyed by repo root + commit SHA + file path) and
// writing fresh parses back. Cache faults — open, decode, write — fall back
// to a direct locator.Enumerate call so the wider walk continues unimpeded.
//
// RepoRoot is hashed into the key so a shared cache file across multiple
// repos can't collide on a coincident (commit, file) pair.
type ASTEnumerator struct {
	C        *Cache
	RepoRoot string
}

// Enumerate returns the symbols for the given commit/file. The blob argument
// is the source bytes to parse on a cache miss; cache hits never read it.
func (e *ASTEnumerator) Enumerate(commitSHA, filePath string, blob []byte) ([]locator.Symbol, error) {
	if e == nil || e.C == nil || e.C.db == nil || commitSHA == "" {
		return locator.Enumerate(blob)
	}

	key := astKey(e.RepoRoot, commitSHA, filePath)

	var cached []locator.Symbol
	hit := false
	_ = e.C.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(astBucket)
		if b == nil {
			return nil
		}
		raw := b.Get(key)
		if raw == nil {
			return nil
		}
		var decoded []locator.Symbol
		if err := json.Unmarshal(raw, &decoded); err != nil {
			// Corrupt entry: treat as a miss and let the writer overwrite.
			return nil
		}
		cached = decoded
		hit = true
		return nil
	})
	if hit {
		return cached, nil
	}

	syms, err := locator.Enumerate(blob)
	if err != nil {
		return nil, err
	}

	if raw, mErr := json.Marshal(syms); mErr == nil {
		_ = e.C.db.Update(func(tx *bolt.Tx) error {
			b := tx.Bucket(astBucket)
			if b == nil {
				return nil
			}
			return b.Put(key, raw)
		})
	}
	return syms, nil
}

// astKey builds the (repo-root, commit-sha, file-path) lookup key. The repo
// root is hashed (SHA-256, hex) to keep the bbolt key bounded in length and
// independent of OS path quirks; the commit SHA is already a stable digest;
// the file path is stored verbatim so a glance at a dumped DB is still
// readable.
func astKey(repoRoot, commitSHA, filePath string) []byte {
	sum := sha256.Sum256([]byte(repoRoot))
	out := make([]byte, 0, 64+1+len(commitSHA)+1+len(filePath))
	out = append(out, []byte(hex.EncodeToString(sum[:]))...)
	out = append(out, '|')
	out = append(out, []byte(commitSHA)...)
	out = append(out, '|')
	out = append(out, []byte(filePath)...)
	return out
}
