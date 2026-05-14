package forge

import (
	"context"
	"fmt"
	"os"

	"github.com/go-git/go-git/v5"
	"github.com/google/go-github/v66/github"
)

// GitHubForge resolves commit-to-PR via the GitHub REST API. Constructed via
// NewGitHubFromRepo so it can discover (owner, repo) from go-git and handle
// token resolution + anonymous-mode warning in one place.
type GitHubForge struct {
	client *github.Client
	owner  string
	repo   string
}

// NewGitHubFromRepo discovers the github.com remote on repo, resolves a token
// (env → config → gh CLI → anonymous), and returns a ready Forge. The
// configToken arg lets cmd/wcaw supply a token from ~/.config/wcaw/config.json
// without this package importing the config package. Returns ErrNoGitHubRemote
// when neither origin nor upstream points at github.com. Anonymous mode emits
// a single one-line stderr warning so the rate-limit reality is visible
// without flooding stderr on every commit lookup.
func NewGitHubFromRepo(ctx context.Context, repo *git.Repository, configToken string) (*GitHubForge, error) {
	owner, name, err := discoverGitHubRemote(repo)
	if err != nil {
		return nil, err
	}
	token, source := resolveToken(ctx, configToken)
	if token == "" {
		fmt.Fprintln(os.Stderr, "wcaw: no GitHub token (GITHUB_TOKEN/GH_TOKEN/config/gh auth token); using anonymous API (60 req/hr)")
	} else {
		_ = source
	}
	client := github.NewClient(newAuthedHTTPClient(token))
	return &GitHubForge{client: client, owner: owner, repo: name}, nil
}

// Owner returns the github.com owner ("login") this forge was bound to. The
// caching layer reads it to derive a per-repo key namespace without widening
// the Forge interface.
func (f *GitHubForge) Owner() string { return f.owner }

// Repo returns the github.com repository name this forge was bound to.
func (f *GitHubForge) Repo() string { return f.repo }

// PullsForCommit asks GitHub which PRs are associated with the given commit.
// Primary path is the commit-to-PRs REST endpoint; on an empty primary result,
// it falls back to the search API restricted to merged PRs. Network/API
// errors surface to the caller so Group can apply its degradation rules.
func (f *GitHubForge) PullsForCommit(ctx context.Context, sha string) ([]PullRef, error) {
	prs, _, err := f.client.PullRequests.ListPullRequestsWithCommit(ctx, f.owner, f.repo, sha, nil)
	if err != nil {
		return nil, fmt.Errorf("list pulls for %s: %w", short(sha), err)
	}
	if len(prs) > 0 {
		return prsToRefs(prs), nil
	}

	q := fmt.Sprintf("%s type:pr is:merged repo:%s/%s", sha, f.owner, f.repo)
	res, _, serr := f.client.Search.Issues(ctx, q, nil)
	if serr != nil {
		return nil, fmt.Errorf("search pulls for %s: %w", short(sha), serr)
	}
	if res == nil || len(res.Issues) == 0 {
		return nil, nil
	}
	return issuesToRefs(res.Issues), nil
}

func prsToRefs(prs []*github.PullRequest) []PullRef {
	out := make([]PullRef, 0, len(prs))
	for _, pr := range prs {
		if pr == nil {
			continue
		}
		out = append(out, PullRef{
			Number:   pr.GetNumber(),
			Title:    pr.GetTitle(),
			Author:   pr.GetUser().GetLogin(),
			URL:      pr.GetHTMLURL(),
			MergedAt: pr.GetMergedAt().Time,
			MergeSHA: pr.GetMergeCommitSHA(),
			State:    pr.GetState(),
			Body:     pr.GetBody(),
		})
	}
	return out
}

func issuesToRefs(issues []*github.Issue) []PullRef {
	out := make([]PullRef, 0, len(issues))
	for _, iss := range issues {
		if iss == nil || iss.PullRequestLinks == nil {
			continue
		}
		var merged = iss.PullRequestLinks.GetMergedAt().Time
		out = append(out, PullRef{
			Number:   iss.GetNumber(),
			Title:    iss.GetTitle(),
			Author:   iss.GetUser().GetLogin(),
			URL:      iss.GetHTMLURL(),
			MergedAt: merged,
			State:    iss.GetState(),
			Body:     iss.GetBody(),
		})
	}
	return out
}

func short(sha string) string {
	if len(sha) >= 7 {
		return sha[:7]
	}
	return sha
}
