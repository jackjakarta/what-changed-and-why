package cache

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"

	bolt "go.etcd.io/bbolt"

	"github.com/jackjakarta/what-changed-and-why/internal/summarize"
)

// WrapSummarizer decorates an existing summarize.Summarizer with a read-through
// cache keyed by (owner, repo, group identity, symbol). Successful results
// (including the rare "" case) are cached; errors are passed through untouched
// so the orchestrator's degradation counter still observes real failures. A
// nil cache or nil inner returns the inner summarizer as-is.
func WrapSummarizer(inner summarize.Summarizer, c *Cache, owner, repo string) summarize.Summarizer {
	if inner == nil || c == nil || c.db == nil {
		return inner
	}
	return &summarizeCache{inner: inner, c: c, owner: owner, repo: repo}
}

type summarizeCache struct {
	inner summarize.Summarizer
	c     *Cache
	owner string
	repo  string
}

// summarizeEntry is the on-disk value layout. We wrap the bare string in a
// struct so future schema-compatible additions (timestamps, model name) are
// possible without bumping summarize.PromptVersion.
type summarizeEntry struct {
	Summary string `json:"summary"`
}

func (sc *summarizeCache) Summarize(ctx context.Context, b summarize.GroupBrief) (string, error) {
	key := summarizeKey(sc.owner, sc.repo, b)

	var cached summarizeEntry
	hit := false
	_ = sc.c.db.View(func(tx *bolt.Tx) error {
		bk := tx.Bucket(summarizeBucket)
		if bk == nil {
			return nil
		}
		raw := bk.Get(key)
		if raw == nil {
			return nil
		}
		if err := json.Unmarshal(raw, &cached); err != nil {
			return nil
		}
		hit = true
		return nil
	})
	if hit {
		return cached.Summary, nil
	}

	summary, err := sc.inner.Summarize(ctx, b)
	if err != nil {
		return "", err
	}

	if raw, mErr := json.Marshal(summarizeEntry{Summary: summary}); mErr == nil {
		_ = sc.c.db.Update(func(tx *bolt.Tx) error {
			bk := tx.Bucket(summarizeBucket)
			if bk == nil {
				return nil
			}
			return bk.Put(key, raw)
		})
	}
	return summary, nil
}

// summarizeKey produces a deterministic cache key per (owner, repo, group,
// symbol). PR-attached groups are keyed by PR number; no-PR groups by a
// sha256 of their sorted commit hashes so reorderings (or partial overlaps
// from a renamed history walk) don't accidentally hit the wrong row.
func summarizeKey(owner, repo string, b summarize.GroupBrief) []byte {
	if b.PRNumber != 0 {
		return []byte(fmt.Sprintf("%s/%s/pr/%d/%s", owner, repo, b.PRNumber, b.SymbolName))
	}
	hashes := make([]string, 0, len(b.Commits))
	for _, c := range b.Commits {
		hashes = append(hashes, c.Hash)
	}
	sort.Strings(hashes)
	h := sha256.New()
	for _, s := range hashes {
		h.Write([]byte(s))
		h.Write([]byte{0})
	}
	return []byte(fmt.Sprintf("%s/%s/nopr/%s/%s", owner, repo, hex.EncodeToString(h.Sum(nil)), b.SymbolName))
}
