package forge

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackjakarta/what-changed-and-why/internal/history"
)

func TestParseRemoteURL(t *testing.T) {
	cases := []struct {
		name, in, owner, repo string
		ok                    bool
	}{
		{"https", "https://github.com/o/r", "o", "r", true},
		{"https with .git", "https://github.com/o/r.git", "o", "r", true},
		{"https trailing slash", "https://github.com/o/r/", "o", "r", true},
		{"https mixed case host", "https://GitHub.com/o/r", "o", "r", true},
		{"scp-like", "git@github.com:o/r.git", "o", "r", true},
		{"scp-like no .git", "git@github.com:o/r", "o", "r", true},
		{"ssh", "ssh://git@github.com/o/r.git", "o", "r", true},
		{"http", "http://github.com/o/r", "o", "r", true},
		{"hyphens", "https://github.com/some-org/some-repo.git", "some-org", "some-repo", true},

		{"gitlab", "https://gitlab.com/o/r", "", "", false},
		{"bare host", "https://github.com/", "", "", false},
		{"only owner", "https://github.com/o", "", "", false},
		{"empty", "", "", "", false},
		{"garbage", "not a url", "", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			owner, repo, ok := parseRemoteURL(c.in)
			if ok != c.ok || owner != c.owner || repo != c.repo {
				t.Fatalf("parseRemoteURL(%q) = (%q, %q, %v), want (%q, %q, %v)",
					c.in, owner, repo, ok, c.owner, c.repo, c.ok)
			}
		})
	}
}

func TestExtractIssueRefs(t *testing.T) {
	cases := []struct {
		name  string
		texts []string
		want  []IssueRef
	}{
		{
			"hash only",
			[]string{"fixes #142 in the auth layer"},
			[]IssueRef{{Number: 142, Raw: "#142"}},
		},
		{
			"jira only",
			[]string{"see SEC-44 for context"},
			[]IssueRef{{Project: "SEC", Number: 44, Raw: "SEC-44"}},
		},
		{
			"mixed sources, dedup across",
			[]string{"closes #99", "also tracked as #99 and SEC-44"},
			[]IssueRef{
				{Number: 99, Raw: "#99"},
				{Project: "SEC", Number: 44, Raw: "SEC-44"},
			},
		},
		{
			"reject html entity",
			[]string{"raw text with &#123; entity"},
			nil,
		},
		{
			"reject markdown header",
			[]string{"## 123 header"},
			nil,
		},
		{
			"reject lowercase jira",
			[]string{"sec-44 should not match"},
			nil,
		},
		{
			"reject zero number",
			[]string{"#0 nope, SEC-0 nope either"},
			nil,
		},
		{
			"hash at start of string",
			[]string{"#7 is the bug"},
			[]IssueRef{{Number: 7, Raw: "#7"}},
		},
		{
			"hash inside word does not match",
			[]string{"abc#123 should not match"},
			nil,
		},
		{
			"multiple distinct hashes preserve order",
			[]string{"fix #5 and #3"},
			[]IssueRef{
				{Number: 5, Raw: "#5"},
				{Number: 3, Raw: "#3"},
			},
		},
		{
			"jira with digits in project",
			[]string{"AB12-99 should match"},
			[]IssueRef{{Project: "AB12", Number: 99, Raw: "AB12-99"}},
		},
		{
			"empty inputs tolerated",
			[]string{"", ""},
			nil,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := extractIssueRefs(c.texts...)
			if !reflect.DeepEqual(got, c.want) {
				t.Fatalf("extractIssueRefs(%q) = %+v, want %+v", c.texts, got, c.want)
			}
		})
	}
}

// fakeForge maps commit-hash → []PullRef. A nil entry signals "no PR";
// returning an entry from errFor signals an API failure for that commit.
type fakeForge struct {
	byCommit map[string][]PullRef
	errFor   map[string]error
	calls    map[string]int
}

func newFakeForge() *fakeForge {
	return &fakeForge{
		byCommit: make(map[string][]PullRef),
		errFor:   make(map[string]error),
		calls:    make(map[string]int),
	}
}

func (f *fakeForge) PullsForCommit(_ context.Context, sha string) ([]PullRef, error) {
	f.calls[sha]++
	if err, ok := f.errFor[sha]; ok {
		return nil, err
	}
	return f.byCommit[sha], nil
}

func commit(hash string, secs int) history.Commit {
	return history.Commit{
		Hash:    hash,
		Date:    time.Unix(int64(secs), 0),
		Author:  "tester",
		Subject: "msg " + hash,
	}
}

func mergedRef(num int, title, author, body string, secs int) PullRef {
	return PullRef{
		Number:   num,
		Title:    title,
		Author:   author,
		Body:     body,
		MergedAt: time.Unix(int64(secs), 0),
		State:    "closed",
	}
}

func TestGroupCommits_OrdersByNewestCommit(t *testing.T) {
	commits := []history.Commit{
		commit("aaa", 300), // newest
		commit("bbb", 200),
		commit("ccc", 100), // oldest
	}
	f := newFakeForge()
	f.byCommit["aaa"] = []PullRef{mergedRef(1, "first PR", "alice", "", 300)}
	f.byCommit["bbb"] = []PullRef{mergedRef(2, "second PR", "bob", "", 200)}
	f.byCommit["ccc"] = []PullRef{mergedRef(1, "first PR", "alice", "", 300)}

	groups, err := GroupCommits(context.Background(), f, commits)
	if err != nil {
		t.Fatalf("GroupCommits: %v", err)
	}
	if len(groups) != 2 {
		t.Fatalf("got %d groups, want 2", len(groups))
	}
	if groups[0].Pull == nil || groups[0].Pull.Number != 1 {
		t.Errorf("group 0 PR = %+v, want #1 (its newest commit aaa is newer than #2's bbb)", groups[0].Pull)
	}
	if groups[1].Pull == nil || groups[1].Pull.Number != 2 {
		t.Errorf("group 1 PR = %+v, want #2", groups[1].Pull)
	}
	if len(groups[0].Commits) != 2 {
		t.Errorf("group 0 has %d commits, want 2 (aaa+ccc)", len(groups[0].Commits))
	}
}

func TestGroupCommits_NoPRCommitsBucketedInPlace(t *testing.T) {
	commits := []history.Commit{
		commit("aaa", 300),
		commit("bbb", 200), // no PR
		commit("ccc", 100),
	}
	f := newFakeForge()
	f.byCommit["aaa"] = []PullRef{mergedRef(1, "first", "alice", "", 300)}
	f.byCommit["ccc"] = []PullRef{mergedRef(1, "first", "alice", "", 300)}
	// bbb intentionally unset → empty slice

	groups, err := GroupCommits(context.Background(), f, commits)
	if err != nil {
		t.Fatalf("GroupCommits: %v", err)
	}
	// Expected: PR#1 (aaa), no-PR (bbb), PR#1 again? No — dedup means aaa+ccc
	// belong to the same group. But because bbb is interleaved between them
	// in input order, the no-PR bucket appears between groups, and ccc joins
	// PR#1 as the LAST entry (since we walk newest-first and append).
	//
	// So order should be: PR#1 (newest seen first = aaa), no-PR (bbb), and
	// ccc gets appended back to PR#1's group. Total groups: 2.
	if len(groups) != 2 {
		t.Fatalf("got %d groups, want 2: %+v", len(groups), groups)
	}
	if groups[0].Pull == nil || groups[0].Pull.Number != 1 {
		t.Errorf("group 0 PR = %+v, want #1", groups[0].Pull)
	}
	if groups[1].Pull != nil {
		t.Errorf("group 1 should be no-PR, got %+v", groups[1].Pull)
	}
	if len(groups[0].Commits) != 2 {
		t.Errorf("PR#1 should hold aaa+ccc, got %d commits", len(groups[0].Commits))
	}
}

func TestGroupCommits_DedupsByPRNumber(t *testing.T) {
	commits := []history.Commit{
		commit("aaa", 300),
		commit("bbb", 200),
		commit("ccc", 100),
	}
	f := newFakeForge()
	f.byCommit["aaa"] = []PullRef{mergedRef(7, "shared", "alice", "fix SEC-1", 300)}
	f.byCommit["bbb"] = []PullRef{mergedRef(7, "shared", "alice", "fix SEC-1", 300)}
	f.byCommit["ccc"] = []PullRef{mergedRef(7, "shared", "alice", "fix SEC-1", 300)}

	groups, err := GroupCommits(context.Background(), f, commits)
	if err != nil {
		t.Fatalf("GroupCommits: %v", err)
	}
	if len(groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(groups))
	}
	if len(groups[0].Commits) != 3 {
		t.Errorf("expected 3 commits in the single group, got %d", len(groups[0].Commits))
	}
	if len(groups[0].Pull.Issues) != 1 || groups[0].Pull.Issues[0].Raw != "SEC-1" {
		t.Errorf("expected SEC-1 issue extracted once, got %+v", groups[0].Pull.Issues)
	}
}

func TestGroupCommits_AbortsOnConsecutiveErrors(t *testing.T) {
	var commits []history.Commit
	for i := 0; i < 8; i++ {
		commits = append(commits, commit(string(rune('a'+i))+"hash", 100-i))
	}
	f := newFakeForge()
	for _, c := range commits {
		f.errFor[c.Hash] = errors.New("rate limited")
	}

	_, err := GroupCommits(context.Background(), f, commits)
	if err == nil {
		t.Fatal("expected abort error, got nil")
	}
	if !strings.Contains(err.Error(), "consecutive") {
		t.Errorf("expected consecutive-errors message, got %q", err.Error())
	}
}

func TestGroupCommits_TolerateOccasionalError(t *testing.T) {
	commits := []history.Commit{
		commit("aaa", 300),
		commit("bbb", 200),
		commit("ccc", 100),
	}
	f := newFakeForge()
	f.byCommit["aaa"] = []PullRef{mergedRef(1, "p1", "alice", "", 300)}
	f.errFor["bbb"] = errors.New("transient")
	f.byCommit["ccc"] = []PullRef{mergedRef(1, "p1", "alice", "", 300)}

	groups, err := GroupCommits(context.Background(), f, commits)
	if err != nil {
		t.Fatalf("expected tolerance of one error, got %v", err)
	}
	// aaa → PR#1, bbb → no-PR (error swallowed), ccc → PR#1. PR#1 ends up
	// with aaa+ccc; bbb stays in the no-PR bucket between them.
	if len(groups) != 2 {
		t.Fatalf("expected 2 groups, got %d", len(groups))
	}
	if groups[1].Pull != nil {
		t.Errorf("expected error commit to land in no-PR group, got %+v", groups[1].Pull)
	}
}

func TestChooseRef_PrefersMergedThenSmallestNumber(t *testing.T) {
	open := PullRef{Number: 5}
	mergedNew := PullRef{Number: 10, MergedAt: time.Unix(200, 0)}
	mergedOld := PullRef{Number: 8, MergedAt: time.Unix(100, 0)}

	got := chooseRef([]PullRef{open, mergedNew, mergedOld})
	if got.Number != 8 {
		t.Errorf("expected smallest merged (#8), got #%d", got.Number)
	}

	// All unmerged → smallest number wins.
	got = chooseRef([]PullRef{{Number: 12}, {Number: 4}, {Number: 9}})
	if got.Number != 4 {
		t.Errorf("expected smallest open (#4), got #%d", got.Number)
	}
}
