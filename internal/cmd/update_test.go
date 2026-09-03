package cmd

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVersionRequested(t *testing.T) {
	tests := []struct {
		args []string
		want bool
	}{
		{[]string{"--version"}, true},
		{[]string{"-v"}, true},
		{[]string{"list"}, false},
		{[]string{"add", "x"}, false},
		{nil, false},
	}
	for _, tt := range tests {
		if got := versionRequested(tt.args); got != tt.want {
			t.Errorf("versionRequested(%v) = %v; want %v", tt.args, got, tt.want)
		}
	}
}

func TestNewerVersion(t *testing.T) {
	tests := []struct {
		current, latest string
		want            bool
	}{
		{"0.2.5", "v0.2.6", true},
		{"0.2.5", "v0.3.0", true},
		{"0.2.5", "v1.0.0", true},
		{"0.2.5", "v0.2.5", false},
		{"0.3.0", "v0.2.9", false},
		{"dev", "v9.9.9", false},
		{"0.2.5", "not-a-version", false},
	}
	for _, tt := range tests {
		if got := newerVersion(tt.current, tt.latest); got != tt.want {
			t.Errorf("newerVersion(%q, %q) = %v; want %v", tt.current, tt.latest, got, tt.want)
		}
	}
}

// serveLatest points the update check at a local server for the test.
func serveLatest(t *testing.T, status int, body string) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	prev := latestReleaseAPI
	latestReleaseAPI = srv.URL
	t.Cleanup(func() { latestReleaseAPI = prev })
}

// writeUpdateConfig points TH_CONFIG at a config with the given content.
func writeUpdateConfig(t *testing.T, content string) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "th.json")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TH_CONFIG", p)
}

func TestUpdateNotice(t *testing.T) {
	t.Run("disabled by default", func(t *testing.T) {
		writeUpdateConfig(t, `{}`)
		serveLatest(t, 200, `{"tag_name": "v9.9.9"}`)
		if got := updateNotice("0.2.5"); got != "" {
			t.Errorf("notice without update_check = %q; want none", got)
		}
	})

	t.Run("newer release mentioned", func(t *testing.T) {
		writeUpdateConfig(t, `{"update_check": true}`)
		serveLatest(t, 200, `{"tag_name": "v9.9.9"}`)
		got := updateNotice("0.2.5")
		if !strings.Contains(got, "v9.9.9") || !strings.Contains(got, "you have v0.2.5") {
			t.Errorf("notice = %q; want mention of v9.9.9 and current version", got)
		}
	})

	t.Run("up to date is silent", func(t *testing.T) {
		writeUpdateConfig(t, `{"update_check": true}`)
		serveLatest(t, 200, `{"tag_name": "v0.2.5"}`)
		if got := updateNotice("0.2.5"); got != "" {
			t.Errorf("notice when current = %q; want none", got)
		}
	})

	t.Run("dev build is silent", func(t *testing.T) {
		writeUpdateConfig(t, `{"update_check": true}`)
		serveLatest(t, 200, `{"tag_name": "v9.9.9"}`)
		if got := updateNotice("dev"); got != "" {
			t.Errorf("notice for dev build = %q; want none", got)
		}
	})

	t.Run("api failure is silent", func(t *testing.T) {
		writeUpdateConfig(t, `{"update_check": true}`)
		serveLatest(t, 403, `{"message": "rate limited"}`)
		if got := updateNotice("0.2.5"); got != "" {
			t.Errorf("notice on API failure = %q; want none", got)
		}
	})

	t.Run("garbage response is silent", func(t *testing.T) {
		writeUpdateConfig(t, `{"update_check": true}`)
		serveLatest(t, 200, `not json`)
		if got := updateNotice("0.2.5"); got != "" {
			t.Errorf("notice on bad JSON = %q; want none", got)
		}
	})
}
