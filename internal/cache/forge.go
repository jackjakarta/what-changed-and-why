package cache

import (
	"context"
	"encoding/json"

	bolt "go.etcd.io/bbolt"

	"github.com/jackjakarta/what-changed-and-why/internal/forge"
)

// Wrap decorates an existing forge.Forge with a read-through cache keyed by
// (owner, repo, commit-sha). Successful results (including the empty
// "no PRs for this commit" slice) are cached; errors are passed through
// untouched so GroupCommits' degradation counters still observe real
// failures. A nil cache or nil inner returns the inner forge as-is.
func Wrap(inner forge.Forge, c *Cache, owner, repo string) forge.Forge {
	if inner == nil || c == nil || c.db == nil {
		return inner
	}
	return &forgeCache{inner: inner, c: c, owner: owner, repo: repo}
}

type forgeCache struct {
	inner forge.Forge
	c     *Cache
	owner string
	repo  string
}

func (fc *forgeCache) PullsForCommit(ctx context.Context, sha string) ([]forge.PullRef, error) {
	key := forgeKey(fc.owner, fc.repo, sha)

	var cached []forge.PullRef
	hit := false
	_ = fc.c.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(forgeBucket)
		if b == nil {
			return nil
		}
		raw := b.Get(key)
		if raw == nil {
			return nil
		}
		var decoded []forge.PullRef
		if err := json.Unmarshal(raw, &decoded); err != nil {
			return nil
		}
		// Normalise nil -> empty so the "cached empty" case is distinct from
		// a nil-decoded miss; callers downstream tolerate either.
		if decoded == nil {
			decoded = []forge.PullRef{}
		}
		cached = decoded
		hit = true
		return nil
	})
	if hit {
		return cached, nil
	}

	refs, err := fc.inner.PullsForCommit(ctx, sha)
	if err != nil {
		// Never cache failures: GroupCommits relies on real errors to drive
		// its consecutive-fail and >50% abort logic.
		return nil, err
	}

	toWrite := refs
	if toWrite == nil {
		toWrite = []forge.PullRef{}
	}
	if raw, mErr := json.Marshal(toWrite); mErr == nil {
		_ = fc.c.db.Update(func(tx *bolt.Tx) error {
			b := tx.Bucket(forgeBucket)
			if b == nil {
				return nil
			}
			return b.Put(key, raw)
		})
	}
	return refs, nil
}

// forgeKey is "<owner>/<repo>/<sha>". Owner and repo come from the
// *GitHubForge accessors at wrap time, so the same forge instance always
// produces the same key prefix for its commits.
func forgeKey(owner, repo, sha string) []byte {
	out := make([]byte, 0, len(owner)+1+len(repo)+1+len(sha))
	out = append(out, []byte(owner)...)
	out = append(out, '/')
	out = append(out, []byte(repo)...)
	out = append(out, '/')
	out = append(out, []byte(sha)...)
	return out
}
