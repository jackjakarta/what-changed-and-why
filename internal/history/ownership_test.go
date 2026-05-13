package history

import (
	"testing"
	"time"
)

func mkCommit(author string, day int, class Classification) Commit {
	return Commit{
		Author: author,
		Date:   time.Date(2026, 1, day, 12, 0, 0, 0, time.UTC),
		Class:  class,
	}
}

func TestEffectiveOwner_SingleAuthor(t *testing.T) {
	commits := []Commit{
		mkCommit("alice", 3, ClassModified),
		mkCommit("alice", 2, ClassModified),
		mkCommit("alice", 1, ClassIntroduced),
	}
	o, ok := EffectiveOwner(commits)
	if !ok {
		t.Fatalf("ok = false, want true")
	}
	if o.Name != "alice" || o.Commits != 3 || o.Total != 3 {
		t.Errorf("got %+v, want alice 3/3", o)
	}
	if o.Percent() != 100 {
		t.Errorf("percent = %d, want 100", o.Percent())
	}
}

func TestEffectiveOwner_MixedAuthorsClearMajority(t *testing.T) {
	commits := []Commit{
		mkCommit("alice", 5, ClassModified),
		mkCommit("alice", 4, ClassModified),
		mkCommit("bob", 3, ClassIntroduced),
	}
	o, ok := EffectiveOwner(commits)
	if !ok {
		t.Fatalf("ok = false")
	}
	if o.Name != "alice" || o.Commits != 2 || o.Total != 3 {
		t.Errorf("got %+v, want alice 2/3", o)
	}
	if o.Percent() != 67 {
		t.Errorf("percent = %d, want 67", o.Percent())
	}
}

func TestEffectiveOwner_ExcludesUnrelatedFromCountAndTotal(t *testing.T) {
	commits := []Commit{
		mkCommit("alice", 5, ClassUnrelated),
		mkCommit("alice", 4, ClassUnrelated),
		mkCommit("bob", 3, ClassModified),
		mkCommit("bob", 2, ClassIntroduced),
	}
	o, ok := EffectiveOwner(commits)
	if !ok {
		t.Fatalf("ok = false")
	}
	if o.Name != "bob" || o.Commits != 2 || o.Total != 2 {
		t.Errorf("got %+v, want bob 2/2", o)
	}
	if o.Percent() != 100 {
		t.Errorf("percent = %d, want 100", o.Percent())
	}
}

func TestEffectiveOwner_TieBreakByMostRecent(t *testing.T) {
	commits := []Commit{
		mkCommit("alice", 5, ClassModified),
		mkCommit("bob", 10, ClassModified),
	}
	o, ok := EffectiveOwner(commits)
	if !ok {
		t.Fatalf("ok = false")
	}
	if o.Name != "bob" {
		t.Errorf("name = %s, want bob (most-recent tie-break)", o.Name)
	}
}

func TestEffectiveOwner_TieBreakByName(t *testing.T) {
	// Same date for both → fall through to lexicographic.
	commits := []Commit{
		mkCommit("bob", 5, ClassModified),
		mkCommit("alice", 5, ClassModified),
	}
	o, ok := EffectiveOwner(commits)
	if !ok {
		t.Fatalf("ok = false")
	}
	if o.Name != "alice" {
		t.Errorf("name = %s, want alice (lex tie-break)", o.Name)
	}
}

func TestEffectiveOwner_AllUnrelated(t *testing.T) {
	commits := []Commit{
		mkCommit("alice", 1, ClassUnrelated),
		mkCommit("bob", 2, ClassUnrelated),
	}
	_, ok := EffectiveOwner(commits)
	if ok {
		t.Fatalf("ok = true, want false (all unrelated)")
	}
}

func TestEffectiveOwner_Empty(t *testing.T) {
	_, ok := EffectiveOwner(nil)
	if ok {
		t.Fatalf("ok = true, want false (empty)")
	}
}

func TestEffectiveOwner_LastTouchedIsAuthorsLatest(t *testing.T) {
	// alice has commits on day 3 and day 7. Her LastTouched must be day 7,
	// not bob's day 10.
	commits := []Commit{
		mkCommit("alice", 7, ClassModified),
		mkCommit("bob", 10, ClassUnrelated), // ignored
		mkCommit("alice", 3, ClassModified),
	}
	o, ok := EffectiveOwner(commits)
	if !ok {
		t.Fatalf("ok = false")
	}
	if o.Name != "alice" {
		t.Fatalf("name = %s, want alice", o.Name)
	}
	want := time.Date(2026, 1, 7, 12, 0, 0, 0, time.UTC)
	if !o.LastTouched.Equal(want) {
		t.Errorf("LastTouched = %v, want %v", o.LastTouched, want)
	}
}

func TestPercent_Rounding(t *testing.T) {
	cases := []struct {
		commits, total, want int
	}{
		{1, 3, 33}, // 33.33 → 33
		{2, 3, 67}, // 66.67 → 67
		{1, 2, 50}, // 50.0  → 50
		{1, 4, 25}, // 25.0  → 25
		{3, 4, 75}, // 75.0  → 75
		{1, 8, 13}, // 12.5  → 13
		{0, 0, 0},  // zero-total
	}
	for _, tc := range cases {
		o := Owner{Commits: tc.commits, Total: tc.total}
		if got := o.Percent(); got != tc.want {
			t.Errorf("Percent(%d/%d) = %d, want %d", tc.commits, tc.total, got, tc.want)
		}
	}
}
