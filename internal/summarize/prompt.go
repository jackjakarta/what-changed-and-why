package summarize

import (
	"fmt"
	"strings"
)

// maxBodyChars caps the PR body excerpt sent to the LLM. Anthropic's models
// tolerate larger contexts, but PR bodies above a few KB are usually issue
// templates or pasted logs — diminishing returns on quality, increasing cost.
const maxBodyChars = 2000

// maxCommitSubjects caps how many commit subjects are listed in the prompt
// to keep token usage predictable on PRs with long commit chains.
const maxCommitSubjects = 12

// maxSummaryChars is the post-processed length cap on the model's reply.
// Anything longer is truncated with an ellipsis.
const maxSummaryChars = 120

// buildPrompt returns the (system, user) message pair sent to the chat
// completion API. It is a pure function so tests can assert on its output
// without spinning up an HTTP server.
func buildPrompt(b GroupBrief) (system, user string) {
	if b.PRNumber == 0 {
		system = noPRSystemPrompt(b)
	} else {
		system = prSystemPrompt(b)
	}

	var u strings.Builder
	if b.PRNumber != 0 {
		fmt.Fprintf(&u, "PR #%d %q", b.PRNumber, b.Title)
		if b.Author != "" {
			fmt.Fprintf(&u, " by @%s", b.Author)
		}
		u.WriteString("\n")
		if body := truncate(strings.TrimSpace(b.Body), maxBodyChars); body != "" {
			u.WriteString("\nBody:\n")
			u.WriteString(body)
			u.WriteString("\n")
		}
	} else {
		u.WriteString("Commits not attached to any pull request.\n")
	}

	if len(b.Commits) > 0 {
		u.WriteString("\nCommits:\n")
		shown := b.Commits
		if len(shown) > maxCommitSubjects {
			shown = shown[:maxCommitSubjects]
		}
		for _, c := range shown {
			fmt.Fprintf(&u, "  - %s\n", strings.TrimSpace(c.Subject))
		}
		if len(b.Commits) > maxCommitSubjects {
			fmt.Fprintf(&u, "  (...and %d more)\n", len(b.Commits)-maxCommitSubjects)
		}
	}

	if len(b.TestFiles) > 0 {
		fmt.Fprintf(&u, "\nTests touched: %s\n", strings.Join(b.TestFiles, ", "))
	}

	if len(b.Issues) > 0 {
		raws := make([]string, 0, len(b.Issues))
		for _, ir := range b.Issues {
			raws = append(raws, ir.Raw)
		}
		fmt.Fprintf(&u, "\nLinked issues: %s\n", strings.Join(raws, ", "))
	}

	return system, u.String()
}

func prSystemPrompt(b GroupBrief) string {
	kind := b.SymbolKind
	if kind == "" {
		kind = "function"
	}
	return strings.Join([]string{
		"You summarise a single GitHub pull request for a developer tool that",
		fmt.Sprintf("explains the history of a TypeScript %s named `%s`.", kind, b.SymbolName),
		fmt.Sprintf("Reply with ONE line, max %d characters, plain English, describing", maxSummaryChars),
		"WHAT this PR did to that symbol and (briefly) WHY. Prefer concrete verbs",
		"over restating the title. No quotes around the reply. No trailing period.",
		"If the PR only incidentally touched the symbol, say so briefly.",
	}, " ")
}

func noPRSystemPrompt(b GroupBrief) string {
	kind := b.SymbolKind
	if kind == "" {
		kind = "function"
	}
	return strings.Join([]string{
		"You summarise a cluster of commits not attached to any pull request,",
		fmt.Sprintf("describing how they changed the TypeScript %s named `%s`.", kind, b.SymbolName),
		fmt.Sprintf("Reply with ONE line, max %d characters, plain English. No quotes,", maxSummaryChars),
		"no trailing period. Prefer concrete verbs.",
	}, " ")
}

// postProcess massages the model's reply into the shape the renderer expects:
// first non-empty line, surrounding straight or curly quotes stripped, length
// hard-capped, trailing period removed. Returns the empty string when the
// reply is unusable.
func postProcess(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	if idx := strings.IndexByte(s, '\n'); idx >= 0 {
		s = s[:idx]
	}
	s = strings.TrimSpace(s)
	s = stripWrappingQuotes(s)
	s = strings.TrimSpace(s)
	s = strings.TrimRight(s, ".")
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	// Hard cap by rune count so multibyte characters don't get cut in half.
	runes := []rune(s)
	if len(runes) > maxSummaryChars {
		// Reserve one rune for the ellipsis.
		runes = append(runes[:maxSummaryChars-1], '…')
		s = string(runes)
	}
	return s
}

// stripWrappingQuotes peels off matching outer quote characters. Handles
// straight ASCII quotes and the curly typographic variants the model
// sometimes emits.
func stripWrappingQuotes(s string) string {
	if len([]rune(s)) < 2 {
		return s
	}
	pairs := map[rune]rune{
		'"':  '"',
		'\'': '\'',
		'`':  '`',
		'“':  '”',
		'‘':  '’',
	}
	runes := []rune(s)
	close, ok := pairs[runes[0]]
	if !ok {
		return s
	}
	if runes[len(runes)-1] != close {
		return s
	}
	return string(runes[1 : len(runes)-1])
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	if max <= 1 {
		return s[:max]
	}
	return s[:max-1] + "…"
}
