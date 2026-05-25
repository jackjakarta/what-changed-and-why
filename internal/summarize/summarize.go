// Package summarize is the LLM layer for wcaw. It produces a single
// short, plain-English line per forge.Group ("why this PR existed") so the
// timeline reads as a sequence of explained moves rather than a wall of
// titles.
//
// The package is structured around a Summarizer interface so the
// internal/cache decorator can wrap it transparently and tests can stub the
// network entirely. The concrete implementation in openai.go talks to an
// OpenAI-compatible endpoint — wcaw uses the Anthropic-on-OpenAI-compat
// gateway in production, but any endpoint speaking the chat-completion API
// will work.
//
// Errors and failure modes are deliberately non-fatal: a nil Summarizer, an
// empty API key, or repeated upstream errors all degrade to empty summaries.
// Caching never alters program output; the cache decorator lives in
// internal/cache so this package has zero persistence concern.
package summarize

import (
	"context"
	"fmt"
	"os"

	"github.com/jackjakarta/what-changed-and-why/internal/forge"
	"github.com/jackjakarta/what-changed-and-why/internal/history"
	"github.com/jackjakarta/what-changed-and-why/internal/locator"
)

// PromptVersion is bumped whenever buildPrompt's output changes in a way that
// would make cached responses semantically wrong. The cache bucket name
// embeds this value (see internal/cache), so a bump invalidates stale entries
// without touching the wider schema.
const PromptVersion = 1

// Summarizer produces one-line summaries from a GroupBrief. The interface is
// kept minimal so the cache decorator can implement it transparently.
type Summarizer interface {
	Summarize(ctx context.Context, b GroupBrief) (string, error)
}

// GroupBrief is the structured input to a single Summarize call. The
// concrete summarizer turns this into a prompt; tests build it directly.
type GroupBrief struct {
	// PRNumber is 0 for the no-PR bucket; non-zero otherwise.
	PRNumber int
	Title    string
	Body     string
	Author   string
	Issues   []forge.IssueRef
	Commits  []history.Commit
	// TestFiles is the deduped list of test paths touched by this group's
	// commits (from history.CollectTestFiles).
	TestFiles []string
	// SymbolName / SymbolKind identify the tracked symbol so the LLM can
	// ground its summary on it (e.g. "what did this PR do to validateToken").
	SymbolName string
	SymbolKind string
}

// BuildBrief packages a forge.Group and the tracked locator.Symbol into a
// GroupBrief suitable for Summarize. The returned brief shares the underlying
// commit/issue slices with g; callers must not mutate them.
func BuildBrief(g forge.Group, sym locator.Symbol) GroupBrief {
	b := GroupBrief{
		Commits:    g.Commits,
		TestFiles:  g.TestFiles,
		SymbolName: sym.Name,
		SymbolKind: sym.Kind.String(),
	}
	if g.Pull != nil {
		b.PRNumber = g.Pull.Number
		b.Title = g.Pull.Title
		b.Body = g.Pull.Body
		b.Author = g.Pull.Author
		b.Issues = g.Pull.Issues
	}
	return b
}

// DecorateGroups fills Group.Summary in place by calling s.Summarize for each
// group. If s is nil the function is a no-op.
//
// Degradation: a single Summarize error leaves that group's Summary empty and
// emits one stderr line; after three consecutive errors the remaining groups
// are skipped (no further LLM calls) with one final stderr line. This is the
// same shape as the forge fallback, tuned tighter because each call
// costs real money.
func DecorateGroups(ctx context.Context, s Summarizer, groups []forge.Group, sym locator.Symbol) {
	if s == nil {
		return
	}
	consecutive := 0
	aborted := false
	for i := range groups {
		if aborted {
			break
		}
		brief := BuildBrief(groups[i], sym)
		summary, err := s.Summarize(ctx, brief)
		if err != nil {
			consecutive++
			fmt.Fprintf(os.Stderr, "wcaw: summary failed for %s: %v\n", briefLabel(brief), err)
			if consecutive >= 3 {
				fmt.Fprintln(os.Stderr, "wcaw: 3 consecutive summary failures; skipping remaining groups")
				aborted = true
			}
			continue
		}
		consecutive = 0
		groups[i].Summary = summary
	}
}

// briefLabel renders a short tag for stderr messages: "PR #142" or
// "(no-PR bucket)". Kept separate so the orchestrator stays readable.
func briefLabel(b GroupBrief) string {
	if b.PRNumber == 0 {
		return "(no-PR bucket)"
	}
	return fmt.Sprintf("PR #%d", b.PRNumber)
}
