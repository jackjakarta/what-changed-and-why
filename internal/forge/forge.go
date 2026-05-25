// Package forge enriches the per-commit history walk from internal/history
// with GitHub pull-request metadata: PR titles, authors, linked issues, and a
// grouped view that collapses commit lists onto the PRs that introduced them.
//
// The Forge interface exists so the Group orchestrator can be unit-tested
// against a fake while the real GitHub implementation lives in github.go.
package forge

import (
	"context"
	"fmt"
	"time"

	"github.com/jackjakarta/what-changed-and-why/internal/history"
)

// Forge resolves a commit SHA to the pull requests that introduced it. v1 has
// a single GitHub implementation; the interface exists primarily so Group can
// be tested without making network calls.
type Forge interface {
	PullsForCommit(ctx context.Context, sha string) ([]PullRef, error)
}

// PullRef is the result of a commit-to-PR lookup. Body is populated when the
// underlying response carries it (the primary github commit-to-pulls endpoint
// returns full PullRequest objects); on the search-API fallback it may be
// empty. Issue extraction tolerates either case.
type PullRef struct {
	Number   int
	Title    string
	Author   string    // login, no leading "@"
	URL      string    // html_url
	MergedAt time.Time // zero if unmerged
	MergeSHA string
	State    string // "open" | "closed"
	Body     string
}

// Pull is a PullRef enriched with the issue refs extracted from its title and
// body. Group emits these in its returned slice.
type Pull struct {
	PullRef
	Issues []IssueRef
}

// IssueRef is a linked-issue reference. For "#142": Project=="", Number==142,
// Raw=="#142". For "SEC-44": Project=="SEC", Number==44, Raw=="SEC-44".
type IssueRef struct {
	Project string
	Number  int
	Raw     string
}

// Group bundles one PR (or the no-PR bucket, when Pull == nil) with the
// commits that mapped to it. Commits stay in the input slice's order
// (newest-first per history.Track). TestFiles and Summary are populated by
// post-grouping decoration (see cmd/wcaw); GroupCommits itself leaves both
// zero.
type Group struct {
	Pull      *Pull
	Commits   []history.Commit
	TestFiles []string
	Summary   string
}

// GroupCommits resolves each commit to a PR via f, picks a winning PR per
// commit, dedups PRs by number, extracts issue refs, and returns Groups in
// the order of their newest commit (preserves overall newest-first).
//
// Degradation rules: if 5 lookups fail consecutively, or more than half of
// the lookups attempted so far have failed (after at least 5 attempts),
// GroupCommits aborts with an error so the caller can fall back to an
// unenriched render. Individual "no PRs for this commit" results are expected
// and not treated as errors.
func GroupCommits(ctx context.Context, f Forge, commits []history.Commit) ([]Group, error) {
	if len(commits) == 0 {
		return nil, nil
	}

	perCommitPR := make([]int, len(commits)) // 0 means "no PR"
	prs := make(map[int]*PullRef)            // dedup by PR number

	consecutiveFails := 0
	totalFails := 0
	var lastErr error

	for i, c := range commits {
		refs, err := f.PullsForCommit(ctx, c.Hash)
		if err != nil {
			consecutiveFails++
			totalFails++
			lastErr = err
			if consecutiveFails >= 5 {
				return nil, fmt.Errorf("%d consecutive errors; last: %w", consecutiveFails, lastErr)
			}
			if totalFails*2 > i+1 && i >= 4 {
				return nil, fmt.Errorf("%d/%d lookups failed; last: %w", totalFails, i+1, lastErr)
			}
			continue
		}
		consecutiveFails = 0

		if len(refs) == 0 {
			continue
		}
		chosen := chooseRef(refs)
		perCommitPR[i] = chosen.Number
		if _, ok := prs[chosen.Number]; !ok {
			r := chosen
			prs[chosen.Number] = &r
		}
	}

	type pending struct {
		num     int
		commits []history.Commit
	}
	byNum := make(map[int]*pending)
	var order []int

	for i, c := range commits {
		num := perCommitPR[i]
		g, ok := byNum[num]
		if !ok {
			g = &pending{num: num}
			byNum[num] = g
			order = append(order, num)
		}
		g.commits = append(g.commits, c)
	}

	out := make([]Group, 0, len(order))
	for _, num := range order {
		pg := byNum[num]
		var pull *Pull
		if num != 0 {
			ref := prs[num]
			pull = &Pull{
				PullRef: *ref,
				Issues:  extractIssueRefs(ref.Title, ref.Body),
			}
		}
		out = append(out, Group{Pull: pull, Commits: pg.commits})
	}
	return out, nil
}

// chooseRef picks the single PR to attribute a commit to when the forge
// returns multiple. Merged PRs win over unmerged; ties broken by smallest
// (i.e. oldest) PR number, which favours the original PR over later
// cherry-pick or revert PRs that include the same SHA.
func chooseRef(refs []PullRef) PullRef {
	var merged []PullRef
	for _, r := range refs {
		if !r.MergedAt.IsZero() {
			merged = append(merged, r)
		}
	}
	pool := merged
	if len(pool) == 0 {
		pool = refs
	}
	best := pool[0]
	for _, r := range pool[1:] {
		if r.Number < best.Number {
			best = r
		}
	}
	return best
}
