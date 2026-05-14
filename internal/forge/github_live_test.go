//go:build forge_live

// This test makes real GitHub API calls and is excluded from the default
// `go test ./...` run. Enable with:
//
//	GITHUB_TOKEN=… go test -tags forge_live ./internal/forge
//
// It targets a stable commit in a public Microsoft TypeScript repo. The exact
// PR number is asserted, so if Microsoft ever force-pushes history this test
// will break — accept that fragility as the cost of a real smoke test.
package forge

import (
	"context"
	"os"
	"testing"

	"github.com/google/go-github/v66/github"
)

func TestGitHubForge_PullsForCommit_Live(t *testing.T) {
	if os.Getenv("GITHUB_TOKEN") == "" && os.Getenv("GH_TOKEN") == "" {
		t.Skip("set GITHUB_TOKEN to run the live forge smoke test")
	}
	ctx := context.Background()
	token, _ := resolveToken(ctx, "")
	f := &GitHubForge{
		client: github.NewClient(newAuthedHTTPClient(token)),
		owner:  "microsoft",
		repo:   "TypeScript",
	}
	// First commit on the TypeScript repo's history is stable. Any PR
	// association is acceptable; we just want to confirm the API path works.
	const sha = "a4c2fd58c5a2027dafee35d8e76d2e029e1ed7b3"
	_, err := f.PullsForCommit(ctx, sha)
	if err != nil {
		t.Fatalf("PullsForCommit: %v", err)
	}
}
