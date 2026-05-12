package forge

import "regexp"

// hashRefRegex captures `#NNN` issue/PR references. The leading character class
// excludes `&` (HTML entity `&#123;`), `#` (markdown `##` headers), and word
// characters (so `abc#123` doesn't match). RE2 has no lookbehind, so we use a
// non-capturing alternation between "start of string" and "non-word non-amp-or-hash".
var hashRefRegex = regexp.MustCompile(`(?:^|[^A-Za-z0-9_&#])#(\d+)\b`)

// jiraRefRegex captures Jira-style refs like `SEC-44`, `FOO-1`. Project key is
// 2–10 chars starting with a capital letter; the rest are uppercase
// alphanumerics. The number is one or more digits.
var jiraRefRegex = regexp.MustCompile(`\b([A-Z][A-Z0-9]{1,9})-(\d+)\b`)

// extractIssueRefs scans the supplied strings for both hash- and Jira-style
// issue refs, returning them in first-occurrence order with duplicates removed.
// Empty inputs are tolerated.
func extractIssueRefs(texts ...string) []IssueRef {
	var out []IssueRef
	seen := make(map[string]struct{})

	addHashMatches := func(s string) {
		for _, m := range hashRefRegex.FindAllStringSubmatchIndex(s, -1) {
			numStr := s[m[2]:m[3]]
			raw := "#" + numStr
			if _, dup := seen[raw]; dup {
				continue
			}
			n := atoi(numStr)
			if n <= 0 {
				continue
			}
			seen[raw] = struct{}{}
			out = append(out, IssueRef{Number: n, Raw: raw})
		}
	}
	addJiraMatches := func(s string) {
		for _, m := range jiraRefRegex.FindAllStringSubmatch(s, -1) {
			raw := m[0]
			if _, dup := seen[raw]; dup {
				continue
			}
			n := atoi(m[2])
			if n <= 0 {
				continue
			}
			seen[raw] = struct{}{}
			out = append(out, IssueRef{Project: m[1], Number: n, Raw: raw})
		}
	}

	for _, t := range texts {
		if t == "" {
			continue
		}
		addHashMatches(t)
		addJiraMatches(t)
	}
	return out
}

func atoi(s string) int {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return -1
		}
		n = n*10 + int(r-'0')
	}
	return n
}
