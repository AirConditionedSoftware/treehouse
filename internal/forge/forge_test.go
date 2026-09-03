package forge

import (
	"strings"
	"testing"
)

func TestParsePRArg(t *testing.T) {
	valid := []struct {
		arg  string
		want int
	}{
		{"123", 123},
		{"#123", 123},
		{"https://github.com/acme/myapp/pull/123", 123},
		{"https://github.com/acme/myapp/pull/123/files", 123},
		{"https://github.com/acme/myapp/pull/123/commits", 123},
		{"https://github.com/acme/myapp/pull/123?w=1", 123},
		{"https://github.com/acme/myapp/pull/123#discussion_r1", 123},
		{"https://ghe.example.com/acme/myapp/pull/7", 7},
	}
	for _, tt := range valid {
		got, err := ParsePRArg(tt.arg)
		if err != nil {
			t.Errorf("ParsePRArg(%q): unexpected error %v", tt.arg, err)
			continue
		}
		if got != tt.want {
			t.Errorf("ParsePRArg(%q) = %d; want %d", tt.arg, got, tt.want)
		}
	}

	invalid := []string{
		"abc",
		"0",
		"-1",
		"#",
		"",
		"https://github.com/acme/myapp/issues/5",
		"https://github.com/acme/myapp/pull/",
		"https://github.com/acme/myapp",
	}
	for _, arg := range invalid {
		if got, err := ParsePRArg(arg); err == nil {
			t.Errorf("ParsePRArg(%q) = %d; want error", arg, got)
		}
	}

	if _, err := ParsePRArg("https://gitlab.com/grp/proj/-/merge_requests/9"); err == nil ||
		!strings.Contains(err.Error(), "GitLab") {
		t.Errorf("GitLab MR URL: err = %v; want a GitLab-specific message", err)
	}
}

func TestHostFromRemoteURL(t *testing.T) {
	tests := []struct {
		remote, want string
	}{
		{"git@github.com:acme/myapp.git", "github.com"},
		{"ssh://git@github.com/acme/myapp.git", "github.com"},
		{"https://github.com/acme/myapp.git", "github.com"},
		{"https://gitlab.com/grp/proj.git", "gitlab.com"},
		{"git@gitlab.example.com:grp/proj.git", "gitlab.example.com"},
		{"/some/local/path", ""},
		{"../relative/path", ""},
		{"", ""},
	}
	for _, tt := range tests {
		if got := hostFromRemoteURL(tt.remote); got != tt.want {
			t.Errorf("hostFromRemoteURL(%q) = %q; want %q", tt.remote, got, tt.want)
		}
	}
}

func TestDetectFromRemote(t *testing.T) {
	for _, remote := range []string{
		"git@github.com:acme/myapp.git",
		"https://ghe.example.com/acme/myapp.git",
		"/some/local/path", // e2e scratch repos use path remotes
	} {
		f, err := detectFromRemote(remote)
		if err != nil {
			t.Errorf("detectFromRemote(%q): unexpected error %v", remote, err)
			continue
		}
		if f.Name() != "github" {
			t.Errorf("detectFromRemote(%q).Name() = %q; want github", remote, f.Name())
		}
	}

	for _, remote := range []string{
		"git@gitlab.com:grp/proj.git",
		"https://gitlab.example.com/grp/proj.git",
	} {
		if _, err := detectFromRemote(remote); err == nil || !strings.Contains(err.Error(), "GitLab") {
			t.Errorf("detectFromRemote(%q): err = %v; want a GitLab-specific message", remote, err)
		}
	}
}
