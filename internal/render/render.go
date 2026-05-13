// Package render owns the human and JSON output formats for wcaw. The Phase
// 6 timeline mockup (see chat_with_claude.md) is the source of truth for the
// human shape; the JSON schema is documented in docs/SCHEMA.md.
//
// Internally the input is consumed newest-first (matching history.Track and
// forge.GroupCommits); both renderers reverse to chronological order so the
// timeline reads top-down. Owner math runs on the unreversed flat slice.
package render

import (
	"io"
	"os"
	"time"

	"github.com/fatih/color"

	"github.com/jackjakarta/what-changed-and-why/internal/forge"
	"github.com/jackjakarta/what-changed-and-why/internal/history"
	"github.com/jackjakarta/what-changed-and-why/internal/locator"
)

// Input is the structured payload both renderers consume. `Now` is injected
// rather than read from time.Now() so reltime golden tests stay deterministic.
type Input struct {
	Symbol   locator.Symbol
	Path     string
	Groups   []forge.Group
	Commits  []history.Commit
	Owner    history.Owner
	HasOwner bool
	Now      time.Time
}

// Human writes the timeline-style output to w. See chat_with_claude.md for the
// target shape and the package doc for the ordering contract.
func Human(w io.Writer, in Input) error {
	return renderHuman(w, in)
}

// JSON writes the v1 schema document to w. See docs/SCHEMA.md.
func JSON(w io.Writer, in Input) error {
	return renderJSON(w, in)
}

// ResetColors re-evaluates whether color codes should be emitted. The package
// init binds color.NoColor from os.Stdout at startup; callers (cmd/wcaw) pass
// the stdout TTY state explicitly so the renderer doesn't have to know about
// os.Stdout. NO_COLOR (https://no-color.org) is always honored when set.
func ResetColors(stdoutIsTTY bool) {
	color.NoColor = !stdoutIsTTY || os.Getenv("NO_COLOR") != ""
}

func init() {
	fi, err := os.Stdout.Stat()
	isTTY := err == nil && fi != nil && fi.Mode()&os.ModeCharDevice != 0
	ResetColors(isTTY)
}

// reverseGroups returns a chronological (oldest-first) copy of gs.
func reverseGroups(gs []forge.Group) []forge.Group {
	out := make([]forge.Group, len(gs))
	for i, g := range gs {
		out[len(gs)-1-i] = g
	}
	return out
}
