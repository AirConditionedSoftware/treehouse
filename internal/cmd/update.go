package cmd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/AirConditionedSoftware/treehouse/internal/config"
)

// latestReleaseAPI is a variable so tests can point it at a local server.
var latestReleaseAPI = "https://api.github.com/repos/AirConditionedSoftware/treehouse/releases/latest"

const releasesPage = "https://github.com/AirConditionedSoftware/treehouse/releases/latest"

// versionRequested reports whether this invocation is a version query, the
// only time the update check may run.
func versionRequested(args []string) bool {
	for _, a := range args {
		if a == "--version" || a == "-v" {
			return true
		}
	}
	return false
}

// updateNotice returns a message when the update check is enabled and GitHub
// has a newer release than current. Everything that can go wrong — check
// disabled, dev build, network down, rate-limited, unparseable versions —
// returns "" silently: a version query must never fail or slow down loudly
// because of the network.
func updateNotice(current string) string {
	cfg, err := config.Load()
	if err != nil || !cfg.UpdateCheckEnabled() {
		return ""
	}
	if _, ok := parseSemver(current); !ok {
		return "" // dev builds have nothing meaningful to compare
	}

	client := &http.Client{Timeout: 2500 * time.Millisecond}
	req, err := http.NewRequest(http.MethodGet, latestReleaseAPI, nil)
	if err != nil {
		return ""
	}
	req.Header.Set("User-Agent", "treehouse-update-check")
	resp, err := client.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ""
	}
	var release struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return ""
	}

	if !newerVersion(current, release.TagName) {
		return ""
	}
	return fmt.Sprintf("A newer release is available: %s (you have v%s)\n%s",
		release.TagName, strings.TrimPrefix(current, "v"), releasesPage)
}

// newerVersion reports whether latest is a strictly newer semver than
// current; unparseable input is never "newer".
func newerVersion(current, latest string) bool {
	c, ok := parseSemver(current)
	if !ok {
		return false
	}
	l, ok := parseSemver(latest)
	if !ok {
		return false
	}
	for i := range c {
		if l[i] != c[i] {
			return l[i] > c[i]
		}
	}
	return false
}

func parseSemver(v string) ([3]int, bool) {
	var out [3]int
	parts := strings.Split(strings.TrimPrefix(strings.TrimSpace(v), "v"), ".")
	if len(parts) != 3 {
		return out, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return out, false
		}
		out[i] = n
	}
	return out, true
}
