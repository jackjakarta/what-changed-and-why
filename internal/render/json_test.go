package render

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestJSONRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	if err := JSON(&buf, sampleInput()); err != nil {
		t.Fatalf("JSON: %v", err)
	}

	var doc map[string]any
	if err := json.Unmarshal(buf.Bytes(), &doc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if got := doc["schema_version"]; got != float64(1) {
		t.Errorf("schema_version: got %v, want 1", got)
	}
	if _, ok := doc["symbol"]; !ok {
		t.Errorf("missing symbol")
	}
	summary, ok := doc["summary"].(map[string]any)
	if !ok {
		t.Fatalf("summary missing or wrong shape")
	}
	if summary["touching_commits"] != float64(5) {
		t.Errorf("touching_commits: got %v, want 5", summary["touching_commits"])
	}
	if summary["pr_count"] != float64(3) {
		t.Errorf("pr_count: got %v, want 3", summary["pr_count"])
	}
	groups, ok := doc["groups"].([]any)
	if !ok || len(groups) != 4 {
		t.Fatalf("groups: got len=%d, want 4", len(groups))
	}

	// Groups must be chronological (oldest first → PR #142 first).
	g0 := groups[0].(map[string]any)
	p0, ok := g0["pull"].(map[string]any)
	if !ok {
		t.Fatalf("first group should have a pull (PR #142)")
	}
	if p0["number"] != float64(142) {
		t.Errorf("first group PR number: got %v, want 142", p0["number"])
	}

	// Last group is the no-PR bucket.
	gN := groups[len(groups)-1].(map[string]any)
	if gN["pull"] != nil {
		t.Errorf("last group should have null pull, got %v", gN["pull"])
	}

	owner, ok := doc["owner"].(map[string]any)
	if !ok {
		t.Fatalf("owner missing")
	}
	if owner["name"] != "maria" {
		t.Errorf("owner.name: got %v, want maria", owner["name"])
	}
	if owner["percent"] != float64(60) {
		t.Errorf("owner.percent: got %v, want 60", owner["percent"])
	}
}

func TestJSONOwnerNullWhenAbsent(t *testing.T) {
	in := sampleInput()
	in.HasOwner = false
	var buf bytes.Buffer
	if err := JSON(&buf, in); err != nil {
		t.Fatalf("JSON: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(buf.Bytes(), &doc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if doc["owner"] != nil {
		t.Fatalf("owner should be null, got %v", doc["owner"])
	}
}

func TestJSONGoldenSample(t *testing.T) {
	var buf bytes.Buffer
	if err := JSON(&buf, sampleInput()); err != nil {
		t.Fatalf("JSON: %v", err)
	}
	goldenPath := filepath.Join("testdata", "sample.json")
	if os.Getenv("UPDATE_GOLDEN") != "" {
		if err := os.WriteFile(goldenPath, buf.Bytes(), 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		t.Logf("updated %s", goldenPath)
		return
	}
	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden: %v (run with UPDATE_GOLDEN=1 to create)", err)
	}
	got := buf.String()
	if strings.TrimRight(got, "\n") != strings.TrimRight(string(want), "\n") {
		t.Fatalf("JSON output diverges from golden %s.\ngot:\n%s\nwant:\n%s",
			goldenPath, got, string(want))
	}
}
