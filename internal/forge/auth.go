package forge

import (
	"context"
	"net/http"
	"os"
	"os/exec"
	"strings"
)

// resolveToken returns a GitHub token plus a human-readable source label.
// Resolution order:
//  1. GITHUB_TOKEN env var
//  2. GH_TOKEN env var (used by the gh CLI itself)
//  3. `gh auth token` shell-out
//  4. ("", "anonymous") — anonymous GitHub API access (60 req/hr)
func resolveToken(ctx context.Context) (string, string) {
	if t := strings.TrimSpace(os.Getenv("GITHUB_TOKEN")); t != "" {
		return t, "GITHUB_TOKEN"
	}
	if t := strings.TrimSpace(os.Getenv("GH_TOKEN")); t != "" {
		return t, "GH_TOKEN"
	}
	cmd := exec.CommandContext(ctx, "gh", "auth", "token")
	out, err := cmd.Output()
	if err == nil {
		if t := strings.TrimSpace(string(out)); t != "" {
			return t, "gh auth token"
		}
	}
	return "", "anonymous"
}

// bearerTransport adds an Authorization: Bearer header to every request. It
// exists to avoid pulling in golang.org/x/oauth2 just for this one need.
type bearerTransport struct {
	token string
	base  http.RoundTripper
}

func (t *bearerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r2 := r.Clone(r.Context())
	r2.Header.Set("Authorization", "Bearer "+t.token)
	return t.base.RoundTrip(r2)
}

func newAuthedHTTPClient(token string) *http.Client {
	if token == "" {
		return http.DefaultClient
	}
	return &http.Client{Transport: &bearerTransport{token: token, base: http.DefaultTransport}}
}
