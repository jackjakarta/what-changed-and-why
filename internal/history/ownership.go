package history

import "time"

// Owner names the developer with the largest share of "touching" commits on
// the tracked symbol — i.e. commits where the symbol was actually changed
// (Class != ClassUnrelated). Commits / Total give the percentage; LastTouched
// records that author's most recent touching commit.
type Owner struct {
	Name        string
	Commits     int
	Total       int
	LastTouched time.Time
}

// EffectiveOwner returns the dominant author across the touching commits in
// `commits`. The bool is false when no commit qualifies (empty slice, or every
// commit is ClassUnrelated) — callers should then suppress the footer entirely
// rather than print "unknown".
//
// Tie-break (deterministic): highest commit count → most recent LastTouched →
// lexicographically smallest name.
func EffectiveOwner(commits []Commit) (Owner, bool) {
	type agg struct {
		name  string
		count int
		last  time.Time
	}
	stats := make(map[string]*agg)
	total := 0
	for _, c := range commits {
		if c.Class == ClassUnrelated || c.Class == ClassUnknown {
			continue
		}
		total++
		a, ok := stats[c.Author]
		if !ok {
			a = &agg{name: c.Author}
			stats[c.Author] = a
		}
		a.count++
		if c.Date.After(a.last) {
			a.last = c.Date
		}
	}
	if total == 0 {
		return Owner{}, false
	}

	var best *agg
	for _, a := range stats {
		if best == nil {
			best = a
			continue
		}
		if a.count != best.count {
			if a.count > best.count {
				best = a
			}
			continue
		}
		if !a.last.Equal(best.last) {
			if a.last.After(best.last) {
				best = a
			}
			continue
		}
		if a.name < best.name {
			best = a
		}
	}
	return Owner{
		Name:        best.name,
		Commits:     best.count,
		Total:       total,
		LastTouched: best.last,
	}, true
}

// Percent returns the rounded-to-nearest-integer percentage of touching
// commits attributable to this owner. Returns 0 on a zero-Total Owner.
func (o Owner) Percent() int {
	if o.Total == 0 {
		return 0
	}
	return (o.Commits*100 + o.Total/2) / o.Total
}
