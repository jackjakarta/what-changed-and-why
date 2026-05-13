package render

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/jackjakarta/what-changed-and-why/internal/forge"
	"github.com/jackjakarta/what-changed-and-why/internal/history"
	"github.com/jackjakarta/what-changed-and-why/internal/locator"
)

func TestMain(m *testing.M) {
	// Force colors off for deterministic golden strings. ResetColors is
	// idempotent so tests that need a different state can call it again.
	ResetColors(false)
	m.Run()
}

// sampleInput builds a rich fixture exercising:
//   - introducing commit (1 line bullet),
//   - rename within a PR,
//   - cross-file move (also-touched),
//   - test files (alongside),
//   - issue refs (linked issues),
//   - a no-PR bucket.
//
// All commit slices stay newest-first so the renderer's reversal is exercised.
func sampleInput() Input {
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)

	dateAug2024 := time.Date(2024, 8, 12, 10, 0, 0, 0, time.UTC)
	dateOct2024 := time.Date(2024, 10, 4, 9, 0, 0, 0, time.UTC)
	dateMar2025 := time.Date(2025, 3, 8, 14, 0, 0, 0, time.UTC)
	dateMar2025b := time.Date(2025, 3, 9, 14, 0, 0, 0, time.UTC)
	dateNoPR := time.Date(2025, 9, 1, 8, 0, 0, 0, time.UTC)

	// Commits in newest-first order overall.
	cNoPR := history.Commit{
		Hash: "0000111", Date: dateNoPR, Author: "carla",
		Subject: "tweak comment", Class: history.ClassModified,
		Symbol: &history.SymbolRef{Name: "validateToken", StartLine: 14, EndLine: 32},
	}
	cMar1 := history.Commit{
		Hash: "1111222", Date: dateMar2025b, Author: "maria",
		Subject: "split into refresh path", Class: history.ClassModified,
		Symbol: &history.SymbolRef{Name: "validateToken", StartLine: 14, EndLine: 50},
	}
	cMar2 := history.Commit{
		Hash: "2222333", Date: dateMar2025, Author: "maria",
		Subject: "moved out of session.ts", Class: history.ClassMovedFrom,
		Symbol: &history.SymbolRef{
			Name: "validateToken", SourceFile: "src/auth/session.ts",
			StartLine: 14, EndLine: 32,
		},
	}
	cOct := history.Commit{
		Hash: "3333444", Date: dateOct2024, Author: "jonas",
		Subject: "fix clock skew", Class: history.ClassModified,
		Symbol: &history.SymbolRef{Name: "validateToken", StartLine: 14, EndLine: 32},
	}
	cAug := history.Commit{
		Hash: "4444555", Date: dateAug2024, Author: "maria",
		Subject: "feat: initial JWT validation", Class: history.ClassIntroduced,
		Symbol: &history.SymbolRef{Name: "validateToken", StartLine: 14, EndLine: 32},
	}

	groups := []forge.Group{
		// No-PR group is newest (chronologically last).
		{Pull: nil, Commits: []history.Commit{cNoPR}, TestFiles: nil},
		// PR #311: cross-file move + 2 commits.
		{
			Pull: &forge.Pull{
				PullRef: forge.PullRef{
					Number: 311, Title: "Refactor for refresh tokens",
					Author: "maria", URL: "https://example/pull/311",
					MergedAt: dateMar2025, State: "closed", MergeSHA: "abcdef0",
				},
			},
			Commits: []history.Commit{cMar1, cMar2},
		},
		{
			Pull: &forge.Pull{
				PullRef: forge.PullRef{
					Number: 189, Title: "Fix clock-skew tolerance",
					Author: "jonas", URL: "https://example/pull/189",
					MergedAt: dateOct2024, State: "closed",
				},
				Issues: []forge.IssueRef{{Raw: "SEC-44", Project: "SEC", Number: 44}},
			},
			Commits: []history.Commit{cOct},
		},
		{
			Pull: &forge.Pull{
				PullRef: forge.PullRef{
					Number: 142, Title: "Initial JWT validation",
					Author: "maria", URL: "https://example/pull/142",
					MergedAt: dateAug2024, State: "closed",
				},
			},
			Commits:   []history.Commit{cAug},
			TestFiles: []string{"src/auth/login.test.ts"},
		},
	}

	commits := []history.Commit{cNoPR, cMar1, cMar2, cOct, cAug}

	return Input{
		Symbol: locator.Symbol{
			Name: "validateToken", Kind: locator.KindFunction,
			StartLine: 14, EndLine: 32, StartByte: 220, EndByte: 612,
		},
		Path:    "src/auth/login.ts",
		Groups:  groups,
		Commits: commits,
		Owner: history.Owner{
			Name: "maria", Commits: 3, Total: 5,
			LastTouched: dateMar2025b,
		},
		HasOwner: true,
		Now:      now,
	}
}

func TestHumanRichFixture(t *testing.T) {
	var buf bytes.Buffer
	if err := Human(&buf, sampleInput()); err != nil {
		t.Fatalf("Human: %v", err)
	}
	got := buf.String()
	wantLines := []string{
		`validateToken — introduced 1 year ago, 5 commits across 3 PRs`,
		``,
		`  Aug 2024  PR #142 "Initial JWT validation"  @maria`,
		`            ─ 19 lines`,
		`            ─ alongside src/auth/login.test.ts`,
		`  Oct 2024  PR #189 "Fix clock-skew tolerance"  @jonas`,
		`            ─ linked issue: SEC-44`,
		`  Mar 2025  PR #311 "Refactor for refresh tokens"  @maria`,
		`            ─ 2 commits`,
		`            ─ also touched src/auth/session.ts`,
		`  Sep 2025  (no PR)`,
		``,
		`Effective owner: @maria (60% of changes, last touched 14 months ago)`,
		``,
	}
	want := strings.Join(wantLines, "\n")
	if got != want {
		t.Fatalf("Human output mismatch.\ngot:\n%s\n--\nwant:\n%s", got, want)
	}
}

func TestHumanEmptyCommits(t *testing.T) {
	in := sampleInput()
	in.Commits = nil
	in.Groups = nil
	in.HasOwner = false
	var buf bytes.Buffer
	if err := Human(&buf, in); err != nil {
		t.Fatalf("Human: %v", err)
	}
	if buf.Len() != 0 {
		t.Fatalf("expected empty output, got %q", buf.String())
	}
}

func TestHumanNoOwner(t *testing.T) {
	in := sampleInput()
	in.HasOwner = false
	var buf bytes.Buffer
	if err := Human(&buf, in); err != nil {
		t.Fatalf("Human: %v", err)
	}
	if strings.Contains(buf.String(), "Effective owner") {
		t.Fatalf("expected no owner line, got:\n%s", buf.String())
	}
}

func TestHumanSummaryBullet(t *testing.T) {
	in := sampleInput()
	// Add a summary to PR #142 (the introducing PR — last group in the
	// newest-first slice). The bullet should appear *above* the "N lines"
	// bullet that already fires for the introducing group.
	in.Groups[len(in.Groups)-1].Summary = "tightened JWT expiry tolerance"

	var buf bytes.Buffer
	if err := Human(&buf, in); err != nil {
		t.Fatalf("Human: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "tightened JWT expiry tolerance") {
		t.Fatalf("summary line missing:\n%s", out)
	}
	summaryIdx := strings.Index(out, "tightened JWT expiry tolerance")
	linesIdx := strings.Index(out, "19 lines")
	if summaryIdx < 0 || linesIdx < 0 {
		t.Fatalf("could not locate both bullets in:\n%s", out)
	}
	if summaryIdx > linesIdx {
		t.Errorf("summary bullet should appear before the lines bullet; summary@%d lines@%d", summaryIdx, linesIdx)
	}
}

func TestHumanAllNoPR(t *testing.T) {
	in := sampleInput()
	// Collapse everything into a single no-PR bucket; keep the introducing
	// commit so the "N lines" bullet still fires.
	flat := []forge.Group{{Pull: nil, Commits: in.Commits}}
	in.Groups = flat
	var buf bytes.Buffer
	if err := Human(&buf, in); err != nil {
		t.Fatalf("Human: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "(no PR)") {
		t.Fatalf("expected (no PR) line, got:\n%s", out)
	}
	if !strings.Contains(out, "(no PRs)") {
		t.Fatalf("expected '(no PRs)' header tail, got:\n%s", out)
	}
}
