package render

import (
	"fmt"
	"io"
	"strings"

	"github.com/fatih/color"

	"github.com/jackjakarta/what-changed-and-why/internal/forge"
	"github.com/jackjakarta/what-changed-and-why/internal/history"
)

// Color palette. Pre-bound SprintFuncs honor color.NoColor at call time so we
// can flip the global once at startup (via ResetColors) and forget about it.
var (
	cPR     = color.New(color.FgCyan).SprintFunc()
	cAuthor = color.New(color.FgYellow).SprintFunc()
	cIssue  = color.New(color.FgGreen).SprintFunc()
	cDate   = color.New(color.Faint).SprintFunc()
	cSymbol = color.New(color.Bold).SprintFunc()
	cMuted  = color.New(color.Faint).SprintFunc()
)

const bulletHang = "            " // 12-column hang under "  <Mon YYYY>  "

func renderHuman(w io.Writer, in Input) error {
	if len(in.Commits) == 0 {
		return nil
	}

	touching, prCount := countsForHeader(in)
	oldest := in.Commits[len(in.Commits)-1].Date
	ago := Humanize(in.Now, oldest)

	fmt.Fprintf(w, "%s — introduced %s, %s",
		cSymbol(in.Symbol.Name),
		cMuted(ago),
		pluralize(touching, "commit"),
	)
	if prCount > 0 {
		fmt.Fprintf(w, " across %s\n\n", pluralize(prCount, "PR"))
	} else {
		fmt.Fprint(w, " (no PRs)\n\n")
	}

	groups := reverseGroups(in.Groups)
	introducingNum := introducingGroupNumber(groups)

	for _, g := range groups {
		writeGroupBlock(w, g, introducingNum)
	}

	if in.HasOwner {
		fmt.Fprintf(w, "\nEffective owner: %s (%s of changes, last touched %s)\n",
			cAuthor("@"+in.Owner.Name),
			cMuted(fmt.Sprintf("%d%%", in.Owner.Percent())),
			cMuted(Humanize(in.Now, in.Owner.LastTouched)),
		)
	}
	return nil
}

func countsForHeader(in Input) (touching, prCount int) {
	for _, c := range in.Commits {
		if c.Class == history.ClassUnrelated || c.Class == history.ClassUnknown {
			continue
		}
		touching++
	}
	for _, g := range in.Groups {
		if g.Pull != nil {
			prCount++
		}
	}
	return
}

// introducingGroupNumber returns the Pull number of the group containing the
// oldest commit whose Class is ClassIntroduced. Zero means "no PR matched the
// introducing commit" (and we use it to tag the no-PR bucket too).
//
// Phase 6 only uses this to decide where to emit the `─ N lines` bullet.
func introducingGroupNumber(groupsChrono []forge.Group) int {
	for _, g := range groupsChrono {
		for _, c := range g.Commits {
			if c.Class == history.ClassIntroduced && c.Symbol != nil {
				if g.Pull != nil {
					return g.Pull.Number
				}
				return -1
			}
		}
	}
	return 0
}

func writeGroupBlock(w io.Writer, g forge.Group, introducingNum int) {
	date := groupDate(g)

	groupNum := -1
	if g.Pull != nil {
		groupNum = g.Pull.Number
	}

	var headline string
	if g.Pull != nil {
		headline = fmt.Sprintf("%s %q", cPR(fmt.Sprintf("PR #%d", g.Pull.Number)), g.Pull.Title)
		if g.Pull.Author != "" {
			headline += "  " + cAuthor("@"+g.Pull.Author)
		}
	} else {
		headline = cMuted("(no PR)")
	}

	fmt.Fprintf(w, "  %s  %s\n", cDate(date), headline)

	for _, b := range groupBullets(g, introducingNum == groupNum) {
		fmt.Fprintf(w, "%s%s %s\n", bulletHang, cMuted("─"), b)
	}
}

// groupDate returns the "Mon YYYY" prefix for a group: PR's MergedAt when
// available, else the oldest commit's date.
func groupDate(g forge.Group) string {
	if g.Pull != nil && !g.Pull.MergedAt.IsZero() {
		return g.Pull.MergedAt.Format("Jan 2006")
	}
	if len(g.Commits) > 0 {
		return g.Commits[len(g.Commits)-1].Date.Format("Jan 2006")
	}
	return ""
}

// groupBullets returns the detail lines for a single group in display order.
// isIntroducingGroup tells us whether to emit the `N lines` bullet.
func groupBullets(g forge.Group, isIntroducingGroup bool) []string {
	var out []string

	if g.Summary != "" {
		out = append(out, cMuted(g.Summary))
	}

	if isIntroducingGroup {
		for _, c := range g.Commits {
			if c.Class == history.ClassIntroduced && c.Symbol != nil {
				lines := int(c.Symbol.EndLine) - int(c.Symbol.StartLine) + 1
				if lines > 0 {
					out = append(out, cMuted(fmt.Sprintf("%d lines", lines)))
				}
				break
			}
		}
	}

	if len(g.Commits) > 1 {
		out = append(out, cMuted(fmt.Sprintf("%d commits", len(g.Commits))))
	}

	if sources := collectSourceFiles(g); len(sources) > 0 {
		out = append(out, cMuted("also touched ")+strings.Join(sources, ", "))
	}

	if prev := firstRenamePrev(g); prev != "" {
		out = append(out, cMuted("renamed from ")+prev)
	}

	if len(g.TestFiles) > 0 {
		out = append(out, cMuted("alongside ")+strings.Join(g.TestFiles, ", "))
	}

	if g.Pull != nil && len(g.Pull.Issues) > 0 {
		raws := make([]string, 0, len(g.Pull.Issues))
		for _, ir := range g.Pull.Issues {
			raws = append(raws, cIssue(ir.Raw))
		}
		label := "linked issue: "
		if len(raws) > 1 {
			label = "linked issues: "
		}
		out = append(out, cMuted(label)+strings.Join(raws, ", "))
	}

	return out
}

func collectSourceFiles(g forge.Group) []string {
	seen := make(map[string]struct{})
	var out []string
	for _, c := range g.Commits {
		if c.Symbol == nil || c.Symbol.SourceFile == "" {
			continue
		}
		if _, ok := seen[c.Symbol.SourceFile]; ok {
			continue
		}
		seen[c.Symbol.SourceFile] = struct{}{}
		out = append(out, c.Symbol.SourceFile)
	}
	return out
}

func firstRenamePrev(g forge.Group) string {
	for _, c := range g.Commits {
		if c.Class == history.ClassRenamed && c.Symbol != nil && c.Symbol.PrevName != "" {
			return c.Symbol.PrevName
		}
	}
	return ""
}

func pluralize(n int, word string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, word)
	}
	return fmt.Sprintf("%d %ss", n, word)
}
