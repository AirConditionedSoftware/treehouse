// Package forge resolves pull-request metadata from code-hosting providers.
// A Forge answers "what is this PR?" and "which hidden ref fetches its
// head?"; running git stays with the caller.
package forge

import (
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/AirConditionedSoftware/treehouse/internal/gitx"
)

// PR is pull-request metadata as learned from the forge's CLI.
type PR struct {
	Number      int
	State       string // gh casing: "OPEN", "CLOSED", "MERGED"
	Title       string
	URL         string
	HeadRef     string // the PR author's branch name
	IsCrossRepo bool   // head lives in a fork
	HeadOwner   string // login of the head repo's owner, for messages
}

// Forge resolves pull-request metadata for one hosting provider.
type Forge interface {
	Name() string
	// ResolvePR asks the forge's CLI about PR number for the repo at dir.
	// A missing or failing CLI is an ordinary error; callers fall back to
	// fetching PullHeadRef with plain git.
	ResolvePR(dir string, number int) (PR, error)
	// PullHeadRef is the hidden ref that fetches the PR head straight from
	// the remote without any CLI: refs/pull/<n>/head on GitHub.
	PullHeadRef(number int) string
}

// Detect picks the forge for the repo at dir from origin's URL. GitLab is
// recognized only to fail with a clear message; every other host — including
// GitHub Enterprise and forges that serve the same refs/pull/<n>/head
// convention — gets the GitHub forge.
func Detect(dir string) (Forge, error) {
	remote, err := gitx.Run(dir, "remote", "get-url", "origin")
	if err != nil {
		return nil, err
	}
	return detectFromRemote(remote)
}

func detectFromRemote(remote string) (Forge, error) {
	host := hostFromRemoteURL(remote)
	if strings.Contains(host, "gitlab") {
		return nil, fmt.Errorf("GitLab merge requests are not supported yet (origin is %s)", host)
	}
	return gitHub{}, nil
}

// hostFromRemoteURL extracts the host from a git remote URL: URL-style
// (https://, ssh://, git://), scp-style (git@host:path), or "" for local
// paths.
func hostFromRemoteURL(remote string) string {
	if u, err := url.Parse(remote); err == nil && u.Host != "" {
		return u.Hostname()
	}
	// scp-style [user@]host:path — a colon before the first slash.
	head, _, ok := strings.Cut(remote, ":")
	if !ok || head == "" || strings.Contains(head, "/") {
		return ""
	}
	if _, host, ok := strings.Cut(head, "@"); ok {
		return host
	}
	return head
}

var prURLPath = regexp.MustCompile(`^/[^/]+/[^/]+/pull/(\d+)(?:/|$)`)

// ParsePRArg extracts a pull-request number from "123", "#123", or a GitHub
// PR URL like https://github.com/OWNER/REPO/pull/123 (extra path segments,
// query strings, and fragments are tolerated).
func ParsePRArg(arg string) (int, error) {
	if n, err := strconv.Atoi(strings.TrimPrefix(arg, "#")); err == nil {
		if n <= 0 {
			return 0, fmt.Errorf("invalid pull request number %q", arg)
		}
		return n, nil
	}
	if u, err := url.Parse(arg); err == nil && u.Host != "" {
		if strings.Contains(u.Hostname(), "gitlab") || strings.Contains(u.Path, "/merge_requests/") {
			return 0, fmt.Errorf("GitLab merge requests are not supported yet: %s", arg)
		}
		if m := prURLPath.FindStringSubmatch(u.Path); m != nil {
			if n, err := strconv.Atoi(m[1]); err == nil && n > 0 {
				return n, nil
			}
		}
	}
	return 0, fmt.Errorf("expected a pull request number or URL, got %q", arg)
}
