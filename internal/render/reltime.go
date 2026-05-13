package render

import (
	"fmt"
	"time"
)

// Humanize renders the gap between `now` and `t` as a coarse English phrase
// suitable for a single inline cell ("3 weeks ago", "14 months ago"). The
// buckets intentionally avoid calendar precision: month = 30d, year = 365d.
//
// `t` after `now` returns "in the future" defensively; callers don't normally
// produce that case but it's not worth panicking.
func Humanize(now, t time.Time) string {
	delta := now.Sub(t)
	if delta < 0 {
		return "in the future"
	}
	const (
		hour  = time.Hour
		day   = 24 * hour
		week  = 7 * day
		month = 30 * day
		year  = 365 * day
	)
	switch {
	case delta < hour:
		return "just now"
	case delta < day:
		return "today"
	case delta < 2*day:
		return "yesterday"
	case delta < week:
		return fmt.Sprintf("%d days ago", int(delta/day))
	case delta < 2*week:
		return "last week"
	case delta < 60*day:
		return fmt.Sprintf("%d weeks ago", int(delta/week))
	case delta < 18*month:
		return fmt.Sprintf("%d months ago", int(delta/month))
	default:
		n := int(delta / year)
		if n < 1 {
			n = 1
		}
		if n == 1 {
			return "1 year ago"
		}
		return fmt.Sprintf("%d years ago", n)
	}
}
