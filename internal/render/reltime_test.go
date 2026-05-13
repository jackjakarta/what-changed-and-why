package render

import (
	"testing"
	"time"
)

func TestHumanize(t *testing.T) {
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name string
		gap  time.Duration
		want string
	}{
		{"future", -time.Hour, "in the future"},
		{"just now", 30 * time.Minute, "just now"},
		{"today", 6 * time.Hour, "today"},
		{"yesterday", 30 * time.Hour, "yesterday"},
		{"3 days", 3 * 24 * time.Hour, "3 days ago"},
		{"6 days", 6 * 24 * time.Hour, "6 days ago"},
		{"last week", 8 * 24 * time.Hour, "last week"},
		{"2 weeks", 15 * 24 * time.Hour, "2 weeks ago"},
		{"8 weeks", 56 * 24 * time.Hour, "8 weeks ago"},
		{"3 months", 90 * 24 * time.Hour, "3 months ago"},
		{"14 months", 420 * 24 * time.Hour, "14 months ago"},
		{"17 months", 510 * 24 * time.Hour, "17 months ago"},
		{"1 year (18mo bucket)", 18 * 30 * 24 * time.Hour, "1 year ago"},
		{"2 years", 800 * 24 * time.Hour, "2 years ago"},
		{"5 years", 5 * 365 * 24 * time.Hour, "5 years ago"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Humanize(now, now.Add(-c.gap))
			if got != c.want {
				t.Fatalf("Humanize gap=%s: got %q, want %q", c.gap, got, c.want)
			}
		})
	}
}
