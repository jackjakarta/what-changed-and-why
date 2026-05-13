package forge

import (
	"errors"
	"net/url"
	"strings"

	"github.com/go-git/go-git/v5"
)

// ErrNoGitHubRemote is returned when no usable github.com remote is configured
// on the repository. Callers should treat it as a soft signal to skip forge
// enrichment, not a hard failure.
var ErrNoGitHubRemote = errors.New("no github remote configured")

// discoverGitHubRemote inspects the repo's remotes (origin first, then upstream)
// and returns the (owner, name) for the first URL that points at github.com.
// Returns ErrNoGitHubRemote when neither remote yields a github.com URL.
func discoverGitHubRemote(repo *git.Repository) (string, string, error) {
	for _, remoteName := range []string{"origin", "upstream"} {
		r, err := repo.Remote(remoteName)
		if err != nil {
			continue
		}
		for _, u := range r.Config().URLs {
			if owner, name, ok := parseRemoteURL(u); ok {
				return owner, name, nil
			}
		}
	}
	return "", "", ErrNoGitHubRemote
}

// parseRemoteURL recognises the three URL shapes git tooling produces for
// GitHub repos and returns (owner, repo, true) when the host is github.com.
//
//	https://github.com/owner/repo(.git)?
//	ssh://git@github.com/owner/repo(.git)?
//	git@github.com:owner/repo(.git)?   (scp-like, not a true URL)
//
// Anything else (gitlab, bitbucket, missing path, etc.) returns ("","",false).
func parseRemoteURL(raw string) (string, string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", "", false
	}

	// scp-like: git@github.com:owner/repo(.git)?
	if strings.HasPrefix(raw, "git@") && strings.Contains(raw, ":") && !strings.Contains(raw, "://") {
		at := strings.Index(raw, "@")
		colon := strings.Index(raw, ":")
		if at >= 0 && colon > at {
			host := raw[at+1 : colon]
			path := raw[colon+1:]
			return splitOwnerRepo(host, path)
		}
	}

	u, err := url.Parse(raw)
	if err != nil {
		return "", "", false
	}
	if u.Host == "" || u.Path == "" {
		return "", "", false
	}
	return splitOwnerRepo(u.Host, u.Path)
}

func splitOwnerRepo(host, path string) (string, string, bool) {
	if !strings.EqualFold(host, "github.com") {
		return "", "", false
	}
	path = strings.TrimPrefix(path, "/")
	path = strings.TrimSuffix(path, "/")
	path = strings.TrimSuffix(path, ".git")
	parts := strings.Split(path, "/")
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}
