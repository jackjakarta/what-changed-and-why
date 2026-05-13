package render

import (
	"encoding/json"
	"io"
	"time"

	"github.com/jackjakarta/what-changed-and-why/internal/forge"
	"github.com/jackjakarta/what-changed-and-why/internal/history"
)

// SchemaVersion bumps on any breaking change to the JSON wire format.
// See docs/SCHEMA.md for the field-by-field reference.
const SchemaVersion = 1

// jsonDoc and the nested wire types intentionally live in this package rather
// than as tags on the internal history/forge structs. The wire format is a
// public contract; the internal structs change with the codebase.
type jsonDoc struct {
	SchemaVersion int         `json:"schema_version"`
	Symbol        jsonSymbol  `json:"symbol"`
	Summary       jsonSummary `json:"summary"`
	Groups        []jsonGroup `json:"groups"`
	Owner         *jsonOwner  `json:"owner"`
}

type jsonSymbol struct {
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	Path      string `json:"path"`
	StartLine uint32 `json:"start_line"`
	EndLine   uint32 `json:"end_line"`
	StartByte uint32 `json:"start_byte"`
	EndByte   uint32 `json:"end_byte"`
}

type jsonSummary struct {
	IntroducedAt    *time.Time `json:"introduced_at"`
	TouchingCommits int        `json:"touching_commits"`
	TotalCommits    int        `json:"total_commits"`
	PRCount         int        `json:"pr_count"`
}

type jsonGroup struct {
	Pull      *jsonPull    `json:"pull"`
	Commits   []jsonCommit `json:"commits"`
	TestFiles []string     `json:"test_files"`
}

type jsonPull struct {
	Number   int            `json:"number"`
	Title    string         `json:"title"`
	Author   string         `json:"author"`
	URL      string         `json:"url"`
	MergedAt *time.Time     `json:"merged_at"`
	State    string         `json:"state"`
	MergeSHA string         `json:"merge_sha"`
	Issues   []jsonIssueRef `json:"issues"`
}

type jsonIssueRef struct {
	Raw     string `json:"raw"`
	Project string `json:"project"`
	Number  int    `json:"number"`
}

type jsonCommit struct {
	Hash    string            `json:"hash"`
	Date    time.Time         `json:"date"`
	Author  string            `json:"author"`
	Subject string            `json:"subject"`
	Class   string            `json:"class"`
	Symbol  *jsonCommitSymbol `json:"symbol"`
}

type jsonCommitSymbol struct {
	Name       string `json:"name"`
	PrevName   string `json:"prev_name"`
	SourceFile string `json:"source_file"`
	StartLine  uint32 `json:"start_line"`
	EndLine    uint32 `json:"end_line"`
}

type jsonOwner struct {
	Name        string    `json:"name"`
	Commits     int       `json:"commits"`
	Total       int       `json:"total"`
	Percent     int       `json:"percent"`
	LastTouched time.Time `json:"last_touched"`
}

func renderJSON(w io.Writer, in Input) error {
	doc := buildDoc(in)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(doc)
}

func buildDoc(in Input) jsonDoc {
	touching, prCount := countsForHeader(in)

	var introducedAt *time.Time
	if len(in.Commits) > 0 {
		d := in.Commits[len(in.Commits)-1].Date
		introducedAt = &d
	}

	chrono := reverseGroups(in.Groups)
	groups := make([]jsonGroup, 0, len(chrono))
	for _, g := range chrono {
		groups = append(groups, buildJSONGroup(g))
	}

	doc := jsonDoc{
		SchemaVersion: SchemaVersion,
		Symbol: jsonSymbol{
			Name:      in.Symbol.Name,
			Kind:      in.Symbol.Kind.String(),
			Path:      in.Path,
			StartLine: in.Symbol.StartLine,
			EndLine:   in.Symbol.EndLine,
			StartByte: in.Symbol.StartByte,
			EndByte:   in.Symbol.EndByte,
		},
		Summary: jsonSummary{
			IntroducedAt:    introducedAt,
			TouchingCommits: touching,
			TotalCommits:    len(in.Commits),
			PRCount:         prCount,
		},
		Groups: groups,
	}

	if in.HasOwner {
		doc.Owner = &jsonOwner{
			Name:        in.Owner.Name,
			Commits:     in.Owner.Commits,
			Total:       in.Owner.Total,
			Percent:     in.Owner.Percent(),
			LastTouched: in.Owner.LastTouched,
		}
	}

	return doc
}

func buildJSONGroup(g forge.Group) jsonGroup {
	commits := make([]jsonCommit, 0, len(g.Commits))
	// Reverse to chronological inside each group too.
	for i := len(g.Commits) - 1; i >= 0; i-- {
		commits = append(commits, buildJSONCommit(g.Commits[i]))
	}
	testFiles := g.TestFiles
	if testFiles == nil {
		testFiles = []string{}
	}
	jg := jsonGroup{Commits: commits, TestFiles: testFiles}
	if g.Pull != nil {
		jg.Pull = buildJSONPull(*g.Pull)
	}
	return jg
}

func buildJSONPull(p forge.Pull) *jsonPull {
	var merged *time.Time
	if !p.MergedAt.IsZero() {
		m := p.MergedAt
		merged = &m
	}
	issues := make([]jsonIssueRef, 0, len(p.Issues))
	for _, ir := range p.Issues {
		issues = append(issues, jsonIssueRef{
			Raw:     ir.Raw,
			Project: ir.Project,
			Number:  ir.Number,
		})
	}
	return &jsonPull{
		Number:   p.Number,
		Title:    p.Title,
		Author:   p.Author,
		URL:      p.URL,
		MergedAt: merged,
		State:    p.State,
		MergeSHA: p.MergeSHA,
		Issues:   issues,
	}
}

func buildJSONCommit(c history.Commit) jsonCommit {
	out := jsonCommit{
		Hash:    c.Hash,
		Date:    c.Date,
		Author:  c.Author,
		Subject: c.Subject,
		Class:   c.Class.String(),
	}
	if c.Symbol != nil {
		out.Symbol = &jsonCommitSymbol{
			Name:       c.Symbol.Name,
			PrevName:   c.Symbol.PrevName,
			SourceFile: c.Symbol.SourceFile,
			StartLine:  c.Symbol.StartLine,
			EndLine:    c.Symbol.EndLine,
		}
	}
	return out
}
