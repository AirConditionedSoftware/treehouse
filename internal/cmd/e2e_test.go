package cmd_test

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/AirConditionedSoftware/treehouse/internal/config"
)

var thBin string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "th-e2e")
	if err != nil {
		panic(err)
	}
	thBin = filepath.Join(dir, "th")
	root, err := filepath.Abs("../..")
	if err != nil {
		panic(err)
	}
	build := exec.Command("go", "build", "-o", thBin, "./cmd/th")
	build.Dir = root
	if out, err := build.CombinedOutput(); err != nil {
		panic("building th: " + err.Error() + "\n" + string(out))
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

// gitEnv isolates git from the developer's real global/system config.
func gitEnv(home string) []string {
	return append(os.Environ(),
		"HOME="+home,
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_AUTHOR_NAME=th-test",
		"GIT_AUTHOR_EMAIL=th@test.invalid",
		"GIT_COMMITTER_NAME=th-test",
		"GIT_COMMITTER_EMAIL=th@test.invalid",
	)
}

func git(t *testing.T, home, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = gitEnv(home)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func th(t *testing.T, home, cfg, dir string, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	return thWithEnv(t, home, cfg, dir, nil, args...)
}

func thWithEnv(t *testing.T, home, cfg, dir string, extraEnv []string, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	cmd := exec.Command(thBin, args...)
	cmd.Dir = dir
	cmd.Env = append(append(gitEnv(home), "TH_CONFIG="+cfg), extraEnv...)
	var so, se strings.Builder
	cmd.Stdout = &so
	cmd.Stderr = &se
	err = cmd.Run()
	return strings.TrimSpace(so.String()), se.String(), err
}

func TestEndToEnd(t *testing.T) {
	home := t.TempDir()
	work := t.TempDir()

	// A source repo that will act as origin, with a branch that only exists
	// there.
	origin := filepath.Join(work, "origin-src")
	if err := os.MkdirAll(origin, 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, home, origin, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(origin, "file.txt"), []byte("hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, home, origin, "add", ".")
	git(t, home, origin, "commit", "-m", "init")
	git(t, home, origin, "branch", "remote-only")

	git(t, home, work, "clone", origin, "myapp")
	repo := filepath.Join(work, "myapp")

	trees := filepath.Join(work, "trees")
	cfg := filepath.Join(work, "th.json")
	cfgJSON := `{
  "worktree_dir": "` + trees + `/{repo}/{branch}",
  "repos": [{"name": "myapp", "path": "` + repo + `", "default_base": "main"}]
}`
	if err := os.WriteFile(cfg, []byte(cfgJSON), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Run("add new branch", func(t *testing.T) {
		out, _, err := th(t, home, cfg, repo, "add", "feature/login")
		if err != nil {
			t.Fatal(err)
		}
		want := filepath.Join(trees, "myapp", "feature-login")
		if out != want {
			t.Errorf("stdout = %q; want %q", out, want)
		}
		if got := git(t, home, want, "rev-parse", "--abbrev-ref", "HEAD"); got != "feature/login" {
			t.Errorf("checked-out branch = %q; want feature/login", got)
		}
	})

	t.Run("add existing local branch", func(t *testing.T) {
		git(t, home, repo, "branch", "local-b")
		out, _, err := th(t, home, cfg, repo, "add", "local-b")
		if err != nil {
			t.Fatal(err)
		}
		want := filepath.Join(trees, "myapp", "local-b")
		if out != want {
			t.Errorf("stdout = %q; want %q", out, want)
		}
	})

	t.Run("add remote branch tracks origin", func(t *testing.T) {
		out, _, err := th(t, home, cfg, repo, "add", "remote-only")
		if err != nil {
			t.Fatal(err)
		}
		upstream := git(t, home, out, "rev-parse", "--abbrev-ref", "remote-only@{upstream}")
		if upstream != "origin/remote-only" {
			t.Errorf("upstream = %q; want origin/remote-only", upstream)
		}
	})

	t.Run("add already checked out branch fails", func(t *testing.T) {
		_, stderr, err := th(t, home, cfg, repo, "add", "main")
		if err == nil {
			t.Fatal("expected error for already checked out branch")
		}
		if !strings.Contains(stderr, "already checked out") {
			t.Errorf("stderr = %q; want mention of already checked out", stderr)
		}
	})

	t.Run("list", func(t *testing.T) {
		out, _, err := th(t, home, cfg, repo, "list")
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{"main", "feature/login", "local-b", "remote-only"} {
			if !strings.Contains(out, want) {
				t.Errorf("list output missing %q:\n%s", want, out)
			}
		}
		if strings.Contains(out, "\x1b") {
			t.Errorf("list output contains ANSI escapes without a terminal:\n%q", out)
		}
		if !strings.Contains(out, "ago)") {
			t.Errorf("list output missing relative commit age:\n%s", out)
		}
		if !strings.Contains(out, "0 unstaged") {
			t.Errorf("list output missing unstaged count:\n%s", out)
		}
		if !strings.Contains(out, "merged into main") {
			t.Errorf("list output missing merge status:\n%s", out)
		}
		if !strings.Contains(out, "[feature/login]") {
			t.Errorf("list output missing bracketed branch:\n%s", out)
		}
		// Every branch here is either in sync with its upstream or has
		// none, so the sync segment stays quiet.
		for _, quiet := range []string{"↑", "↓", "upstream gone"} {
			if strings.Contains(out, quiet) {
				t.Errorf("list output shows %q though nothing is out of sync:\n%s", quiet, out)
			}
		}
	})

	// entryByBranch returns the `list --json` entry for branch out of out.
	entryByBranch := func(t *testing.T, out, branch string) map[string]any {
		t.Helper()
		var wts []map[string]any
		if err := json.Unmarshal([]byte(out), &wts); err != nil {
			t.Fatalf("list --json produced invalid JSON: %v\n%s", err, out)
		}
		for _, w := range wts {
			if w["branch"] == branch {
				return w
			}
		}
		t.Fatalf("list --json has no entry for branch %q:\n%s", branch, out)
		return nil
	}

	t.Run("list json", func(t *testing.T) {
		out, _, err := th(t, home, cfg, repo, "list", "--json")
		if err != nil {
			t.Fatal(err)
		}
		var wts []map[string]any
		if err := json.Unmarshal([]byte(out), &wts); err != nil {
			t.Fatalf("list --json produced invalid JSON: %v\n%s", err, out)
		}
		if len(wts) != 4 {
			t.Errorf("list --json returned %d worktrees; want 4", len(wts))
		}
		// main tracks origin/main and is in sync: both counts present at 0.
		mainWT := entryByBranch(t, out, "main")
		if a, ok := mainWT["ahead"].(float64); !ok || a != 0 {
			t.Errorf("main ahead = %v; want present and 0", mainWT["ahead"])
		}
		if b, ok := mainWT["behind"].(float64); !ok || b != 0 {
			t.Errorf("main behind = %v; want present and 0", mainWT["behind"])
		}
		// feature/login has no upstream: both keys absent.
		fl := entryByBranch(t, out, "feature/login")
		for _, key := range []string{"ahead", "behind"} {
			if v, ok := fl[key]; ok {
				t.Errorf("feature/login %s = %v; want the key absent without an upstream", key, v)
			}
		}
	})

	t.Run("list shows ahead/behind vs upstream", func(t *testing.T) {
		wt := filepath.Join(trees, "myapp", "remote-only")
		// A local commit the upstream doesn't have.
		if err := os.WriteFile(filepath.Join(wt, "ahead.txt"), []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		git(t, home, wt, "add", ".")
		git(t, home, wt, "commit", "-m", "local work")

		out, _, err := th(t, home, cfg, repo, "list")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, "↑1") {
			t.Errorf("list output missing ↑1 for the unpushed commit:\n%s", out)
		}
		if strings.Contains(out, "↓") {
			t.Errorf("list output shows ↓ though the branch is not behind:\n%s", out)
		}
		if strings.Contains(out, "\x1b") {
			t.Errorf("piped list output contains ANSI escapes:\n%q", out)
		}

		// Advance the upstream too: a commit on origin's remote-only,
		// fetched into the clone, puts the branch ahead and behind at once.
		git(t, home, origin, "checkout", "remote-only")
		if err := os.WriteFile(filepath.Join(origin, "behind.txt"), []byte("y\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		git(t, home, origin, "add", ".")
		git(t, home, origin, "commit", "-m", "remote work")
		git(t, home, origin, "checkout", "main")
		git(t, home, repo, "fetch", "origin")

		out, _, err = th(t, home, cfg, repo, "list")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, "↑1 ↓1") {
			t.Errorf("list output missing the joined ↑1 ↓1 segment:\n%s", out)
		}

		jsonOut, _, err := th(t, home, cfg, repo, "list", "--json")
		if err != nil {
			t.Fatal(err)
		}
		ro := entryByBranch(t, jsonOut, "remote-only")
		if a, ok := ro["ahead"].(float64); !ok || a != 1 {
			t.Errorf("remote-only ahead = %v; want 1", ro["ahead"])
		}
		if b, ok := ro["behind"].(float64); !ok || b != 1 {
			t.Errorf("remote-only behind = %v; want 1", ro["behind"])
		}

		// Drop the local commit and stay one behind the upstream.
		git(t, home, wt, "reset", "--hard", "origin/remote-only~1")
		out, _, err = th(t, home, cfg, repo, "list")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, "↓1") || strings.Contains(out, "↑") {
			t.Errorf("list output should show ↓1 only:\n%s", out)
		}

		// Back in sync — quiet again, and the state later subtests expect.
		git(t, home, wt, "reset", "--hard", "origin/remote-only")
		out, _, err = th(t, home, cfg, repo, "list")
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(out, "↑") || strings.Contains(out, "↓") {
			t.Errorf("list output shows arrows though everything is in sync again:\n%s", out)
		}
	})

	t.Run("list tags gone upstreams", func(t *testing.T) {
		// A branch that exists on origin, checked out tracking it...
		git(t, home, origin, "branch", "ephemeral")
		_, stderr, err := th(t, home, cfg, repo, "add", "ephemeral")
		if err != nil {
			t.Fatalf("%v\n%s", err, stderr)
		}
		// ...and then deleted there.
		git(t, home, origin, "branch", "-D", "ephemeral")
		git(t, home, repo, "fetch", "--prune", "origin")

		out, _, err := th(t, home, cfg, repo, "list")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, "upstream gone") {
			t.Errorf("list output missing the upstream gone tag:\n%s", out)
		}

		jsonOut, _, err := th(t, home, cfg, repo, "list", "--json")
		if err != nil {
			t.Fatal(err)
		}
		eph := entryByBranch(t, jsonOut, "ephemeral")
		if gone, _ := eph["upstream_gone"].(bool); !gone {
			t.Errorf("ephemeral upstream_gone = %v; want true", eph["upstream_gone"])
		}
		for _, key := range []string{"ahead", "behind"} {
			if v, ok := eph[key]; ok {
				t.Errorf("ephemeral %s = %v; want the key absent with a gone upstream", key, v)
			}
		}

		// Clean up so later subtests see the same worktrees and branches
		// as before.
		if _, stderr, err := th(t, home, cfg, repo, "remove", "ephemeral"); err != nil {
			t.Fatalf("%v\n%s", err, stderr)
		}
		git(t, home, repo, "branch", "-D", "ephemeral")
	})

	t.Run("du lists sizes per worktree", func(t *testing.T) {
		out, _, err := th(t, home, cfg, repo, "du")
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{"BRANCH", "main", "TOTAL"} {
			if !strings.Contains(out, want) {
				t.Errorf("du output missing %q:\n%s", want, out)
			}
		}
		out2, _, err := th(t, home, cfg, repo, "du", "--unit", "KB")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out2, "KB") {
			t.Errorf("du --unit KB output has no KB sizes:\n%s", out2)
		}
		if _, _, err := th(t, home, cfg, repo, "du", "--unit", "TB"); err == nil {
			t.Error("du with invalid unit should fail")
		}
	})

	t.Run("list outside a repo fails", func(t *testing.T) {
		if _, _, err := th(t, home, cfg, t.TempDir(), "list"); err == nil {
			t.Error("expected error outside a git repository")
		}
	})

	t.Run("remove worktree keeps branch", func(t *testing.T) {
		out, stderr, err := th(t, home, cfg, repo, "remove", "local-b")
		if err != nil {
			t.Fatalf("%v\n%s", err, stderr)
		}
		if out != "" {
			t.Errorf("stdout = %q; want empty", out)
		}
		path := filepath.Join(trees, "myapp", "local-b")
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("worktree dir %s still exists", path)
		}
		git(t, home, repo, "rev-parse", "--verify", "refs/heads/local-b")
	})

	t.Run("remove via -r flag", func(t *testing.T) {
		if _, stderr, err := th(t, home, cfg, repo, "-r", "remote-only"); err != nil {
			t.Fatalf("%v\n%s", err, stderr)
		}
		path := filepath.Join(trees, "myapp", "remote-only")
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("worktree dir %s still exists", path)
		}
	})

	t.Run("remove shows progress", func(t *testing.T) {
		for _, b := range []string{"rp-a", "rp-b"} {
			if _, stderr, err := th(t, home, cfg, repo, "add", b); err != nil {
				t.Fatalf("%v\n%s", err, stderr)
			}
		}
		_, stderr, err := th(t, home, cfg, repo, "remove", "rp-a", "rp-b")
		if err != nil {
			t.Fatalf("%v\n%s", err, stderr)
		}
		// Piped output narrates each removal with a counter and size up
		// front (the live elapsed ticker is terminal-only).
		for _, want := range []string{"[1/2] Removing ", "[2/2] Removing "} {
			if !strings.Contains(stderr, want) {
				t.Errorf("stderr missing %q:\n%s", want, stderr)
			}
		}
		if !strings.Contains(stderr, " B)") && !strings.Contains(stderr, " KB)") {
			t.Errorf("stderr missing a size suffix:\n%s", stderr)
		}
		if strings.Contains(stderr, "\x1b") {
			t.Errorf("piped stderr contains ANSI escapes:\n%q", stderr)
		}

		// A single removal gets no counter prefix.
		if _, stderr, err = th(t, home, cfg, repo, "add", "rp-c"); err != nil {
			t.Fatalf("%v\n%s", err, stderr)
		}
		_, stderr, err = th(t, home, cfg, repo, "remove", "rp-c")
		if err != nil {
			t.Fatalf("%v\n%s", err, stderr)
		}
		if !strings.Contains(stderr, "Removing ") || strings.Contains(stderr, "[1/1]") {
			t.Errorf("single removal stderr = %q; want Removing line without a counter", stderr)
		}
	})

	t.Run("remove main worktree fails", func(t *testing.T) {
		_, stderr, err := th(t, home, cfg, repo, "remove", "main")
		if err == nil {
			t.Fatal("expected error removing the main worktree")
		}
		if !strings.Contains(stderr, "main worktree") {
			t.Errorf("stderr = %q; want mention of main worktree", stderr)
		}
	})

	t.Run("remove worktree you are in fails", func(t *testing.T) {
		inside := filepath.Join(trees, "myapp", "feature-login")
		_, stderr, err := th(t, home, cfg, inside, "remove", "feature/login")
		if err == nil {
			t.Fatal("expected error removing the current worktree")
		}
		if !strings.Contains(stderr, "worktree you are in") {
			t.Errorf("stderr = %q; want mention of current worktree", stderr)
		}
	})

	t.Run("remove unknown branch fails", func(t *testing.T) {
		_, stderr, err := th(t, home, cfg, repo, "remove", "no-such-branch")
		if err == nil {
			t.Fatal("expected error for unknown worktree")
		}
		if !strings.Contains(stderr, "no worktree found") {
			t.Errorf("stderr = %q; want no-worktree-found error", stderr)
		}
	})

	t.Run("remove multiple branches at once", func(t *testing.T) {
		for _, b := range []string{"multi-a", "multi-b"} {
			if _, _, err := th(t, home, cfg, repo, "add", b); err != nil {
				t.Fatal(err)
			}
		}
		if _, stderr, err := th(t, home, cfg, repo, "remove", "multi-a", "multi-b"); err != nil {
			t.Fatalf("%v\n%s", err, stderr)
		}
		for _, b := range []string{"multi-a", "multi-b"} {
			path := filepath.Join(trees, "myapp", b)
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Errorf("worktree dir %s still exists", path)
			}
		}
	})

	t.Run("remove with no args needs a terminal", func(t *testing.T) {
		_, stderr, err := th(t, home, cfg, repo, "remove")
		if err == nil {
			t.Fatal("expected error for interactive remove without a terminal")
		}
		if !strings.Contains(stderr, "terminal") {
			t.Errorf("stderr = %q; want mention of terminal", stderr)
		}
	})

	t.Run("remove dirty worktree needs force", func(t *testing.T) {
		out, _, err := th(t, home, cfg, repo, "add", "dirty")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(out, "untracked.txt"), []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		_, stderr, err := th(t, home, cfg, repo, "remove", "dirty")
		if err == nil {
			t.Fatal("expected error removing a dirty worktree without --force")
		}
		if !strings.Contains(stderr, "modified or untracked") || !strings.Contains(stderr, "--force") {
			t.Errorf("stderr = %q; want dirty-worktree message pointing at --force", stderr)
		}
		if _, stderr, err := th(t, home, cfg, repo, "remove", "--force", "dirty"); err != nil {
			t.Fatalf("remove --force: %v\n%s", err, stderr)
		}
	})

	cfgPrefix := filepath.Join(work, "th-prefix.json")
	// At the current schema so th prints it back byte for byte: an older
	// file would be migrated and rewritten before th config reads it.
	cfgPrefixJSON := `{
  "version": 2,
  "worktree_dir": "` + trees + `/{repo}/{branch}",
  "branch_prefix": "peter/"
}`
	if err := os.WriteFile(cfgPrefix, []byte(cfgPrefixJSON), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Run("add applies configured branch prefix", func(t *testing.T) {
		out, _, err := th(t, home, cfgPrefix, repo, "add", "widget")
		if err != nil {
			t.Fatal(err)
		}
		want := filepath.Join(trees, "myapp", "peter-widget")
		if out != want {
			t.Errorf("stdout = %q; want %q", out, want)
		}
		if got := git(t, home, out, "rev-parse", "--abbrev-ref", "HEAD"); got != "peter/widget" {
			t.Errorf("checked-out branch = %q; want peter/widget", got)
		}
	})

	t.Run("add finds branch created with prefix earlier", func(t *testing.T) {
		if _, stderr, err := th(t, home, cfgPrefix, repo, "remove", "peter/widget"); err != nil {
			t.Fatalf("%v\n%s", err, stderr)
		}
		_, stderr, err := th(t, home, cfgPrefix, repo, "add", "widget")
		if err != nil {
			t.Fatalf("%v\n%s", err, stderr)
		}
		if !strings.Contains(stderr, "existing branch") {
			t.Errorf("stderr = %q; want reuse of existing branch", stderr)
		}
		path := filepath.Join(trees, "myapp", "peter-widget")
		if got := git(t, home, path, "rev-parse", "--abbrev-ref", "HEAD"); got != "peter/widget" {
			t.Errorf("checked-out branch = %q; want peter/widget", got)
		}
	})

	t.Run("add --no-prefix skips prefix", func(t *testing.T) {
		out, _, err := th(t, home, cfgPrefix, repo, "add", "--no-prefix", "plain")
		if err != nil {
			t.Fatal(err)
		}
		want := filepath.Join(trees, "myapp", "plain")
		if out != want {
			t.Errorf("stdout = %q; want %q", out, want)
		}
		if got := git(t, home, out, "rev-parse", "--abbrev-ref", "HEAD"); got != "plain" {
			t.Errorf("checked-out branch = %q; want plain", got)
		}
	})

	t.Run("prefix not applied to existing branch", func(t *testing.T) {
		git(t, home, repo, "branch", "preexisting")
		out, _, err := th(t, home, cfgPrefix, repo, "add", "preexisting")
		if err != nil {
			t.Fatal(err)
		}
		want := filepath.Join(trees, "myapp", "preexisting")
		if out != want {
			t.Errorf("stdout = %q; want %q", out, want)
		}
		if got := git(t, home, out, "rev-parse", "--abbrev-ref", "HEAD"); got != "preexisting" {
			t.Errorf("checked-out branch = %q; want preexisting", got)
		}
	})

	t.Run("config prints location and content", func(t *testing.T) {
		out, stderr, err := th(t, home, cfgPrefix, repo, "config")
		if err != nil {
			t.Fatalf("%v\n%s", err, stderr)
		}
		if !strings.Contains(stderr, cfgPrefix) || !strings.Contains(stderr, "TH_CONFIG") {
			t.Errorf("stderr = %q; want config path and $TH_CONFIG mention", stderr)
		}
		if out != strings.TrimSpace(cfgPrefixJSON) {
			t.Errorf("stdout = %q; want file content %q", out, cfgPrefixJSON)
		}
	})

	t.Run("config without a file prints defaults", func(t *testing.T) {
		out, stderr, err := th(t, home, "", repo, "config")
		if err != nil {
			t.Fatalf("%v\n%s", err, stderr)
		}
		if !strings.Contains(stderr, "built-in defaults") {
			t.Errorf("stderr = %q; want built-in defaults notice", stderr)
		}
		var def map[string]any
		if err := json.Unmarshal([]byte(out), &def); err != nil {
			t.Fatalf("stdout is not JSON: %v\n%s", err, out)
		}
		if def["worktree_dir"] != "~/worktrees/{repo}/{branch}" {
			t.Errorf("defaults = %v; want default worktree_dir", def)
		}
	})

	t.Run("config with invalid file fails after printing it", func(t *testing.T) {
		bad := filepath.Join(work, "bad.json")
		if err := os.WriteFile(bad, []byte(`{"worktre_dir": "/x"}`), 0o644); err != nil {
			t.Fatal(err)
		}
		out, _, err := th(t, home, bad, repo, "config")
		if err == nil {
			t.Fatal("expected error for invalid config")
		}
		if !strings.Contains(out, "worktre_dir") {
			t.Errorf("stdout = %q; want the file content printed anyway", out)
		}
	})

	t.Run("bare prefix gets default slash separator", func(t *testing.T) {
		cfgBare := filepath.Join(work, "th-bare-prefix.json")
		cfgJSON := `{
  "worktree_dir": "` + trees + `/{repo}/{branch}",
  "branch_prefix": "peter"
}`
		if err := os.WriteFile(cfgBare, []byte(cfgJSON), 0o644); err != nil {
			t.Fatal(err)
		}
		out, _, err := th(t, home, cfgBare, repo, "add", "doohickey")
		if err != nil {
			t.Fatal(err)
		}
		want := filepath.Join(trees, "myapp", "peter-doohickey")
		if out != want {
			t.Errorf("stdout = %q; want %q", out, want)
		}
		if got := git(t, home, out, "rev-parse", "--abbrev-ref", "HEAD"); got != "peter/doohickey" {
			t.Errorf("checked-out branch = %q; want peter/doohickey", got)
		}
	})

	t.Run("custom prefix separator per repo", func(t *testing.T) {
		cfgSep := filepath.Join(work, "th-sep.json")
		cfgJSON := `{
  "worktree_dir": "` + trees + `/{repo}/{branch}",
  "branch_prefix": "peter",
  "repos": [{"path": "` + repo + `", "prefix_separator": "-"}]
}`
		if err := os.WriteFile(cfgSep, []byte(cfgJSON), 0o644); err != nil {
			t.Fatal(err)
		}
		out, _, err := th(t, home, cfgSep, repo, "add", "gizmo")
		if err != nil {
			t.Fatal(err)
		}
		want := filepath.Join(trees, "myapp", "peter-gizmo")
		if out != want {
			t.Errorf("stdout = %q; want %q", out, want)
		}
		if got := git(t, home, out, "rev-parse", "--abbrev-ref", "HEAD"); got != "peter-gizmo" {
			t.Errorf("checked-out branch = %q; want peter-gizmo", got)
		}
	})

	t.Run("repo name overrides {repo} in templates", func(t *testing.T) {
		cfgName := filepath.Join(work, "th-name.json")
		cfgJSON := `{
  "worktree_dir": "` + trees + `/{repo}/{branch}",
  "repos": [{"name": "renamed", "path": "` + repo + `"}]
}`
		if err := os.WriteFile(cfgName, []byte(cfgJSON), 0o644); err != nil {
			t.Fatal(err)
		}
		out, _, err := th(t, home, cfgName, repo, "add", "nametest")
		if err != nil {
			t.Fatal(err)
		}
		want := filepath.Join(trees, "renamed", "nametest")
		if out != want {
			t.Errorf("stdout = %q; want %q", out, want)
		}
	})

	t.Run("copy hooks with relative hooksPath", func(t *testing.T) {
		hooksDir := filepath.Join(repo, ".githooks")
		if err := os.MkdirAll(hooksDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(hooksDir, "pre-commit"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		git(t, home, repo, "config", "core.hooksPath", ".githooks")

		out, stderr, err := th(t, home, cfgPrefix, repo, "add", "--copy-hooks", "hooked")
		if err != nil {
			t.Fatalf("%v\n%s", err, stderr)
		}
		info, err := os.Stat(filepath.Join(out, ".githooks", "pre-commit"))
		if err != nil {
			t.Fatalf("hook not copied into worktree: %v", err)
		}
		if info.Mode().Perm()&0o100 == 0 {
			t.Errorf("copied hook lost its executable bit: %v", info.Mode())
		}
	})

	t.Run("copy_hooks config and --no-copy-hooks override", func(t *testing.T) {
		cfgHooks := filepath.Join(work, "th-hooks.json")
		cfgJSON := `{
  "worktree_dir": "` + trees + `/{repo}/{branch}",
  "repos": [{"path": "` + repo + `", "copy_hooks": true}]
}`
		if err := os.WriteFile(cfgHooks, []byte(cfgJSON), 0o644); err != nil {
			t.Fatal(err)
		}
		out, _, err := th(t, home, cfgHooks, repo, "add", "hooked-cfg")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(filepath.Join(out, ".githooks", "pre-commit")); err != nil {
			t.Errorf("copy_hooks config did not copy hooks: %v", err)
		}
		out2, _, err := th(t, home, cfgHooks, repo, "add", "--no-copy-hooks", "hooked-off")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(filepath.Join(out2, ".githooks")); !os.IsNotExist(err) {
			t.Error("--no-copy-hooks should have prevented the copy")
		}
	})

	t.Run("copy_files config copies untracked files", func(t *testing.T) {
		for name, content := range map[string]string{
			".env":       "SECRET=1\n",
			".env.local": "LOCAL=1\n",
		} {
			if err := os.WriteFile(filepath.Join(repo, name), []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.MkdirAll(filepath.Join(repo, "local-conf"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(repo, "local-conf", "dev.json"), []byte("{}\n"), 0o644); err != nil {
			t.Fatal(err)
		}

		cfgFiles := filepath.Join(work, "th-files.json")
		cfgJSON := `{
  "worktree_dir": "` + trees + `/{repo}/{branch}",
  "repos": [{"path": "` + repo + `", "copy_files": [".env*", "local-conf"]}]
}`
		if err := os.WriteFile(cfgFiles, []byte(cfgJSON), 0o644); err != nil {
			t.Fatal(err)
		}

		out, stderr, err := th(t, home, cfgFiles, repo, "add", "with-files")
		if err != nil {
			t.Fatalf("%v\n%s", err, stderr)
		}
		for _, f := range []string{".env", ".env.local", "local-conf/dev.json"} {
			if _, err := os.Stat(filepath.Join(out, f)); err != nil {
				t.Errorf("%s not copied: %v", f, err)
			}
		}
		// Piped stderr narrates each match up front (so a hanging copy shows
		// what it's stuck on) plus a sized total; live in-place progress is
		// terminal-only.
		for _, want := range []string{"Copying .env\n", "Copying local-conf\n", "Copied 3 file(s) ("} {
			if !strings.Contains(stderr, want) {
				t.Errorf("stderr missing %q:\n%s", want, stderr)
			}
		}
		if strings.Contains(stderr, "\x1b") {
			t.Errorf("piped stderr contains ANSI escapes:\n%q", stderr)
		}

		out2, _, err := th(t, home, cfgFiles, repo, "add", "--no-copy-files", "without-files")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(filepath.Join(out2, ".env")); !os.IsNotExist(err) {
			t.Error("--no-copy-files should have prevented the copy")
		}
	})

	t.Run("copy-file flag without config", func(t *testing.T) {
		out, _, err := th(t, home, cfgPrefix, repo, "add", "--copy-file", ".env", "flag-files")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(filepath.Join(out, ".env")); err != nil {
			t.Errorf(".env not copied via --copy-file: %v", err)
		}
		if _, err := os.Stat(filepath.Join(out, ".env.local")); !os.IsNotExist(err) {
			t.Error(".env.local should not have been copied")
		}
	})

	t.Run("copy hooks with shared default hooks", func(t *testing.T) {
		git(t, home, repo, "config", "--unset", "core.hooksPath")
		_, stderr, err := th(t, home, cfgPrefix, repo, "add", "--copy-hooks", "hooked-shared")
		if err != nil {
			t.Fatalf("%v\n%s", err, stderr)
		}
		if !strings.Contains(stderr, "shared") {
			t.Errorf("stderr = %q; want note that hooks are already shared", stderr)
		}
	})

	t.Run("vscode workspace file with custom prefix and title", func(t *testing.T) {
		cfgVS := filepath.Join(work, "th-vscode.json")
		cfgJSON := `{
  "worktree_dir": "` + trees + `/{repo}/{branch}",
  "repos": [{"path": "` + repo + `", "vscode": {"workspace_file": true, "workspace_prefix": "acs-", "window_title": "myapp — ${activeEditorShort}",
    "workspace_paths": [{"name": "docs", "path": "~/notes"}, {"path": "/shared/lib"}]}}]
}`
		if err := os.WriteFile(cfgVS, []byte(cfgJSON), 0o644); err != nil {
			t.Fatal(err)
		}
		out, stderr, err := th(t, home, cfgVS, repo, "add", "ws-test")
		if err != nil {
			t.Fatalf("%v\n%s", err, stderr)
		}
		wsPath := filepath.Join(filepath.Dir(out), "acs-ws-test.code-workspace")
		data, err := os.ReadFile(wsPath)
		if err != nil {
			t.Fatalf("workspace file not written: %v", err)
		}
		var ws struct {
			Folders []struct {
				Name string `json:"name"`
				Path string `json:"path"`
			} `json:"folders"`
			Settings map[string]any `json:"settings"`
		}
		if err := json.Unmarshal(data, &ws); err != nil {
			t.Fatalf("workspace file is not valid JSON: %v\n%s", err, data)
		}
		if len(ws.Folders) != 3 || ws.Folders[0].Path != out {
			t.Fatalf("folders = %+v; want worktree plus two vscode.workspace_paths entries", ws.Folders)
		}
		if ws.Folders[1].Name != "docs" || ws.Folders[1].Path != filepath.Join(home, "notes") {
			t.Errorf("folders[1] = %+v; want name docs and ~ expanded to %s/notes", ws.Folders[1], home)
		}
		if ws.Folders[2].Name != "" || ws.Folders[2].Path != "/shared/lib" {
			t.Errorf("folders[2] = %+v; want nameless /shared/lib passed through", ws.Folders[2])
		}
		if got, _ := ws.Settings["window.title"].(string); got != "myapp — ${activeEditorShort}" {
			t.Errorf("window.title = %q; want the configured value verbatim", got)
		}
		if _, err := os.Stat(filepath.Join(out, "acs-ws-test.code-workspace")); !os.IsNotExist(err) {
			t.Error("workspace file must not be inside the worktree")
		}

		if _, stderr, err := th(t, home, cfgVS, repo, "remove", "ws-test"); err != nil {
			t.Fatalf("%v\n%s", err, stderr)
		}
		if _, err := os.Stat(wsPath); !os.IsNotExist(err) {
			t.Error("sibling workspace file should be cleaned up with the worktree")
		}
	})

	t.Run("vscode workspace title defaults to repo name", func(t *testing.T) {
		cfgVS := filepath.Join(work, "th-vscode-default.json")
		cfgJSON := `{
  "worktree_dir": "` + trees + `/{repo}/{branch}",
  "repos": [{"path": "` + repo + `", "vscode": {"workspace_file": true}}]
}`
		if err := os.WriteFile(cfgVS, []byte(cfgJSON), 0o644); err != nil {
			t.Fatal(err)
		}
		out, _, err := th(t, home, cfgVS, repo, "add", "ws-default")
		if err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(filepath.Join(filepath.Dir(out), "ws-default.code-workspace"))
		if err != nil {
			t.Fatalf("workspace file not written: %v", err)
		}
		var ws struct {
			Settings map[string]any `json:"settings"`
		}
		if err := json.Unmarshal(data, &ws); err != nil {
			t.Fatal(err)
		}
		if got, _ := ws.Settings["window.title"].(string); got != "myapp" {
			t.Errorf("window.title = %q; want repo name \"myapp\"", got)
		}
		if _, ok := ws.Settings["workbench.colorCustomizations"]; ok {
			t.Error("workbench.colorCustomizations must be absent when vscode.window_color is unset")
		}
	})

	// readColorCustomizations reads the workbench.colorCustomizations map
	// out of the workspace file that sits next to the worktree at out.
	readColorCustomizations := func(t *testing.T, out string) map[string]any {
		t.Helper()
		wsPath := filepath.Join(filepath.Dir(out), filepath.Base(out)+".code-workspace")
		data, err := os.ReadFile(wsPath)
		if err != nil {
			t.Fatalf("workspace file not written: %v", err)
		}
		var ws struct {
			Settings map[string]any `json:"settings"`
		}
		if err := json.Unmarshal(data, &ws); err != nil {
			t.Fatalf("workspace file is not valid JSON: %v\n%s", err, data)
		}
		cc, ok := ws.Settings["workbench.colorCustomizations"].(map[string]any)
		if !ok {
			t.Fatalf("workbench.colorCustomizations missing or not an object:\n%s", data)
		}
		return cc
	}

	t.Run("vscode window color fixed hex", func(t *testing.T) {
		cfgColor := filepath.Join(work, "th-color-hex.json")
		cfgJSON := `{
  "worktree_dir": "` + trees + `/{repo}/{branch}",
  "repos": [{"path": "` + repo + `", "vscode": {"workspace_file": true, "window_color": "#AABBCC"}}]
}`
		if err := os.WriteFile(cfgColor, []byte(cfgJSON), 0o644); err != nil {
			t.Fatal(err)
		}
		out, stderr, err := th(t, home, cfgColor, repo, "add", "color-hex")
		if err != nil {
			t.Fatalf("%v\n%s", err, stderr)
		}
		cc := readColorCustomizations(t, out)
		for key, want := range map[string]string{
			"titleBar.activeBackground":   "#aabbcc", // normalized to lowercase
			"titleBar.activeForeground":   "#000000", // light color -> black text
			"titleBar.inactiveBackground": "#aabbcc99",
			"statusBar.background":        "#aabbcc",
		} {
			if got, _ := cc[key].(string); got != want {
				t.Errorf("%s = %q; want %q", key, cc[key], want)
			}
		}
	})

	t.Run("vscode window color auto differs per worktree", func(t *testing.T) {
		cfgColor := filepath.Join(work, "th-color-auto.json")
		cfgJSON := `{
  "worktree_dir": "` + trees + `/{repo}/{branch}",
  "repos": [{"path": "` + repo + `", "vscode": {"workspace_file": true, "window_color": "auto"}}]
}`
		if err := os.WriteFile(cfgColor, []byte(cfgJSON), 0o644); err != nil {
			t.Fatal(err)
		}
		hexRe := regexp.MustCompile(`^#[0-9a-f]{6}$`)
		var colors []string
		for _, branch := range []string{"color-a", "color-b"} {
			out, stderr, err := th(t, home, cfgColor, repo, "add", branch)
			if err != nil {
				t.Fatalf("%v\n%s", err, stderr)
			}
			bg, _ := readColorCustomizations(t, out)["titleBar.activeBackground"].(string)
			if !hexRe.MatchString(bg) {
				t.Errorf("%s: titleBar.activeBackground = %q; want #rrggbb", branch, bg)
			}
			colors = append(colors, bg)
		}
		if colors[0] == colors[1] {
			t.Errorf("auto gave both worktrees the same color %q; want distinct colors", colors[0])
		}
	})

	t.Run("invalid vscode window color fails th add", func(t *testing.T) {
		cfgColor := filepath.Join(work, "th-color-bad.json")
		cfgJSON := `{"vscode": {"workspace_file": true, "window_color": "red"}}`
		if err := os.WriteFile(cfgColor, []byte(cfgJSON), 0o644); err != nil {
			t.Fatal(err)
		}
		_, stderr, err := th(t, home, cfgColor, repo, "add", "color-bad")
		if err == nil {
			t.Fatal("expected error for an invalid vscode.window_color")
		}
		if !strings.Contains(stderr, "vscode.window_color") {
			t.Errorf("stderr = %q; want mention of vscode.window_color", stderr)
		}
	})

	t.Run("vscode window color without workspace file hints", func(t *testing.T) {
		cfgColor := filepath.Join(work, "th-color-hint.json")
		cfgJSON := `{
  "worktree_dir": "` + trees + `/{repo}/{branch}",
  "repos": [{"path": "` + repo + `", "vscode": {"window_color": "auto"}}]
}`
		if err := os.WriteFile(cfgColor, []byte(cfgJSON), 0o644); err != nil {
			t.Fatal(err)
		}
		out, stderr, err := th(t, home, cfgColor, repo, "add", "color-hint")
		if err != nil {
			t.Fatalf("%v\n%s", err, stderr)
		}
		if !strings.Contains(stderr, "vscode.window_color has no effect") {
			t.Errorf("stderr = %q; want the no-effect hint", stderr)
		}
		wsPath := filepath.Join(filepath.Dir(out), "color-hint.code-workspace")
		if _, err := os.Stat(wsPath); !os.IsNotExist(err) {
			t.Error("no workspace file should be written when vscode.workspace_file is off")
		}
	})

	t.Run("open requires vscode.open", func(t *testing.T) {
		_, stderr, err := th(t, home, cfg, repo, "open", "main")
		if err == nil {
			t.Fatal("expected error when vscode.open is not enabled")
		}
		if !strings.Contains(stderr, "vscode.open") {
			t.Errorf("stderr = %q; want note about enabling vscode.open", stderr)
		}
	})

	t.Run("open launches code on the right target", func(t *testing.T) {
		stub := t.TempDir()
		codeLog := filepath.Join(stub, "code.log")
		script := "#!/bin/sh\necho \"$@\" > " + codeLog + "\n"
		if err := os.WriteFile(filepath.Join(stub, "code"), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
		stubPath := []string{"PATH=" + stub + string(os.PathListSeparator) + os.Getenv("PATH")}

		cfgOpen := filepath.Join(work, "th-open.json")
		cfgJSON := `{
  "worktree_dir": "` + trees + `/{repo}/{branch}",
  "repos": [{"path": "` + repo + `", "vscode": {"open": true, "workspace_file": true, "workspace_prefix": "acs-"}}]
}`
		if err := os.WriteFile(cfgOpen, []byte(cfgJSON), 0o644); err != nil {
			t.Fatal(err)
		}

		// git reports symlink-resolved paths (/private/var vs /var on
		// macOS), so compare resolved forms.
		resolve := func(p string) string {
			if r, err := filepath.EvalSymlinks(p); err == nil {
				return r
			}
			return p
		}

		// A worktree without a workspace file opens as a folder.
		_, stderr, err := thWithEnv(t, home, cfgOpen, repo, stubPath, "open", "feature/login")
		if err != nil {
			t.Fatalf("%v\n%s", err, stderr)
		}
		logged, err := os.ReadFile(codeLog)
		if err != nil {
			t.Fatalf("stub code was not invoked: %v", err)
		}
		wantFolder := filepath.Join(trees, "myapp", "feature-login")
		if got := strings.TrimSpace(string(logged)); resolve(got) != resolve(wantFolder) {
			t.Errorf("code opened %q; want the folder %q", got, wantFolder)
		}

		// A worktree with a th-generated workspace file opens that file.
		out, _, err := thWithEnv(t, home, cfgOpen, repo, stubPath, "add", "--no-open", "open-ws")
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := thWithEnv(t, home, cfgOpen, repo, stubPath, "open", "open-ws"); err != nil {
			t.Fatal(err)
		}
		logged, err = os.ReadFile(codeLog)
		if err != nil {
			t.Fatal(err)
		}
		wantWS := filepath.Join(filepath.Dir(out), "acs-open-ws.code-workspace")
		if got := strings.TrimSpace(string(logged)); resolve(got) != resolve(wantWS) {
			t.Errorf("code opened %q; want the workspace file %q", got, wantWS)
		}
	})

	t.Run("full paths flag and config", func(t *testing.T) {
		inHome := filepath.Join(home, "trees", "homed")
		out, _, err := th(t, home, cfg, repo, "add", "--path", inHome, "homed")
		if err != nil {
			t.Fatal(err)
		}
		if out != inHome {
			t.Errorf("th add stdout = %q; want absolute %q", out, inHome)
		}

		l, _, err := th(t, home, cfg, repo, "du")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(l, "~/trees/homed") {
			t.Errorf("du should abbreviate the home directory:\n%s", l)
		}

		lf, _, err := th(t, home, cfg, repo, "du", "--full-paths")
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(lf, "~/trees/homed") {
			t.Errorf("du --full-paths should not abbreviate:\n%s", lf)
		}

		cfgFull := filepath.Join(work, "th-fullpaths.json")
		cfgJSON := `{
  "worktree_dir": "` + trees + `/{repo}/{branch}",
  "repos": [{"path": "` + repo + `", "full_paths": true}]
}`
		if err := os.WriteFile(cfgFull, []byte(cfgJSON), 0o644); err != nil {
			t.Fatal(err)
		}
		lc, _, err := th(t, home, cfgFull, repo, "du")
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(lc, "~/trees/homed") {
			t.Errorf("du with full_paths config should not abbreviate:\n%s", lc)
		}

		if _, _, err := th(t, home, cfg, repo, "remove", "homed"); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("completion", func(t *testing.T) {
		out, _, err := th(t, home, cfg, repo, "completion", "zsh")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, "#compdef") {
			t.Errorf("zsh completion script missing #compdef header:\n%.120s", out)
		}
		_, stderr, err := th(t, home, cfg, repo, "completion")
		if err == nil {
			t.Error("wizard without a terminal should fail")
		} else if !strings.Contains(stderr, "terminal") {
			t.Errorf("stderr = %q; want terminal hint", stderr)
		}
		if _, _, err := th(t, home, cfg, repo, "completion", "tcsh"); err == nil {
			t.Error("unsupported shell should fail")
		}
	})

	t.Run("post_create runs commands in the new worktree", func(t *testing.T) {
		cfgPC := filepath.Join(work, "th-postcreate.json")
		cfgJSON := `{
  "worktree_dir": "` + trees + `/{repo}/{branch}",
  "repos": [{"path": "` + repo + `", "post_create": [
    "echo ran > marker.txt",
    "printf %s \"$TH_BRANCH\" > branch.txt",
    "printf %s \"$TH_REPO\" > repo.txt"
  ]}]
}`
		if err := os.WriteFile(cfgPC, []byte(cfgJSON), 0o644); err != nil {
			t.Fatal(err)
		}
		out, stderr, err := th(t, home, cfgPC, repo, "add", "pc-test")
		if err != nil {
			t.Fatalf("%v\n%s", err, stderr)
		}
		if _, err := os.Stat(filepath.Join(out, "marker.txt")); err != nil {
			t.Errorf("post_create did not run in the worktree: %v", err)
		}
		if b, _ := os.ReadFile(filepath.Join(out, "branch.txt")); string(b) != "pc-test" {
			t.Errorf("TH_BRANCH = %q; want pc-test", b)
		}
		if b, _ := os.ReadFile(filepath.Join(out, "repo.txt")); string(b) != "myapp" {
			t.Errorf("TH_REPO = %q; want myapp", b)
		}

		out2, _, err := th(t, home, cfgPC, repo, "add", "--no-post-create", "pc-skip")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(filepath.Join(out2, "marker.txt")); !os.IsNotExist(err) {
			t.Error("--no-post-create should have skipped the commands")
		}
	})

	t.Run("post_create is safe against shell metacharacters in branch names", func(t *testing.T) {
		cfgPC := filepath.Join(work, "th-postcreate-inj.json")
		cfgJSON := `{
  "worktree_dir": "` + trees + `/{repo}/{branch}",
  "repos": [{"path": "` + repo + `", "post_create": ["printf %s \"$TH_BRANCH\" > branch.txt"]}]
}`
		if err := os.WriteFile(cfgPC, []byte(cfgJSON), 0o644); err != nil {
			t.Fatal(err)
		}
		// A legal git ref name (no spaces allowed) that creates BOOM via
		// redirection if it is ever shell-interpolated.
		evil := "inj-$(>BOOM)"
		out, stderr, err := th(t, home, cfgPC, repo, "add", evil)
		if err != nil {
			t.Fatalf("%v\n%s", err, stderr)
		}
		if _, err := os.Stat(filepath.Join(out, "BOOM")); !os.IsNotExist(err) {
			t.Error("branch name was shell-interpolated: BOOM file exists")
		}
		if b, _ := os.ReadFile(filepath.Join(out, "branch.txt")); string(b) != evil {
			t.Errorf("TH_BRANCH = %q; want the literal branch name %q", b, evil)
		}
	})

	t.Run("post_create failure reports the worktree survived", func(t *testing.T) {
		cfgPC := filepath.Join(work, "th-postcreate-fail.json")
		cfgJSON := `{
  "worktree_dir": "` + trees + `/{repo}/{branch}",
  "repos": [{"path": "` + repo + `", "post_create": ["exit 7"]}]
}`
		if err := os.WriteFile(cfgPC, []byte(cfgJSON), 0o644); err != nil {
			t.Fatal(err)
		}
		_, stderr, err := th(t, home, cfgPC, repo, "add", "pc-fail")
		if err == nil {
			t.Fatal("expected error when a post_create command fails")
		}
		if !strings.Contains(stderr, "worktree created") || !strings.Contains(stderr, "post_create") {
			t.Errorf("stderr = %q; want worktree-created and post_create mention", stderr)
		}
		if _, err := os.Stat(filepath.Join(trees, "myapp", "pc-fail")); err != nil {
			t.Errorf("worktree should survive a failed post_create: %v", err)
		}
	})

	// The repo-local config lives in the main worktree and is deleted again
	// by each subtest, so it cannot leak into the ones that follow.
	localCfg := filepath.Join(repo, ".thrc")

	t.Run(".thrc overrides global repos entry", func(t *testing.T) {
		cfgLocal := filepath.Join(work, "th-local.json")
		cfgJSON := `{
  "worktree_dir": "` + trees + `/{repo}/{branch}",
  "repos": [{"name": "myapp", "path": "` + repo + `", "worktree_dir": "` + trees + `/trees-A/{branch}", "branch_prefix": "team"}]
}`
		if err := os.WriteFile(cfgLocal, []byte(cfgJSON), 0o644); err != nil {
			t.Fatal(err)
		}
		localJSON := `{
  "name": "localname",
  "worktree_dir": "` + trees + `/local-{repo}/{branch}",
  "branch_prefix": "local"
}`
		if err := os.WriteFile(localCfg, []byte(localJSON), 0o644); err != nil {
			t.Fatal(err)
		}
		defer os.Remove(localCfg)

		out, stderr, err := th(t, home, cfgLocal, repo, "add", "wtj-test")
		if err != nil {
			t.Fatalf("%v\n%s", err, stderr)
		}
		want := filepath.Join(trees, "local-localname", "local-wtj-test")
		if out != want {
			t.Errorf("stdout = %q; want %q from .thrc", out, want)
		}
		if got := git(t, home, out, "rev-parse", "--abbrev-ref", "HEAD"); got != "local/wtj-test" {
			t.Errorf("checked-out branch = %q; want local/wtj-test", got)
		}
		if _, stderr, err := th(t, home, cfgLocal, repo, "remove", "local/wtj-test"); err != nil {
			t.Fatalf("%v\n%s", err, stderr)
		}
	})

	t.Run("repo post_create requires approval", func(t *testing.T) {
		trustFile := filepath.Join(home, ".th", "trust.json")
		defer func() {
			os.Remove(localCfg)
			os.Remove(trustFile)
		}()
		writeLocal := func(command string) {
			t.Helper()
			if err := os.WriteFile(localCfg, []byte(`{"post_create": ["`+command+`"]}`), 0o644); err != nil {
				t.Fatal(err)
			}
		}

		const approvedCmd = "echo ran > pc-marker.txt"
		writeLocal(approvedCmd)

		// Unapproved: the worktree is created, the commands are not run.
		out, stderr, err := th(t, home, cfg, repo, "add", "pc-unapproved")
		if err != nil {
			t.Fatalf("%v\n%s", err, stderr)
		}
		if !strings.Contains(stderr, "not approved") {
			t.Errorf("stderr = %q; want a not-approved warning", stderr)
		}
		if _, err := os.Stat(filepath.Join(out, "pc-marker.txt")); !os.IsNotExist(err) {
			t.Error("unapproved repo post_create ran")
		}
		if _, err := os.Stat(out); err != nil {
			t.Errorf("worktree should still be created: %v", err)
		}

		// Approving the exact command list makes it run without prompting.
		mainPath, err := filepath.EvalSymlinks(repo)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Dir(trustFile), 0o700); err != nil {
			t.Fatal(err)
		}
		trustJSON := `{"repos":{"` + mainPath + `":{"post_create":["` + approvedCmd + `"],"approved_at":"2026-01-01T00:00:00Z"}}}`
		if err := os.WriteFile(trustFile, []byte(trustJSON), 0o600); err != nil {
			t.Fatal(err)
		}
		out, stderr, err = th(t, home, cfg, repo, "add", "pc-trusted")
		if err != nil {
			t.Fatalf("%v\n%s", err, stderr)
		}
		if _, err := os.Stat(filepath.Join(out, "pc-marker.txt")); err != nil {
			t.Errorf("approved repo post_create did not run: %v", err)
		}

		// Changing the commands invalidates the approval.
		writeLocal("echo changed > pc-marker.txt")
		out, stderr, err = th(t, home, cfg, repo, "add", "pc-changed")
		if err != nil {
			t.Fatalf("%v\n%s", err, stderr)
		}
		if !strings.Contains(stderr, "not approved") {
			t.Errorf("stderr = %q; want a not-approved warning after the commands changed", stderr)
		}
		if _, err := os.Stat(filepath.Join(out, "pc-marker.txt")); !os.IsNotExist(err) {
			t.Error("changed repo post_create ran without re-approval")
		}
	})

	t.Run(".thrc with repos key fails add but not list", func(t *testing.T) {
		if err := os.WriteFile(localCfg, []byte(`{"repos": []}`), 0o644); err != nil {
			t.Fatal(err)
		}
		defer os.Remove(localCfg)

		_, stderr, err := th(t, home, cfg, repo, "add", "wtj-invalid")
		if err == nil {
			t.Fatal("expected error for a .thrc with a repos key")
		}
		if !strings.Contains(stderr, ".thrc") {
			t.Errorf("stderr = %q; want the repo-local file named", stderr)
		}
		if _, stderr, err := th(t, home, cfg, repo, "list"); err != nil {
			t.Errorf("list should survive a broken .thrc: %v\n%s", err, stderr)
		}
	})

	t.Run("th config prints repo-local file", func(t *testing.T) {
		localJSON := `{"branch_prefix": "local"}`
		if err := os.WriteFile(localCfg, []byte(localJSON+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		defer os.Remove(localCfg)

		// git reports symlink-resolved paths, and so does the header.
		resolved, err := filepath.EvalSymlinks(localCfg)
		if err != nil {
			t.Fatal(err)
		}
		out, stderr, err := th(t, home, cfg, repo, "config")
		if err != nil {
			t.Fatalf("%v\n%s", err, stderr)
		}
		if !strings.Contains(stderr, resolved) || !strings.Contains(stderr, "repo-local") {
			t.Errorf("stderr = %q; want the .thrc path marked repo-local", stderr)
		}
		if !strings.Contains(out, localJSON) {
			t.Errorf("stdout = %q; want the repo-local content %q", out, localJSON)
		}
	})

	t.Run("config --effective shows merged settings with sources", func(t *testing.T) {
		cfgEff := filepath.Join(work, "th-effective.json")
		cfgEffJSON := `{
  "default_base": "main",
  "branch_prefix": "global",
  "repos": [{"name": "entryname", "path": "` + repo + `", "branch_prefix": "team"}]
}`
		if err := os.WriteFile(cfgEff, []byte(cfgEffJSON), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(localCfg, []byte(`{"name": "localname", "copy_files": [".env"]}`), 0o644); err != nil {
			t.Fatal(err)
		}
		defer os.Remove(localCfg)

		out, stderr, err := th(t, home, cfgEff, repo, "config", "--effective")
		if err != nil {
			t.Fatalf("%v\n%s", err, stderr)
		}
		for _, want := range []string{cfgEff, "repos[0] matches", "repo-local"} {
			if !strings.Contains(stderr, want) {
				t.Errorf("stderr = %q; want it to mention %q", stderr, want)
			}
		}
		for _, row := range [][3]string{
			{"name", "localname", ".thrc"},
			{"worktree_dir", "~/worktrees/{repo}/{branch}", "default"},
			{"default_base", "main", "top-level"},
			{"branch_prefix", "team", "repos[0]"},
			{"copy_files", `[".env"]`, ".thrc"},
			{"copy_hooks", "false", "default"},
			{"vscode.window_color", "(unset)", "default"},
		} {
			assertEffectiveRow(t, out, row[0], row[1], row[2])
		}
	})

	t.Run("config --effective outside a repository", func(t *testing.T) {
		out, stderr, err := th(t, home, "", work, "config", "--effective")
		if err != nil {
			t.Fatalf("%v\n%s", err, stderr)
		}
		if !strings.Contains(stderr, "not inside a git repository") {
			t.Errorf("stderr = %q; want not-a-repository notice", stderr)
		}
		assertEffectiveRow(t, out, "worktree_dir", "~/worktrees/{repo}/{branch}", "default")
		if strings.Contains(out, "\nname ") {
			t.Errorf("stdout = %q; want no name row outside a repository", out)
		}
	})

	t.Run("init scaffolds a repo-local .thrc", func(t *testing.T) {
		out, stderr, err := th(t, home, cfg, repo, "init")
		if err != nil {
			t.Fatalf("%v\n%s", err, stderr)
		}
		wtjson := filepath.Join(repo, ".thrc")
		t.Cleanup(func() { os.Remove(wtjson) })
		data, err := os.ReadFile(wtjson)
		if err != nil {
			t.Fatalf(".thrc not created at the main worktree root: %v", err)
		}
		var lc struct {
			Version int    `json:"version"`
			Name    string `json:"name"`
		}
		if err := json.Unmarshal(data, &lc); err != nil || lc.Name != "myapp" {
			t.Errorf(".thrc = %s (err %v); want name myapp", data, err)
		}
		// The scaffold is stamped with the current schema, so it never
		// looks like a file that predates versioning.
		if want := config.CurrentLocalVersion(); lc.Version != want {
			t.Errorf(".thrc version = %d; want the current schema version %d", lc.Version, want)
		}
		if filepath.Base(out) != ".thrc" {
			t.Errorf("stdout = %q; want the created file path", out)
		}

		if _, stderr, err := th(t, home, cfg, repo, "init"); err == nil {
			t.Error("second init should refuse to overwrite")
		} else if !strings.Contains(stderr, "already exists") {
			t.Errorf("stderr = %q; want already-exists error", stderr)
		}

		// From a linked worktree, init still targets the main worktree root.
		os.Remove(wtjson)
		linked, _, err := th(t, home, cfg, repo, "add", "init-linked")
		if err != nil {
			t.Fatal(err)
		}
		if _, stderr, err := th(t, home, cfg, linked, "init"); err != nil {
			t.Fatalf("%v\n%s", err, stderr)
		}
		if _, err := os.Stat(wtjson); err != nil {
			t.Errorf(".thrc missing from main worktree after init in linked worktree: %v", err)
		}
		if _, err := os.Stat(filepath.Join(linked, ".thrc")); !os.IsNotExist(err) {
			t.Error(".thrc must not be created inside the linked worktree")
		}
	})

	t.Run("init flags pre-fill the scaffolded .thrc", func(t *testing.T) {
		thrc := filepath.Join(repo, ".thrc")
		t.Cleanup(func() { os.Remove(thrc) })
		// Each init refuses to overwrite, so start from a clean slate.
		scaffold := func(args ...string) []byte {
			t.Helper()
			os.Remove(thrc)
			if _, stderr, err := th(t, home, cfg, repo, append([]string{"init"}, args...)...); err != nil {
				t.Fatalf("th init %v: %v\n%s", args, err, stderr)
			}
			data, err := os.ReadFile(thrc)
			if err != nil {
				t.Fatalf("th init %v did not create the file: %v", args, err)
			}
			return data
		}

		data := scaffold("--prefix", "team", "--separator", "-", "--base", "develop",
			"--copy-file", ".env*", "--copy-file", "local-conf",
			"--post-create", "npm ci", "--copy-hooks", "--open")
		var got struct {
			Name            string   `json:"name"`
			BranchPrefix    string   `json:"branch_prefix"`
			PrefixSeparator string   `json:"prefix_separator"`
			DefaultBase     string   `json:"default_base"`
			CopyFiles       []string `json:"copy_files"`
			PostCreate      []string `json:"post_create"`
			CopyHooks       bool     `json:"copy_hooks"`
			VSCode          struct {
				Open bool `json:"open"`
			} `json:"vscode"`
		}
		if err := json.Unmarshal(data, &got); err != nil {
			t.Fatalf("unmarshal %s: %v", data, err)
		}
		if got.Name != "myapp" {
			t.Errorf("name = %q; want the directory basename myapp", got.Name)
		}
		if got.BranchPrefix != "team" || got.PrefixSeparator != "-" || got.DefaultBase != "develop" {
			t.Errorf("prefix/separator/base = %q/%q/%q; want team/-/develop", got.BranchPrefix, got.PrefixSeparator, got.DefaultBase)
		}
		if !slices.Equal(got.CopyFiles, []string{".env*", "local-conf"}) {
			t.Errorf("copy_files = %q; want [.env* local-conf]", got.CopyFiles)
		}
		if !slices.Equal(got.PostCreate, []string{"npm ci"}) {
			t.Errorf("post_create = %q; want [npm ci]", got.PostCreate)
		}
		if !got.CopyHooks || !got.VSCode.Open {
			t.Errorf("copy_hooks/vscode.open = %v/%v; want both true", got.CopyHooks, got.VSCode.Open)
		}
		// A bool flag that was not passed leaves its field out entirely.
		var raw map[string]any
		if err := json.Unmarshal(data, &raw); err != nil {
			t.Fatal(err)
		}
		vs, _ := raw["vscode"].(map[string]any)
		if _, ok := vs["workspace_file"]; ok {
			t.Errorf(".thrc = %s; want no vscode.workspace_file when --workspace-file is absent", data)
		}

		data = scaffold("--name", "custom")
		var named struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(data, &named); err != nil || named.Name != "custom" {
			t.Errorf(".thrc = %s (err %v); want name custom", data, err)
		}

		// Without flags the file is just the schema version and the name —
		// no empty "vscode" object either.
		want := fmt.Sprintf("{\n  \"version\": %d,\n  \"name\": \"myapp\"\n}\n", config.CurrentLocalVersion())
		if data := scaffold(); string(data) != want {
			t.Errorf("flagless init wrote %q; want %q", data, want)
		} else if strings.Contains(string(data), "vscode") {
			t.Errorf("flagless init wrote %q; want no vscode key at all", data)
		}
	})

	t.Run("prune cleans up stale worktrees", func(t *testing.T) {
		out, _, err := th(t, home, cfg, repo, "add", "doomed")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.RemoveAll(out); err != nil {
			t.Fatal(err)
		}

		_, stderr, err := th(t, home, cfg, repo, "prune", "--dry-run")
		if err != nil {
			t.Fatalf("%v\n%s", err, stderr)
		}
		if !strings.Contains(stderr, "doomed") || !strings.Contains(stderr, "dry run") {
			t.Errorf("dry-run stderr = %q; want the stale worktree listed", stderr)
		}
		if listOut, _, err := th(t, home, cfg, repo, "list"); err != nil || !strings.Contains(listOut, "doomed") {
			t.Errorf("dry run should not have pruned; list = %q, %v", listOut, err)
		}

		_, stderr, err = th(t, home, cfg, repo, "prune")
		if err != nil {
			t.Fatalf("%v\n%s", err, stderr)
		}
		if !strings.Contains(stderr, "Pruned 1 stale worktree entry") {
			t.Errorf("prune stderr = %q; want prune confirmation", stderr)
		}
		if listOut, _, err := th(t, home, cfg, repo, "list"); err != nil || strings.Contains(listOut, "doomed") {
			t.Errorf("stale worktree still listed after prune: %q, %v", listOut, err)
		}
		git(t, home, repo, "rev-parse", "--verify", "refs/heads/doomed")

		_, stderr, err = th(t, home, cfg, repo, "prune")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(stderr, "Nothing to prune") {
			t.Errorf("second prune stderr = %q; want nothing to prune", stderr)
		}
	})

	t.Run("repo branch prefix overrides global", func(t *testing.T) {
		cfgRepoPrefix := filepath.Join(work, "th-repo-prefix.json")
		cfgJSON := `{
  "worktree_dir": "` + trees + `/{repo}/{branch}",
  "branch_prefix": "peter/",
  "repos": [{"path": "` + repo + `", "branch_prefix": "team"}]
}`
		if err := os.WriteFile(cfgRepoPrefix, []byte(cfgJSON), 0o644); err != nil {
			t.Fatal(err)
		}
		out, _, err := th(t, home, cfgRepoPrefix, repo, "add", "gadget")
		if err != nil {
			t.Fatal(err)
		}
		want := filepath.Join(trees, "myapp", "team-gadget")
		if out != want {
			t.Errorf("stdout = %q; want %q", out, want)
		}
		if got := git(t, home, out, "rev-parse", "--abbrev-ref", "HEAD"); got != "team/gadget" {
			t.Errorf("checked-out branch = %q; want team/gadget", got)
		}
	})

	// cdFileEnv gives th a TH_CD_FILE and returns the path to inspect;
	// "not written" is asserted as the file not existing.
	cdFileEnv := func(t *testing.T) (string, []string) {
		t.Helper()
		f := filepath.Join(t.TempDir(), "cd")
		return f, []string{"TH_CD_FILE=" + f}
	}
	notWritten := func(t *testing.T, f, when string) {
		t.Helper()
		if _, err := os.Stat(f); !os.IsNotExist(err) {
			t.Errorf("TH_CD_FILE written %s", when)
		}
	}

	t.Run("shell integration cd file on add", func(t *testing.T) {
		f, env := cdFileEnv(t)
		out, stderr, err := thWithEnv(t, home, cfg, repo, env, "add", "cd-a")
		if err != nil {
			t.Fatalf("%v\n%s", err, stderr)
		}
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("TH_CD_FILE not written: %v", err)
		}
		if string(data) != out {
			t.Errorf("TH_CD_FILE = %q; want the stdout path %q exactly", data, out)
		}

		f, env = cdFileEnv(t)
		if _, stderr, err := thWithEnv(t, home, cfg, repo, env, "add", "--no-cd", "cd-d"); err != nil {
			t.Fatalf("%v\n%s", err, stderr)
		}
		notWritten(t, f, "despite --no-cd")

		// Without the env var nothing changes: stdout stays the path.
		out, _, err = th(t, home, cfg, repo, "add", "cd-g")
		if err != nil {
			t.Fatal(err)
		}
		if want := filepath.Join(trees, "myapp", "cd-g"); out != want {
			t.Errorf("stdout = %q; want %q", out, want)
		}
	})

	t.Run("auto_cd config layers", func(t *testing.T) {
		cfgNoCD := filepath.Join(work, "th-nocd.json")
		cfgJSON := `{
  "worktree_dir": "` + trees + `/{repo}/{branch}",
  "repos": [{"path": "` + repo + `", "auto_cd": false}]
}`
		if err := os.WriteFile(cfgNoCD, []byte(cfgJSON), 0o644); err != nil {
			t.Fatal(err)
		}

		f, env := cdFileEnv(t)
		if _, stderr, err := thWithEnv(t, home, cfgNoCD, repo, env, "add", "cd-e"); err != nil {
			t.Fatalf("%v\n%s", err, stderr)
		}
		notWritten(t, f, "despite auto_cd: false")

		f, env = cdFileEnv(t)
		if _, stderr, err := thWithEnv(t, home, cfgNoCD, repo, env, "add", "--cd", "cd-f"); err != nil {
			t.Fatalf("%v\n%s", err, stderr)
		}
		if _, err := os.ReadFile(f); err != nil {
			t.Errorf("--cd should override auto_cd: false: %v", err)
		}
	})

	t.Run("vscode open wins over cd", func(t *testing.T) {
		stub := t.TempDir()
		codeLog := filepath.Join(stub, "code.log")
		script := "#!/bin/sh\necho \"$@\" > " + codeLog + "\n"
		if err := os.WriteFile(filepath.Join(stub, "code"), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
		pathEnv := "PATH=" + stub + string(os.PathListSeparator) + os.Getenv("PATH")

		cfgOpenCD := filepath.Join(work, "th-open-cd.json")
		cfgJSON := `{
  "worktree_dir": "` + trees + `/{repo}/{branch}",
  "repos": [{"path": "` + repo + `", "vscode": {"open": true}}]
}`
		if err := os.WriteFile(cfgOpenCD, []byte(cfgJSON), 0o644); err != nil {
			t.Fatal(err)
		}

		f, env := cdFileEnv(t)
		if _, stderr, err := thWithEnv(t, home, cfgOpenCD, repo, append(env, pathEnv), "add", "cd-b"); err != nil {
			t.Fatalf("%v\n%s", err, stderr)
		}
		if _, err := os.Stat(codeLog); err != nil {
			t.Fatalf("stub code was not invoked: %v", err)
		}
		notWritten(t, f, "while VS Code opened")

		f, env = cdFileEnv(t)
		if _, stderr, err := thWithEnv(t, home, cfgOpenCD, repo, append(env, pathEnv), "add", "--cd", "cd-k"); err != nil {
			t.Fatalf("%v\n%s", err, stderr)
		}
		notWritten(t, f, "while VS Code opened, even with an explicit --cd")

		f, env = cdFileEnv(t)
		if _, stderr, err := thWithEnv(t, home, cfgOpenCD, repo, append(env, pathEnv), "add", "--no-open", "cd-c"); err != nil {
			t.Fatalf("%v\n%s", err, stderr)
		}
		if _, err := os.ReadFile(f); err != nil {
			t.Errorf("--no-open should let auto-cd apply: %v", err)
		}
	})

	t.Run("th cd resolves worktrees", func(t *testing.T) {
		resolve := func(p string) string {
			if r, err := filepath.EvalSymlinks(p); err == nil {
				return r
			}
			return p
		}
		cfgNoCD := filepath.Join(work, "th-nocd.json") // written above; auto_cd: false
		f, env := cdFileEnv(t)
		out, stderr, err := thWithEnv(t, home, cfgNoCD, repo, env, "cd", "feature/login")
		if err != nil {
			t.Fatalf("%v\n%s", err, stderr)
		}
		// git reports symlink-resolved paths (/private/var vs /var on macOS).
		if want := filepath.Join(trees, "myapp", "feature-login"); resolve(out) != resolve(want) {
			t.Errorf("stdout = %q; want %q", out, want)
		}
		// th cd is explicit navigation: it writes even with auto_cd: false.
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("th cd did not write TH_CD_FILE: %v", err)
		}
		if string(data) != out {
			t.Errorf("TH_CD_FILE = %q; want %q", data, out)
		}

		if _, stderr, err := th(t, home, cfg, repo, "cd"); err == nil {
			t.Error("th cd with no argument should fail without a terminal")
		} else if !strings.Contains(stderr, "interactive selection needs a terminal") {
			t.Errorf("stderr = %q; want the non-TTY picker error", stderr)
		}

		if _, stderr, err := th(t, home, cfg, repo, "cd", "no-such-branch"); err == nil {
			t.Error("th cd with an unknown branch should fail")
		} else if !strings.Contains(stderr, "no worktree found") {
			t.Errorf("stderr = %q; want no-worktree-found", stderr)
		}
	})

	t.Run("shell-init output", func(t *testing.T) {
		if _, stderr, err := th(t, home, cfg, repo, "shell-init", "powershell"); err == nil {
			t.Error("shell-init powershell should fail")
		} else if !strings.Contains(stderr, "invalid argument") {
			t.Errorf("stderr = %q; want invalid-argument", stderr)
		}

		out, _, err := thWithEnv(t, home, cfg, repo, []string{"SHELL=/bin/bash"}, "shell-init")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, "th() {") {
			t.Errorf("detected-shell output missing the POSIX wrapper:\n%s", out)
		}
	})

	t.Run("shell wrapper cds into the new worktree", func(t *testing.T) {
		bashBin, err := exec.LookPath("bash")
		if err != nil {
			t.Skip("bash not on PATH")
		}
		resolve := func(p string) string {
			if r, err := filepath.EvalSymlinks(p); err == nil {
				return r
			}
			return p
		}
		env := append(append(gitEnv(home), "TH_CONFIG="+cfg),
			"PATH="+filepath.Dir(thBin)+string(os.PathListSeparator)+os.Getenv("PATH"))
		runBash := func(script string) (string, string, error) {
			cmd := exec.Command(bashBin, "-c", script)
			cmd.Dir = repo
			cmd.Env = env
			var so, se strings.Builder
			cmd.Stdout, cmd.Stderr = &so, &se
			err := cmd.Run()
			return strings.TrimSpace(so.String()), se.String(), err
		}

		out, stderr, err := runBash(`eval "$(th shell-init bash)"; th add wrap-test >/dev/null; pwd`)
		if err != nil {
			t.Fatalf("%v\n%s", err, stderr)
		}
		want := filepath.Join(trees, "myapp", "wrap-test")
		if resolve(out) != resolve(want) {
			t.Errorf("wrapper pwd = %q; want %q", out, want)
		}

		// A failing th writes no cd file: the status propagates and the
		// terminal stays put ("main" is already checked out).
		out, stderr, err = runBash(`eval "$(th shell-init bash)"; th add main >/dev/null 2>&1; echo "rc=$?"; pwd`)
		if err != nil {
			t.Fatalf("%v\n%s", err, stderr)
		}
		lines := strings.Split(out, "\n")
		if len(lines) != 2 || lines[0] != "rc=1" {
			t.Errorf("output = %q; want rc=1 then pwd", out)
		}
		if len(lines) == 2 && resolve(lines[1]) != resolve(repo) {
			t.Errorf("pwd after failed add = %q; want the repo %q", lines[1], repo)
		}
	})

	// link_files / tracked-file guard fixtures: an untracked directory to
	// link, a committed-then-dirtied file, and a mixed directory.
	lfResolve := func(p string) string {
		if r, err := filepath.EvalSymlinks(p); err == nil {
			return r
		}
		return p
	}
	if err := os.MkdirAll(filepath.Join(repo, "shared-cache"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "shared-cache", "data.bin"), []byte("cache\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(repo, "confdir"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "confdir", "a.txt"), []byte("committed-a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, home, repo, "add", "confdir/a.txt")
	git(t, home, repo, "commit", "-m", "add confdir/a.txt")
	// Dirty the tracked file in the main worktree: the guard must keep the
	// checkout's committed content, not copy this edit.
	if err := os.WriteFile(filepath.Join(repo, "confdir", "a.txt"), []byte("dirty-a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "confdir", "b.local"), []byte("local-b\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfgLink := filepath.Join(work, "th-link.json")
	cfgLinkJSON := `{
  "worktree_dir": "` + trees + `/{repo}/{branch}",
  "repos": [{"path": "` + repo + `", "link_files": ["shared-cache"]}]
}`
	if err := os.WriteFile(cfgLink, []byte(cfgLinkJSON), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Run("link_files links a directory", func(t *testing.T) {
		out, stderr, err := th(t, home, cfgLink, repo, "add", "lf-link")
		if err != nil {
			t.Fatalf("%v\n%s", err, stderr)
		}
		link := filepath.Join(out, "shared-cache")
		fi, err := os.Lstat(link)
		if err != nil {
			t.Fatalf("symlink not created: %v", err)
		}
		if fi.Mode()&os.ModeSymlink == 0 {
			t.Fatalf("shared-cache is not a symlink (mode %v)", fi.Mode())
		}
		dest, err := os.Readlink(link)
		if err != nil {
			t.Fatal(err)
		}
		if lfResolve(dest) != lfResolve(filepath.Join(repo, "shared-cache")) {
			t.Errorf("link target = %q; want the main worktree's shared-cache", dest)
		}
		// Writing through the link lands in the main worktree — shared on
		// purpose.
		if err := os.WriteFile(filepath.Join(link, "new.txt"), []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(filepath.Join(repo, "shared-cache", "new.txt")); err != nil {
			t.Errorf("write through the link did not reach the main worktree: %v", err)
		}
		for _, want := range []string{"Linked shared-cache", "Linked 1 into"} {
			if !strings.Contains(stderr, want) {
				t.Errorf("stderr missing %q:\n%s", want, stderr)
			}
		}
	})

	t.Run("copy_files skips tracked files", func(t *testing.T) {
		cfgGuard := filepath.Join(work, "th-guard.json")
		cfgJSON := `{
  "worktree_dir": "` + trees + `/{repo}/{branch}",
  "repos": [{"path": "` + repo + `", "copy_files": ["confdir/a.txt", "confdir/b.local"]}]
}`
		if err := os.WriteFile(cfgGuard, []byte(cfgJSON), 0o644); err != nil {
			t.Fatal(err)
		}
		out, stderr, err := th(t, home, cfgGuard, repo, "add", "lf-guard")
		if err != nil {
			t.Fatalf("%v\n%s", err, stderr)
		}
		data, err := os.ReadFile(filepath.Join(out, "confdir", "a.txt"))
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != "committed-a\n" {
			t.Errorf("tracked a.txt = %q; want the checkout's committed content, not the dirty copy", data)
		}
		if _, err := os.Stat(filepath.Join(out, "confdir", "b.local")); err != nil {
			t.Errorf("untracked b.local not copied: %v", err)
		}
		if !strings.Contains(stderr, `skipping tracked "confdir/a.txt"`) {
			t.Errorf("stderr missing the tracked-skip note:\n%s", stderr)
		}
	})

	t.Run("copy_files directory skips tracked members", func(t *testing.T) {
		cfgDir := filepath.Join(work, "th-guard-dir.json")
		cfgJSON := `{
  "worktree_dir": "` + trees + `/{repo}/{branch}",
  "repos": [{"path": "` + repo + `", "copy_files": ["confdir"]}]
}`
		if err := os.WriteFile(cfgDir, []byte(cfgJSON), 0o644); err != nil {
			t.Fatal(err)
		}
		out, stderr, err := th(t, home, cfgDir, repo, "add", "lf-mixed")
		if err != nil {
			t.Fatalf("%v\n%s", err, stderr)
		}
		data, err := os.ReadFile(filepath.Join(out, "confdir", "a.txt"))
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != "committed-a\n" {
			t.Errorf("tracked a.txt = %q; want the checkout's content", data)
		}
		if _, err := os.Stat(filepath.Join(out, "confdir", "b.local")); err != nil {
			t.Errorf("untracked b.local not copied: %v", err)
		}
		if !strings.Contains(stderr, `skipped 1 tracked file(s) under "confdir"`) {
			t.Errorf("stderr missing the counted skip note:\n%s", stderr)
		}
	})

	t.Run("link_files refuses tracked targets", func(t *testing.T) {
		cfgBadLink := filepath.Join(work, "th-badlink.json")
		cfgJSON := `{
  "worktree_dir": "` + trees + `/{repo}/{branch}",
  "repos": [{"path": "` + repo + `", "link_files": ["confdir"]}]
}`
		if err := os.WriteFile(cfgBadLink, []byte(cfgJSON), 0o644); err != nil {
			t.Fatal(err)
		}
		out, stderr, err := th(t, home, cfgBadLink, repo, "add", "lf-badlink")
		if err != nil {
			t.Fatalf("%v\n%s", err, stderr)
		}
		fi, err := os.Lstat(filepath.Join(out, "confdir"))
		if err != nil {
			t.Fatal(err)
		}
		if fi.Mode()&os.ModeSymlink != 0 {
			t.Error("confdir must stay a real directory, not a link over tracked files")
		}
		if !strings.Contains(stderr, "would shadow the checkout") {
			t.Errorf("stderr missing the shadow note:\n%s", stderr)
		}
	})

	t.Run("link-file flag and no-link-files", func(t *testing.T) {
		out, stderr, err := th(t, home, cfg, repo, "add", "--link-file", "shared-cache", "lf-flag")
		if err != nil {
			t.Fatalf("%v\n%s", err, stderr)
		}
		if fi, err := os.Lstat(filepath.Join(out, "shared-cache")); err != nil || fi.Mode()&os.ModeSymlink == 0 {
			t.Errorf("--link-file did not create a symlink: %v", err)
		}

		out2, stderr, err := th(t, home, cfgLink, repo, "add", "--no-link-files", "lf-noflag")
		if err != nil {
			t.Fatalf("%v\n%s", err, stderr)
		}
		if _, err := os.Lstat(filepath.Join(out2, "shared-cache")); !os.IsNotExist(err) {
			t.Error("--no-link-files should skip the config's link_files list")
		}
	})

	t.Run("copy wins when both lists match", func(t *testing.T) {
		if err := os.WriteFile(filepath.Join(repo, "dup.txt"), []byte("dup\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		cfgDup := filepath.Join(work, "th-dup.json")
		cfgJSON := `{
  "worktree_dir": "` + trees + `/{repo}/{branch}",
  "repos": [{"path": "` + repo + `", "copy_files": ["dup.txt"], "link_files": ["dup.txt"]}]
}`
		if err := os.WriteFile(cfgDup, []byte(cfgJSON), 0o644); err != nil {
			t.Fatal(err)
		}
		out, stderr, err := th(t, home, cfgDup, repo, "add", "lf-dup")
		if err != nil {
			t.Fatalf("%v\n%s", err, stderr)
		}
		fi, err := os.Lstat(filepath.Join(out, "dup.txt"))
		if err != nil {
			t.Fatal(err)
		}
		if fi.Mode()&os.ModeSymlink != 0 {
			t.Error("dup.txt should be a copied regular file, not a link")
		}
		if !strings.Contains(stderr, `"dup.txt" already exists in the worktree; not linking`) {
			t.Errorf("stderr missing the already-exists note:\n%s", stderr)
		}
	})

	t.Run("remove keeps the link target", func(t *testing.T) {
		// The symlink counts as an untracked file in the worktree (unless
		// gitignored), so a non-interactive remove needs --force.
		if _, stderr, err := th(t, home, cfgLink, repo, "remove", "--force", "lf-link"); err != nil {
			t.Fatalf("%v\n%s", err, stderr)
		}
		// Removing the worktree removes the symlink only — the main
		// worktree's directory and its contents must survive.
		for _, f := range []string{"data.bin", "new.txt"} {
			if _, err := os.Stat(filepath.Join(repo, "shared-cache", f)); err != nil {
				t.Errorf("main worktree's shared-cache/%s lost after remove: %v", f, err)
			}
		}
	})

	t.Run("refresh copies a new copy_files entry", func(t *testing.T) {
		wt, stderr, err := th(t, home, cfg, repo, "add", "rf-copy")
		if err != nil {
			t.Fatalf("%v\n%s", err, stderr)
		}
		if err := os.WriteFile(filepath.Join(repo, "rf-new.cfg"), []byte("new\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		cfgRF := filepath.Join(work, "th-refresh-copy.json")
		cfgJSON := `{
  "worktree_dir": "` + trees + `/{repo}/{branch}",
  "repos": [{"path": "` + repo + `", "copy_files": ["rf-new.cfg"]}]
}`
		if err := os.WriteFile(cfgRF, []byte(cfgJSON), 0o644); err != nil {
			t.Fatal(err)
		}
		out, stderr, err := th(t, home, cfgRF, repo, "refresh", "rf-copy")
		if err != nil {
			t.Fatalf("%v\n%s", err, stderr)
		}
		if out != "" {
			t.Errorf("stdout = %q; want empty", out)
		}
		if _, err := os.Stat(filepath.Join(wt, "rf-new.cfg")); err != nil {
			t.Errorf("rf-new.cfg not copied on refresh: %v", err)
		}
		for _, want := range []string{"Copied 1 file(s) (", "Refreshed "} {
			if !strings.Contains(stderr, want) {
				t.Errorf("stderr missing %q:\n%s", want, stderr)
			}
		}
	})

	t.Run("refresh overwrites an updated untracked file", func(t *testing.T) {
		cfgEnv := filepath.Join(work, "th-refresh-env.json")
		cfgJSON := `{
  "worktree_dir": "` + trees + `/{repo}/{branch}",
  "repos": [{"path": "` + repo + `", "copy_files": [".env"]}]
}`
		if err := os.WriteFile(cfgEnv, []byte(cfgJSON), 0o644); err != nil {
			t.Fatal(err)
		}
		wt, stderr, err := th(t, home, cfgEnv, repo, "add", "rf-env")
		if err != nil {
			t.Fatalf("%v\n%s", err, stderr)
		}
		if err := os.WriteFile(filepath.Join(repo, ".env"), []byte("SECRET=2\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		out, stderr, err := th(t, home, cfgEnv, repo, "refresh", "rf-env")
		if err != nil {
			t.Fatalf("%v\n%s", err, stderr)
		}
		if out != "" {
			t.Errorf("stdout = %q; want empty", out)
		}
		data, err := os.ReadFile(filepath.Join(wt, ".env"))
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != "SECRET=2\n" {
			t.Errorf(".env = %q; want the refreshed content SECRET=2", data)
		}
	})

	t.Run("refresh links a later link_files entry", func(t *testing.T) {
		wt, stderr, err := th(t, home, cfg, repo, "add", "rf-link")
		if err != nil {
			t.Fatalf("%v\n%s", err, stderr)
		}
		// An untracked dir in the main worktree whose worktree-side path
		// already exists as a real directory: it must be kept, not linked.
		if err := os.MkdirAll(filepath.Join(repo, "rf-realdir"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(repo, "rf-realdir", "main.txt"), []byte("main\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(wt, "rf-realdir"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(wt, "rf-realdir", "mine.txt"), []byte("mine\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		cfgRL := filepath.Join(work, "th-refresh-link.json")
		cfgJSON := `{
  "worktree_dir": "` + trees + `/{repo}/{branch}",
  "repos": [{"path": "` + repo + `", "link_files": ["shared-cache", "rf-realdir"]}]
}`
		if err := os.WriteFile(cfgRL, []byte(cfgJSON), 0o644); err != nil {
			t.Fatal(err)
		}
		out, stderr, err := th(t, home, cfgRL, repo, "refresh", "rf-link")
		if err != nil {
			t.Fatalf("%v\n%s", err, stderr)
		}
		if out != "" {
			t.Errorf("stdout = %q; want empty", out)
		}
		link := filepath.Join(wt, "shared-cache")
		fi, err := os.Lstat(link)
		if err != nil {
			t.Fatalf("symlink not created on refresh: %v", err)
		}
		if fi.Mode()&os.ModeSymlink == 0 {
			t.Fatalf("shared-cache is not a symlink (mode %v)", fi.Mode())
		}
		dest, err := os.Readlink(link)
		if err != nil {
			t.Fatal(err)
		}
		if lfResolve(dest) != lfResolve(filepath.Join(repo, "shared-cache")) {
			t.Errorf("link target = %q; want the main worktree's shared-cache", dest)
		}
		if fi, err := os.Lstat(filepath.Join(wt, "rf-realdir")); err != nil || fi.Mode()&os.ModeSymlink != 0 {
			t.Errorf("rf-realdir must stay a real directory (err %v)", err)
		}
		if _, err := os.Stat(filepath.Join(wt, "rf-realdir", "mine.txt")); err != nil {
			t.Errorf("pre-existing rf-realdir content lost: %v", err)
		}
		if !strings.Contains(stderr, `"rf-realdir" already exists in the worktree; not linking`) {
			t.Errorf("stderr missing the already-exists note:\n%s", stderr)
		}
	})

	t.Run("refresh never touches tracked files", func(t *testing.T) {
		wt, stderr, err := th(t, home, cfg, repo, "add", "rf-guard")
		if err != nil {
			t.Fatalf("%v\n%s", err, stderr)
		}
		cfgRG := filepath.Join(work, "th-refresh-guard.json")
		cfgJSON := `{
  "worktree_dir": "` + trees + `/{repo}/{branch}",
  "repos": [{"path": "` + repo + `", "copy_files": ["confdir"]}]
}`
		if err := os.WriteFile(cfgRG, []byte(cfgJSON), 0o644); err != nil {
			t.Fatal(err)
		}
		out, stderr, err := th(t, home, cfgRG, repo, "refresh", "rf-guard")
		if err != nil {
			t.Fatalf("%v\n%s", err, stderr)
		}
		if out != "" {
			t.Errorf("stdout = %q; want empty", out)
		}
		// confdir/a.txt is tracked and dirty in the main worktree; the
		// refresh must keep the checkout's committed content.
		data, err := os.ReadFile(filepath.Join(wt, "confdir", "a.txt"))
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != "committed-a\n" {
			t.Errorf("tracked a.txt = %q; want the checkout's committed content", data)
		}
		if _, err := os.Stat(filepath.Join(wt, "confdir", "b.local")); err != nil {
			t.Errorf("untracked b.local not copied on refresh: %v", err)
		}
		if !strings.Contains(stderr, `skipped 1 tracked file(s) under "confdir"`) {
			t.Errorf("stderr missing the counted skip note:\n%s", stderr)
		}
	})

	t.Run("refresh regenerates the workspace file", func(t *testing.T) {
		cfgWS := filepath.Join(work, "th-refresh-ws.json")
		cfgJSON := `{
  "worktree_dir": "` + trees + `/{repo}/{branch}",
  "repos": [{"path": "` + repo + `", "vscode": {"workspace_file": true}}]
}`
		if err := os.WriteFile(cfgWS, []byte(cfgJSON), 0o644); err != nil {
			t.Fatal(err)
		}
		wt, stderr, err := th(t, home, cfgWS, repo, "add", "rf-ws")
		if err != nil {
			t.Fatalf("%v\n%s", err, stderr)
		}
		wsPath := filepath.Join(filepath.Dir(wt), "rf-ws.code-workspace")
		if data, err := os.ReadFile(wsPath); err != nil {
			t.Fatalf("workspace file not written on add: %v", err)
		} else if strings.Contains(string(data), "colorCustomizations") {
			t.Fatalf("workspace file already has colors before the config change:\n%s", data)
		}

		cfgWSColor := filepath.Join(work, "th-refresh-ws-color.json")
		cfgJSON = `{
  "worktree_dir": "` + trees + `/{repo}/{branch}",
  "repos": [{"path": "` + repo + `", "vscode": {"workspace_file": true, "window_color": "#AABBCC"}}]
}`
		if err := os.WriteFile(cfgWSColor, []byte(cfgJSON), 0o644); err != nil {
			t.Fatal(err)
		}
		out, stderr, err := th(t, home, cfgWSColor, repo, "refresh", "rf-ws")
		if err != nil {
			t.Fatalf("%v\n%s", err, stderr)
		}
		if out != "" {
			t.Errorf("stdout = %q; want empty", out)
		}
		if !strings.Contains(stderr, "Wrote ") {
			t.Errorf("stderr missing the Wrote line:\n%s", stderr)
		}
		cc := readColorCustomizations(t, wt)
		if got, _ := cc["titleBar.activeBackground"].(string); got != "#aabbcc" {
			t.Errorf("titleBar.activeBackground = %q; want #aabbcc after refresh", got)
		}
	})

	t.Run("refresh runs post_create only with --post-create", func(t *testing.T) {
		wt, stderr, err := th(t, home, cfg, repo, "add", "rf-pc")
		if err != nil {
			t.Fatalf("%v\n%s", err, stderr)
		}
		cfgPC := filepath.Join(work, "th-refresh-pc.json")
		cfgJSON := `{
  "worktree_dir": "` + trees + `/{repo}/{branch}",
  "repos": [{"path": "` + repo + `", "post_create": ["echo ran > rf-marker.txt"]}]
}`
		if err := os.WriteFile(cfgPC, []byte(cfgJSON), 0o644); err != nil {
			t.Fatal(err)
		}

		// Without the flag the commands do not re-run.
		out, stderr, err := th(t, home, cfgPC, repo, "refresh", "rf-pc")
		if err != nil {
			t.Fatalf("%v\n%s", err, stderr)
		}
		if out != "" {
			t.Errorf("stdout = %q; want empty", out)
		}
		if _, err := os.Stat(filepath.Join(wt, "rf-marker.txt")); !os.IsNotExist(err) {
			t.Error("post_create ran without --post-create")
		}

		// With the flag they do (from the user-owned config: no gate).
		if _, stderr, err := th(t, home, cfgPC, repo, "refresh", "--post-create", "rf-pc"); err != nil {
			t.Fatalf("%v\n%s", err, stderr)
		}
		if _, err := os.Stat(filepath.Join(wt, "rf-marker.txt")); err != nil {
			t.Errorf("--post-create did not run the commands: %v", err)
		}

		// Commands a repo .thrc supplied stay trust-gated: unapproved and
		// without a terminal they are skipped, and the refresh still
		// succeeds.
		if err := os.WriteFile(localCfg, []byte(`{"post_create": ["echo ran > rf-thrc-marker.txt"]}`), 0o644); err != nil {
			t.Fatal(err)
		}
		defer os.Remove(localCfg)
		out, stderr, err = th(t, home, cfg, repo, "refresh", "--post-create", "rf-pc")
		if err != nil {
			t.Fatalf("%v\n%s", err, stderr)
		}
		if out != "" {
			t.Errorf("stdout = %q; want empty", out)
		}
		if !strings.Contains(stderr, "is not approved; skipping") {
			t.Errorf("stderr = %q; want the not-approved warning", stderr)
		}
		if _, err := os.Stat(filepath.Join(wt, "rf-thrc-marker.txt")); !os.IsNotExist(err) {
			t.Error("unapproved repo post_create ran on refresh")
		}
	})

	t.Run("refresh refuses the main worktree", func(t *testing.T) {
		out, stderr, err := th(t, home, cfg, repo, "refresh", "main")
		if err == nil {
			t.Fatal("expected error refreshing the main worktree")
		}
		if out != "" {
			t.Errorf("stdout = %q; want empty", out)
		}
		if !strings.Contains(stderr, "refusing to refresh the main worktree") {
			t.Errorf("stderr = %q; want the main-worktree refusal", stderr)
		}

		out, stderr, err = th(t, home, cfg, repo, "refresh")
		if err == nil {
			t.Fatal("expected error refreshing from the main worktree with no args")
		}
		if out != "" {
			t.Errorf("stdout = %q; want empty", out)
		}
		if !strings.Contains(stderr, "you are in the main worktree; pass a branch to refresh") {
			t.Errorf("stderr = %q; want the pass-a-branch hint", stderr)
		}
	})

	t.Run("refresh unknown branch fails", func(t *testing.T) {
		_, stderr, err := th(t, home, cfg, repo, "refresh", "rf-no-such")
		if err == nil {
			t.Fatal("expected error for an unknown worktree")
		}
		if !strings.Contains(stderr, "no worktree found") {
			t.Errorf("stderr = %q; want no-worktree-found error", stderr)
		}
	})

	t.Run("refresh with no args targets the current worktree", func(t *testing.T) {
		wt, stderr, err := th(t, home, cfg, repo, "add", "rf-noarg")
		if err != nil {
			t.Fatalf("%v\n%s", err, stderr)
		}
		if err := os.WriteFile(filepath.Join(repo, "rf-noarg.cfg"), []byte("here\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		cfgNA := filepath.Join(work, "th-refresh-noarg.json")
		cfgJSON := `{
  "worktree_dir": "` + trees + `/{repo}/{branch}",
  "repos": [{"path": "` + repo + `", "copy_files": ["rf-noarg.cfg"]}]
}`
		if err := os.WriteFile(cfgNA, []byte(cfgJSON), 0o644); err != nil {
			t.Fatal(err)
		}
		out, stderr, err := th(t, home, cfgNA, wt, "refresh")
		if err != nil {
			t.Fatalf("%v\n%s", err, stderr)
		}
		if out != "" {
			t.Errorf("stdout = %q; want empty", out)
		}
		if _, err := os.Stat(filepath.Join(wt, "rf-noarg.cfg")); err != nil {
			t.Errorf("no-arg refresh did not provision the current worktree: %v", err)
		}
		if !strings.Contains(stderr, "Refreshed ") {
			t.Errorf("stderr missing the Refreshed line:\n%s", stderr)
		}
	})

	t.Run("refresh multiple branches shows counters", func(t *testing.T) {
		for _, b := range []string{"rf-multi-a", "rf-multi-b"} {
			if _, stderr, err := th(t, home, cfg, repo, "add", b); err != nil {
				t.Fatalf("%v\n%s", err, stderr)
			}
		}
		out, stderr, err := th(t, home, cfg, repo, "refresh", "rf-multi-a", "rf-multi-b")
		if err != nil {
			t.Fatalf("%v\n%s", err, stderr)
		}
		if out != "" {
			t.Errorf("stdout = %q; want empty", out)
		}
		for _, want := range []string{"[1/2] Refreshing ", "[2/2] Refreshing "} {
			if !strings.Contains(stderr, want) {
				t.Errorf("stderr missing %q:\n%s", want, stderr)
			}
		}
		if got := strings.Count(stderr, "Refreshed "); got != 2 {
			t.Errorf("stderr has %d Refreshed lines; want 2:\n%s", got, stderr)
		}
	})
}

// assertEffectiveRow checks that the config --effective table has a row for
// setting carrying the given value and source.
func assertEffectiveRow(t *testing.T, out, setting, value, source string) {
	t.Helper()
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) > 0 && fields[0] == setting {
			if len(fields) < 3 || fields[1] != value || fields[len(fields)-1] != source {
				t.Errorf("row %q = %q; want value %q from %q", setting, line, value, source)
			}
			return
		}
	}
	t.Errorf("no %q row in output:\n%s", setting, out)
}

// TestAddPR exercises th add pr fully offline: a local origin repo carries
// refs/pull/<n>/head refs (which clones do not fetch, mirroring GitHub), and
// gh is a stub script on PATH.
func TestAddPR(t *testing.T) {
	home := t.TempDir()
	work := t.TempDir()

	origin := filepath.Join(work, "origin-src")
	if err := os.MkdirAll(origin, 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, home, origin, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(origin, "file.txt"), []byte("hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, home, origin, "add", ".")
	git(t, home, origin, "commit", "-m", "init")

	// commitOn creates a one-commit branch off main and returns to main.
	commitOn := func(branch, file string) {
		git(t, home, origin, "checkout", "-b", branch, "main")
		if err := os.WriteFile(filepath.Join(origin, file), []byte(branch+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		git(t, home, origin, "add", ".")
		git(t, home, origin, "commit", "-m", "change on "+branch)
		git(t, home, origin, "checkout", "main")
	}

	// PR 7: same-repo, head branch alive on origin.
	commitOn("alice/fix-login", "login.txt")
	git(t, home, origin, "update-ref", "refs/pull/7/head", "refs/heads/alice/fix-login")

	// PRs whose head exists only under refs/pull (fork PR 8, merged PR 9,
	// no-gh PRs 11+): branch deleted after capturing the ref.
	for _, pr := range []struct {
		n      int
		branch string
	}{{8, "bob/feature"}, {9, "merged-cleanup"}, {11, "gone-a"}, {12, "gone-b"}} {
		commitOn(pr.branch, fmt.Sprintf("pr%d.txt", pr.n))
		git(t, home, origin, "update-ref", fmt.Sprintf("refs/pull/%d/head", pr.n), "refs/heads/"+pr.branch)
		git(t, home, origin, "branch", "-D", pr.branch)
	}

	// PR 10: same-repo, alive branch, addressed by URL.
	commitOn("carol/url-test", "url.txt")
	git(t, home, origin, "update-ref", "refs/pull/10/head", "refs/heads/carol/url-test")

	git(t, home, work, "clone", origin, "myapp")
	repo := filepath.Join(work, "myapp")

	trees := filepath.Join(work, "trees")
	cfg := filepath.Join(work, "th.json")
	cfgJSON := `{"worktree_dir": "` + trees + `/{repo}/{branch}"}`
	if err := os.WriteFile(cfg, []byte(cfgJSON), 0o644); err != nil {
		t.Fatal(err)
	}

	// A gh stub answering `gh pr view <n> --json ...` ($3 is the number)
	// with canned JSON, logging every invocation.
	stub := t.TempDir()
	ghLog := filepath.Join(stub, "gh.log")
	ghJSON := func(n int, state, headRef string, cross bool, owner string) string {
		return fmt.Sprintf(`{"number":%d,"state":%q,"title":"t","url":"https://github.com/acme/myapp/pull/%d","headRefName":%q,"isCrossRepository":%t,"headRepositoryOwner":{"login":%q}}`,
			n, state, n, headRef, cross, owner)
	}
	script := `#!/bin/sh
echo "$@" >> ` + ghLog + `
case "$3" in
7) cat <<'EOF'
` + ghJSON(7, "OPEN", "alice/fix-login", false, "acme") + `
EOF
;;
8) cat <<'EOF'
` + ghJSON(8, "OPEN", "bob/feature", true, "bob") + `
EOF
;;
9) cat <<'EOF'
` + ghJSON(9, "MERGED", "merged-cleanup", false, "acme") + `
EOF
;;
10) cat <<'EOF'
` + ghJSON(10, "OPEN", "carol/url-test", false, "acme") + `
EOF
;;
*) echo "GraphQL: Could not resolve to a PullRequest" >&2; exit 1 ;;
esac
`
	if err := os.WriteFile(filepath.Join(stub, "gh"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	withGH := []string{"PATH=" + stub + string(os.PathListSeparator) + os.Getenv("PATH")}

	// A stub dir whose gh always fails, so fallback tests can never reach a
	// developer's real gh.
	noGHDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(noGHDir, "gh"), []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	noGH := []string{"PATH=" + noGHDir + string(os.PathListSeparator) + os.Getenv("PATH")}

	t.Run("same-repo PR via gh", func(t *testing.T) {
		out, stderr, err := thWithEnv(t, home, cfg, repo, withGH, "add", "pr", "7")
		if err != nil {
			t.Fatalf("%v\n%s", err, stderr)
		}
		want := filepath.Join(trees, "myapp", "alice-fix-login")
		if out != want {
			t.Errorf("stdout = %q; want %q", out, want)
		}
		if got := git(t, home, out, "rev-parse", "--abbrev-ref", "HEAD"); got != "alice/fix-login" {
			t.Errorf("checked-out branch = %q; want alice/fix-login", got)
		}
		if up := git(t, home, out, "rev-parse", "--abbrev-ref", "alice/fix-login@{upstream}"); up != "origin/alice/fix-login" {
			t.Errorf("upstream = %q; want origin/alice/fix-login", up)
		}
		logged, err := os.ReadFile(ghLog)
		if err != nil {
			t.Fatalf("stub gh was not invoked: %v", err)
		}
		if !strings.Contains(string(logged), "pr view 7 --json") {
			t.Errorf("gh log = %q; want a pr view 7 --json invocation", logged)
		}
	})

	t.Run("cross-repo PR via gh", func(t *testing.T) {
		out, stderr, err := thWithEnv(t, home, cfg, repo, withGH, "add", "pr", "8")
		if err != nil {
			t.Fatalf("%v\n%s", err, stderr)
		}
		if got := git(t, home, out, "rev-parse", "--abbrev-ref", "HEAD"); got != "bob/feature" {
			t.Errorf("checked-out branch = %q; want bob/feature", got)
		}
		if head, prHead := git(t, home, out, "rev-parse", "HEAD"), git(t, home, origin, "rev-parse", "refs/pull/8/head"); head != prHead {
			t.Errorf("worktree HEAD = %s; want the PR head %s", head, prHead)
		}
		up := exec.Command("git", "rev-parse", "--abbrev-ref", "bob/feature@{upstream}")
		up.Dir = out
		up.Env = gitEnv(home)
		if up.Run() == nil {
			t.Error("fork PR branch should have no upstream")
		}
		if !strings.Contains(stderr, "fork") {
			t.Errorf("stderr = %q; want a fork note", stderr)
		}
	})

	t.Run("PR URL argument", func(t *testing.T) {
		out, stderr, err := thWithEnv(t, home, cfg, repo, withGH, "add", "pr", "https://github.com/acme/myapp/pull/10/files")
		if err != nil {
			t.Fatalf("%v\n%s", err, stderr)
		}
		if got := git(t, home, out, "rev-parse", "--abbrev-ref", "HEAD"); got != "carol/url-test" {
			t.Errorf("checked-out branch = %q; want carol/url-test", got)
		}
	})

	t.Run("already checked out PR fails", func(t *testing.T) {
		_, stderr, err := thWithEnv(t, home, cfg, repo, withGH, "add", "pr", "7")
		if err == nil {
			t.Fatal("expected error for already checked out PR branch")
		}
		if !strings.Contains(stderr, "already checked out") {
			t.Errorf("stderr = %q; want mention of already checked out", stderr)
		}
	})

	t.Run("merged PR warns but proceeds", func(t *testing.T) {
		out, stderr, err := thWithEnv(t, home, cfg, repo, withGH, "add", "pr", "9")
		if err != nil {
			t.Fatalf("%v\n%s", err, stderr)
		}
		if !strings.Contains(stderr, "merged") {
			t.Errorf("stderr = %q; want a merged warning", stderr)
		}
		// Head branch is gone from origin, so the worktree comes from
		// refs/pull/9/head on the branch name gh reported.
		if got := git(t, home, out, "rev-parse", "--abbrev-ref", "HEAD"); got != "merged-cleanup" {
			t.Errorf("checked-out branch = %q; want merged-cleanup", got)
		}
		if head, prHead := git(t, home, out, "rev-parse", "HEAD"), git(t, home, origin, "rev-parse", "refs/pull/9/head"); head != prHead {
			t.Errorf("worktree HEAD = %s; want the PR head %s", head, prHead)
		}
	})

	t.Run("fallback without gh", func(t *testing.T) {
		out, stderr, err := thWithEnv(t, home, cfg, repo, noGH, "add", "pr", "11")
		if err != nil {
			t.Fatalf("%v\n%s", err, stderr)
		}
		if !strings.Contains(stderr, "falling back to plain git") {
			t.Errorf("stderr = %q; want the falling-back note", stderr)
		}
		want := filepath.Join(trees, "myapp", "pr-11")
		if out != want {
			t.Errorf("stdout = %q; want %q", out, want)
		}
		if got := git(t, home, out, "rev-parse", "--abbrev-ref", "HEAD"); got != "pr-11" {
			t.Errorf("checked-out branch = %q; want pr-11", got)
		}
		if head, prHead := git(t, home, out, "rev-parse", "HEAD"), git(t, home, origin, "rev-parse", "refs/pull/11/head"); head != prHead {
			t.Errorf("worktree HEAD = %s; want the PR head %s", head, prHead)
		}
	})

	t.Run("fallback reuses existing branch", func(t *testing.T) {
		git(t, home, repo, "worktree", "remove", filepath.Join(trees, "myapp", "pr-11"))
		_, stderr, err := thWithEnv(t, home, cfg, repo, noGH, "add", "pr", "11")
		if err != nil {
			t.Fatalf("%v\n%s", err, stderr)
		}
		if !strings.Contains(stderr, "already exists locally") {
			t.Errorf("stderr = %q; want the reuse note", stderr)
		}
	})

	t.Run("nonexistent PR fails", func(t *testing.T) {
		_, stderr, err := thWithEnv(t, home, cfg, repo, noGH, "add", "pr", "999")
		if err == nil {
			t.Fatal("expected error for a nonexistent PR")
		}
		if !strings.Contains(stderr, "PR #999") || !strings.Contains(stderr, "fetched") {
			t.Errorf("stderr = %q; want a PR #999 fetch error", stderr)
		}
	})

	t.Run("bad arguments", func(t *testing.T) {
		_, stderr, err := thWithEnv(t, home, cfg, repo, withGH, "add", "pr", "notanumber")
		if err == nil {
			t.Fatal("expected error for a non-numeric argument")
		}
		if !strings.Contains(stderr, "pull request number") {
			t.Errorf("stderr = %q; want mention of pull request number", stderr)
		}

		_, stderr, err = thWithEnv(t, home, cfg, repo, withGH, "add", "pr")
		if err == nil {
			t.Fatal("expected error for a missing argument")
		}
		if !strings.Contains(stderr, "pull request number") {
			t.Errorf("stderr = %q; want mention of pull request number", stderr)
		}
	})

	t.Run("shared flags work on add pr", func(t *testing.T) {
		custom := filepath.Join(work, "custom-pr-12")
		out, stderr, err := thWithEnv(t, home, cfg, repo, noGH, "add", "pr", "--path", custom, "12")
		if err != nil {
			t.Fatalf("%v\n%s", err, stderr)
		}
		if out != custom {
			t.Errorf("stdout = %q; want %q", out, custom)
		}
	})
}

// TestClean exercises th clean fully offline: a local origin provides a
// merged branch, a branch later deleted on origin, and an active branch.
func TestClean(t *testing.T) {
	home := t.TempDir()
	work := t.TempDir()

	origin := filepath.Join(work, "origin-src")
	if err := os.MkdirAll(origin, 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, home, origin, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(origin, "file.txt"), []byte("hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, home, origin, "add", ".")
	git(t, home, origin, "commit", "-m", "init")

	// Merged: sits at main's tip. Gone/active: one unmerged commit each.
	git(t, home, origin, "branch", "feature-merged")
	for _, b := range []string{"gone-branch", "active"} {
		git(t, home, origin, "checkout", "-b", b, "main")
		if err := os.WriteFile(filepath.Join(origin, b+".txt"), []byte(b+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		git(t, home, origin, "add", ".")
		git(t, home, origin, "commit", "-m", "work on "+b)
		git(t, home, origin, "checkout", "main")
	}

	git(t, home, work, "clone", origin, "myapp")
	repo := filepath.Join(work, "myapp")
	trees := filepath.Join(work, "trees")
	cfg := filepath.Join(work, "th.json")
	if err := os.WriteFile(cfg, []byte(`{"worktree_dir": "`+trees+`/{repo}/{branch}"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	for _, b := range []string{"feature-merged", "gone-branch", "active"} {
		if _, stderr, err := th(t, home, cfg, repo, "add", b); err != nil {
			t.Fatalf("add %s: %v\n%s", b, err, stderr)
		}
	}
	git(t, home, origin, "branch", "-D", "gone-branch")

	mergedDir := filepath.Join(trees, "myapp", "feature-merged")
	goneDir := filepath.Join(trees, "myapp", "gone-branch")
	activeDir := filepath.Join(trees, "myapp", "active")

	t.Run("dry-run lists candidates", func(t *testing.T) {
		_, stderr, err := th(t, home, cfg, repo, "clean", "--dry-run")
		if err != nil {
			t.Fatalf("%v\n%s", err, stderr)
		}
		for _, want := range []string{
			"feature-merged", "merged into main",
			"gone-branch", "gone from origin",
			"Would remove 2 worktrees",
		} {
			if !strings.Contains(stderr, want) {
				t.Errorf("stderr missing %q:\n%s", want, stderr)
			}
		}
		if strings.Contains(stderr, "active") {
			t.Errorf("stderr lists the active branch:\n%s", stderr)
		}
		for _, dir := range []string{mergedDir, goneDir, activeDir} {
			if _, err := os.Stat(dir); err != nil {
				t.Errorf("dry run must not remove %s: %v", dir, err)
			}
		}
	})

	t.Run("non-interactive without yes fails", func(t *testing.T) {
		_, stderr, err := th(t, home, cfg, repo, "clean")
		if err == nil {
			t.Fatal("expected an error without a terminal")
		}
		if !strings.Contains(stderr, "--yes") {
			t.Errorf("stderr = %q; want a pointer to --yes", stderr)
		}
	})

	t.Run("yes removes candidates and keeps branches", func(t *testing.T) {
		out, stderr, err := th(t, home, cfg, repo, "clean", "--yes")
		if err != nil {
			t.Fatalf("%v\n%s", err, stderr)
		}
		if out != "" {
			t.Errorf("stdout = %q; want empty", out)
		}
		for _, dir := range []string{mergedDir, goneDir} {
			if _, err := os.Stat(dir); !os.IsNotExist(err) {
				t.Errorf("worktree dir %s still exists", dir)
			}
		}
		if _, err := os.Stat(activeDir); err != nil {
			t.Errorf("active worktree was removed: %v", err)
		}
		for _, b := range []string{"feature-merged", "gone-branch"} {
			git(t, home, repo, "rev-parse", "--verify", "refs/heads/"+b)
		}
		for _, want := range []string{"[1/2] Removing ", "[2/2] Removing ", "Removed worktree"} {
			if !strings.Contains(stderr, want) {
				t.Errorf("stderr missing %q:\n%s", want, stderr)
			}
		}
	})

	t.Run("nothing to clean afterwards", func(t *testing.T) {
		_, stderr, err := th(t, home, cfg, repo, "clean", "--yes")
		if err != nil {
			t.Fatalf("%v\n%s", err, stderr)
		}
		if !strings.Contains(stderr, "Nothing to clean") {
			t.Errorf("stderr = %q; want Nothing to clean", stderr)
		}
	})

	t.Run("delete-branches dry-run deletes nothing", func(t *testing.T) {
		// Re-add worktrees for the branches the earlier clean kept:
		// feature-merged sits at main's tip, gone-branch still carries its
		// local-only commit.
		for _, b := range []string{"feature-merged", "gone-branch"} {
			if _, stderr, err := th(t, home, cfg, repo, "add", b); err != nil {
				t.Fatalf("add %s: %v\n%s", b, err, stderr)
			}
		}
		_, stderr, err := th(t, home, cfg, repo, "clean", "--dry-run", "-d")
		if err != nil {
			t.Fatalf("%v\n%s", err, stderr)
		}
		if !strings.Contains(stderr, "merged branches would be deleted too") {
			t.Errorf("stderr = %q; want the delete-branches dry-run note", stderr)
		}
		for _, b := range []string{"feature-merged", "gone-branch"} {
			git(t, home, repo, "rev-parse", "--verify", "refs/heads/"+b)
			if _, err := os.Stat(filepath.Join(trees, "myapp", b)); err != nil {
				t.Errorf("dry run must not remove the %s worktree: %v", b, err)
			}
		}
	})

	t.Run("yes with delete-branches keeps unmerged", func(t *testing.T) {
		out, stderr, err := th(t, home, cfg, repo, "clean", "--yes", "--delete-branches")
		if err != nil {
			t.Fatalf("%v\n%s", err, stderr)
		}
		if out != "" {
			t.Errorf("stdout = %q; want empty", out)
		}
		for _, b := range []string{"feature-merged", "gone-branch"} {
			if _, err := os.Stat(filepath.Join(trees, "myapp", b)); !os.IsNotExist(err) {
				t.Errorf("worktree dir for %s still exists", b)
			}
		}
		merged := exec.Command("git", "rev-parse", "--verify", "--quiet", "refs/heads/feature-merged")
		merged.Dir = repo
		merged.Env = gitEnv(home)
		if merged.Run() == nil {
			t.Error("branch feature-merged still exists")
		}
		// The gone-from-origin branch has a local-only commit, so git
		// refuses -d and --yes must not escalate to -D.
		git(t, home, repo, "rev-parse", "--verify", "refs/heads/gone-branch")
		if !strings.Contains(stderr, `(branch "feature-merged" deleted)`) {
			t.Errorf("stderr missing the deleted note:\n%s", stderr)
		}
		if !strings.Contains(stderr, `(branch "gone-branch" kept: not fully merged`) {
			t.Errorf("stderr missing the kept note:\n%s", stderr)
		}
	})
}

func TestDeleteBranch(t *testing.T) {
	home := t.TempDir()
	work := t.TempDir()

	origin := filepath.Join(work, "origin-src")
	if err := os.MkdirAll(origin, 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, home, origin, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(origin, "file.txt"), []byte("hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, home, origin, "add", ".")
	git(t, home, origin, "commit", "-m", "init")

	git(t, home, work, "clone", origin, "myapp")
	repo := filepath.Join(work, "myapp")
	trees := filepath.Join(work, "trees")
	cfg := filepath.Join(work, "th.json")
	if err := os.WriteFile(cfg, []byte(`{"worktree_dir": "`+trees+`/{repo}/{branch}"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	branchExists := func(branch string) bool {
		cmd := exec.Command("git", "rev-parse", "--verify", "--quiet", "refs/heads/"+branch)
		cmd.Dir = repo
		cmd.Env = gitEnv(home)
		return cmd.Run() == nil
	}
	// addUnmerged creates a worktree on a new branch and gives it a commit
	// main doesn't have, so git branch -d refuses to delete it.
	addUnmerged := func(branch string) string {
		t.Helper()
		out, stderr, err := th(t, home, cfg, repo, "add", branch)
		if err != nil {
			t.Fatalf("add %s: %v\n%s", branch, err, stderr)
		}
		if err := os.WriteFile(filepath.Join(out, branch+".txt"), []byte(branch+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		git(t, home, out, "add", ".")
		git(t, home, out, "commit", "-m", "work on "+branch)
		return out
	}

	t.Run("merged branch is deleted", func(t *testing.T) {
		if _, stderr, err := th(t, home, cfg, repo, "add", "del-merged"); err != nil {
			t.Fatalf("%v\n%s", err, stderr)
		}
		out, stderr, err := th(t, home, cfg, repo, "remove", "-d", "del-merged")
		if err != nil {
			t.Fatalf("%v\n%s", err, stderr)
		}
		if out != "" {
			t.Errorf("stdout = %q; want empty", out)
		}
		dir := filepath.Join(trees, "myapp", "del-merged")
		if _, err := os.Stat(dir); !os.IsNotExist(err) {
			t.Errorf("worktree dir %s still exists", dir)
		}
		if branchExists("del-merged") {
			t.Error("branch del-merged still exists")
		}
		if !strings.Contains(stderr, `(branch "del-merged" deleted)`) {
			t.Errorf("stderr = %q; want deleted note", stderr)
		}
	})

	t.Run("unmerged branch is kept non-interactively", func(t *testing.T) {
		dir := addUnmerged("del-unmerged")
		_, stderr, err := th(t, home, cfg, repo, "remove", "--delete-branch", "del-unmerged")
		if err != nil {
			t.Fatalf("removal must succeed even when the branch is kept: %v\n%s", err, stderr)
		}
		if _, err := os.Stat(dir); !os.IsNotExist(err) {
			t.Errorf("worktree dir %s still exists", dir)
		}
		if !branchExists("del-unmerged") {
			t.Error("unmerged branch del-unmerged was deleted")
		}
		if !strings.Contains(stderr, "kept: not fully merged") || !strings.Contains(stderr, "git branch -D") {
			t.Errorf("stderr = %q; want kept-not-fully-merged note pointing at git branch -D", stderr)
		}
	})

	t.Run("force does not escalate to -D", func(t *testing.T) {
		addUnmerged("keep-forced")
		_, stderr, err := th(t, home, cfg, repo, "remove", "-d", "-f", "keep-forced")
		if err != nil {
			t.Fatalf("%v\n%s", err, stderr)
		}
		if !branchExists("keep-forced") {
			t.Error("--force must not force-delete an unmerged branch")
		}
		if !strings.Contains(stderr, "kept: not fully merged") {
			t.Errorf("stderr = %q; want kept-not-fully-merged note", stderr)
		}
	})

	t.Run("multi-remove deletes merged and keeps unmerged", func(t *testing.T) {
		if _, stderr, err := th(t, home, cfg, repo, "add", "multi-m"); err != nil {
			t.Fatalf("%v\n%s", err, stderr)
		}
		addUnmerged("multi-u")
		_, stderr, err := th(t, home, cfg, repo, "remove", "-d", "multi-m", "multi-u")
		if err != nil {
			t.Fatalf("%v\n%s", err, stderr)
		}
		for _, want := range []string{
			"[1/2] Removing ", "[2/2] Removing ",
			`(branch "multi-m" deleted)`,
			`(branch "multi-u" kept: not fully merged`,
		} {
			if !strings.Contains(stderr, want) {
				t.Errorf("stderr missing %q:\n%s", want, stderr)
			}
		}
		if branchExists("multi-m") {
			t.Error("branch multi-m still exists")
		}
		if !branchExists("multi-u") {
			t.Error("branch multi-u was deleted")
		}
	})

	t.Run("default branch is never deleted", func(t *testing.T) {
		// Check out something else in the main worktree so main itself can
		// live in a linked worktree.
		git(t, home, repo, "checkout", "-b", "side")
		if _, stderr, err := th(t, home, cfg, repo, "add", "main"); err != nil {
			t.Fatalf("%v\n%s", err, stderr)
		}
		_, stderr, err := th(t, home, cfg, repo, "remove", "-d", "main")
		if err != nil {
			t.Fatalf("%v\n%s", err, stderr)
		}
		dir := filepath.Join(trees, "myapp", "main")
		if _, err := os.Stat(dir); !os.IsNotExist(err) {
			t.Errorf("worktree dir %s still exists", dir)
		}
		if !branchExists("main") {
			t.Fatal("the default branch was deleted")
		}
		if !strings.Contains(stderr, `(branch "main" kept: default branch)`) {
			t.Errorf("stderr = %q; want default-branch note", stderr)
		}
		git(t, home, repo, "checkout", "main")
	})

	t.Run("detached worktree prints the plain line", func(t *testing.T) {
		dir := filepath.Join(trees, "myapp", "detached")
		git(t, home, repo, "worktree", "add", "--detach", dir, "main")
		_, stderr, err := th(t, home, cfg, repo, "remove", "-d", dir)
		if err != nil {
			t.Fatalf("%v\n%s", err, stderr)
		}
		if _, err := os.Stat(dir); !os.IsNotExist(err) {
			t.Errorf("worktree dir %s still exists", dir)
		}
		if !strings.Contains(stderr, "Removed worktree ") || strings.Contains(stderr, "(branch") {
			t.Errorf("stderr = %q; want the plain Removed line with no branch note", stderr)
		}
	})

	t.Run("root -r -d shorthand deletes the branch", func(t *testing.T) {
		if _, stderr, err := th(t, home, cfg, repo, "add", "root-del"); err != nil {
			t.Fatalf("%v\n%s", err, stderr)
		}
		_, stderr, err := th(t, home, cfg, repo, "-r", "-d", "root-del")
		if err != nil {
			t.Fatalf("%v\n%s", err, stderr)
		}
		if branchExists("root-del") {
			t.Error("branch root-del still exists")
		}
		if !strings.Contains(stderr, `(branch "root-del" deleted)`) {
			t.Errorf("stderr = %q; want deleted note", stderr)
		}
	})
}

// TestLifecycleHooks exercises the pre_create, pre_remove, and post_remove
// hooks end to end (post_create has its own tests above): where each one
// runs, what a failure blocks, the skip flags, the per-hook trust gate for
// .thrc-supplied commands, and th clean firing the remove hooks.
func TestLifecycleHooks(t *testing.T) {
	home := t.TempDir()
	work := t.TempDir()

	origin := filepath.Join(work, "origin-src")
	if err := os.MkdirAll(origin, 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, home, origin, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(origin, "file.txt"), []byte("hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, home, origin, "add", ".")
	git(t, home, origin, "commit", "-m", "init")

	git(t, home, work, "clone", origin, "myapp")
	repo := filepath.Join(work, "myapp")
	trees := filepath.Join(work, "trees")

	// git reports symlink-resolved paths (/private/var vs /var on macOS),
	// and normalizePath resolves the trust store's keys the same way.
	mainResolved, err := filepath.EvalSymlinks(repo)
	if err != nil {
		t.Fatal(err)
	}
	resolve := func(p string) string {
		if r, err := filepath.EvalSymlinks(p); err == nil {
			return r
		}
		return p
	}

	cfg := filepath.Join(work, "th.json")
	if err := os.WriteFile(cfg, []byte(`{"worktree_dir": "`+trees+`/{repo}/{branch}"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	// writeCfg writes a config whose repos entry carries the given extra
	// settings JSON (user-owned, so hooks in it are never gated).
	writeCfg := func(t *testing.T, name, extra string) string {
		t.Helper()
		p := filepath.Join(work, name)
		cfgJSON := `{
  "worktree_dir": "` + trees + `/{repo}/{branch}",
  "repos": [{"path": "` + repo + `", ` + extra + `}]
}`
		if err := os.WriteFile(p, []byte(cfgJSON), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	branchExists := func(branch string) bool {
		check := exec.Command("git", "rev-parse", "--verify", "refs/heads/"+branch)
		check.Dir = repo
		check.Env = gitEnv(home)
		return check.Run() == nil
	}

	t.Run("pre_create runs in the main worktree before creation", func(t *testing.T) {
		cfgPre := writeCfg(t, "th-precreate.json", `"pre_create": ["test ! -d \"$TH_WORKTREE\" && pwd > order.txt"]`)
		out, stderr, err := th(t, home, cfgPre, repo, "add", "hk-pre")
		if err != nil {
			t.Fatalf("%v\n%s", err, stderr)
		}
		// The hook both proved the target didn't exist yet (test ! -d) and
		// recorded its working directory: the main worktree.
		data, err := os.ReadFile(filepath.Join(repo, "order.txt"))
		if err != nil {
			t.Fatalf("pre_create did not run in the main worktree before creation: %v", err)
		}
		if got := resolve(strings.TrimSpace(string(data))); got != mainResolved {
			t.Errorf("pre_create pwd = %q; want the main worktree %q", got, mainResolved)
		}
		if _, err := os.Stat(out); err != nil {
			t.Errorf("worktree missing after add with pre_create: %v", err)
		}
		if !strings.Contains(stderr, "pre_create: ") {
			t.Errorf("stderr = %q; want the pre_create command narrated", stderr)
		}
		if _, stderr, err := th(t, home, cfg, repo, "remove", "hk-pre"); err != nil {
			t.Fatalf("%v\n%s", err, stderr)
		}
	})

	t.Run("failing pre_create aborts the add", func(t *testing.T) {
		cfgFail := writeCfg(t, "th-precreate-fail.json", `"pre_create": ["false"]`)
		_, stderr, err := th(t, home, cfgFail, repo, "add", "hk-pre-fail")
		if err == nil {
			t.Fatal("expected error when a pre_create command fails")
		}
		if !strings.Contains(stderr, "pre_create command") {
			t.Errorf("stderr = %q; want the pre_create command named", stderr)
		}
		if strings.Contains(stderr, "worktree created") {
			t.Errorf("stderr = %q; nothing was created, so nothing to report as created", stderr)
		}
		if _, err := os.Stat(filepath.Join(trees, "myapp", "hk-pre-fail")); !os.IsNotExist(err) {
			t.Error("target directory must not exist after an aborted add")
		}
		if branchExists("hk-pre-fail") {
			t.Error("branch must not exist after an aborted add")
		}
	})

	t.Run("no-pre-create skips a failing pre_create", func(t *testing.T) {
		cfgFail := writeCfg(t, "th-precreate-fail.json", `"pre_create": ["false"]`)
		out, stderr, err := th(t, home, cfgFail, repo, "add", "--no-pre-create", "hk-pre-skip")
		if err != nil {
			t.Fatalf("%v\n%s", err, stderr)
		}
		if _, err := os.Stat(out); err != nil {
			t.Errorf("worktree missing after --no-pre-create add: %v", err)
		}
		if _, stderr, err := th(t, home, cfg, repo, "remove", "hk-pre-skip"); err != nil {
			t.Fatalf("%v\n%s", err, stderr)
		}
	})

	t.Run("pre_remove runs in the worktree before removal", func(t *testing.T) {
		cfgPreRm := writeCfg(t, "th-preremove.json", `"pre_remove": ["cp inside.txt \"$TH_MAIN/copied.txt\""]`)
		out, stderr, err := th(t, home, cfgPreRm, repo, "add", "hk-prerm")
		if err != nil {
			t.Fatalf("%v\n%s", err, stderr)
		}
		if err := os.WriteFile(filepath.Join(out, "inside.txt"), []byte("inside\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		// The untracked file makes the worktree dirty; --force answers the
		// dirty question and must still run the hook.
		stdout, stderr, err := th(t, home, cfgPreRm, repo, "remove", "--force", "hk-prerm")
		if err != nil {
			t.Fatalf("%v\n%s", err, stderr)
		}
		if stdout != "" {
			t.Errorf("stdout = %q; want empty", stdout)
		}
		// Copying a file that only exists inside the worktree proves both
		// the working directory and that removal hadn't happened yet.
		data, err := os.ReadFile(filepath.Join(repo, "copied.txt"))
		if err != nil {
			t.Fatalf("pre_remove did not run inside the worktree: %v", err)
		}
		if string(data) != "inside\n" {
			t.Errorf("copied.txt = %q; want the worktree file's content", data)
		}
		if _, err := os.Stat(out); !os.IsNotExist(err) {
			t.Errorf("worktree dir %s still exists", out)
		}
	})

	t.Run("failing pre_remove blocks removal", func(t *testing.T) {
		cfgFail := writeCfg(t, "th-preremove-fail.json", `"pre_remove": ["false"]`)
		out, stderr, err := th(t, home, cfgFail, repo, "add", "hk-prerm-fail")
		if err != nil {
			t.Fatalf("%v\n%s", err, stderr)
		}
		_, stderr, err = th(t, home, cfgFail, repo, "remove", "hk-prerm-fail")
		if err == nil {
			t.Fatal("expected error when a pre_remove command fails")
		}
		if !strings.Contains(stderr, `pre_remove command "false"`) || !strings.Contains(stderr, "was not removed") {
			t.Errorf("stderr = %q; want the command named and a was-not-removed note", stderr)
		}
		if _, err := os.Stat(out); err != nil {
			t.Errorf("worktree must survive a failed pre_remove: %v", err)
		}
		if _, stderr, err := th(t, home, cfgFail, repo, "remove", "--no-pre-remove", "hk-prerm-fail"); err != nil {
			t.Fatalf("remove --no-pre-remove: %v\n%s", err, stderr)
		}
		if _, err := os.Stat(out); !os.IsNotExist(err) {
			t.Error("--no-pre-remove should have let the removal proceed")
		}
	})

	t.Run("post_remove runs in the main worktree after removal", func(t *testing.T) {
		cfgPost := writeCfg(t, "th-postremove.json", `"post_remove": ["test ! -d \"$TH_WORKTREE\" && echo \"$TH_BRANCH\" > removed.txt"]`)
		if _, stderr, err := th(t, home, cfgPost, repo, "add", "hk-postrm"); err != nil {
			t.Fatalf("%v\n%s", err, stderr)
		}
		if _, stderr, err := th(t, home, cfgPost, repo, "remove", "hk-postrm"); err != nil {
			t.Fatalf("%v\n%s", err, stderr)
		}
		// The hook proved the worktree was already gone (test ! -d) and ran
		// in the main worktree, where TH_BRANCH still names the branch.
		data, err := os.ReadFile(filepath.Join(repo, "removed.txt"))
		if err != nil {
			t.Fatalf("post_remove did not run in the main worktree: %v", err)
		}
		if strings.TrimSpace(string(data)) != "hk-postrm" {
			t.Errorf("removed.txt = %q; want the branch name hk-postrm", data)
		}
	})

	t.Run("failing post_remove reports but the removal stands", func(t *testing.T) {
		cfgFail := writeCfg(t, "th-postremove-fail.json", `"post_remove": ["false"]`)
		out, stderr, err := th(t, home, cfgFail, repo, "add", "hk-postrm-fail")
		if err != nil {
			t.Fatalf("%v\n%s", err, stderr)
		}
		_, stderr, err = th(t, home, cfgFail, repo, "remove", "hk-postrm-fail")
		if err == nil {
			t.Fatal("expected non-zero exit when a post_remove command fails")
		}
		if !strings.Contains(stderr, "worktree removed, but") || !strings.Contains(stderr, "post_remove command") {
			t.Errorf("stderr = %q; want a worktree-removed-but error naming post_remove", stderr)
		}
		if _, err := os.Stat(out); !os.IsNotExist(err) {
			t.Error("removal must stand despite the failed post_remove")
		}

		if _, stderr, err := th(t, home, cfgFail, repo, "add", "hk-postrm-skip"); err != nil {
			t.Fatalf("%v\n%s", err, stderr)
		}
		if _, stderr, err := th(t, home, cfgFail, repo, "remove", "--no-post-remove", "hk-postrm-skip"); err != nil {
			t.Fatalf("remove --no-post-remove: %v\n%s", err, stderr)
		}
	})

	t.Run("TH_ env vars reach all four hooks", func(t *testing.T) {
		envHook := func(hook string) string {
			return `"` + hook + `": ["echo \"$TH_WORKTREE|$TH_MAIN|$TH_REPO|$TH_BRANCH\" > \"$TH_MAIN/env-` + hook + `.txt\""]`
		}
		cfgEnv := writeCfg(t, "th-hook-env.json",
			envHook("pre_create")+", "+envHook("post_create")+", "+envHook("pre_remove")+", "+envHook("post_remove"))
		out, stderr, err := th(t, home, cfgEnv, repo, "add", "hk-env")
		if err != nil {
			t.Fatalf("%v\n%s", err, stderr)
		}
		// The canonical worktree path can only be resolved while the
		// directory exists, so grab it between the add and the remove.
		canonical := resolve(out)
		// pre_create sees the future target, post_remove the former path —
		// the same path the whole way through.
		checkEnv := func(hook string) {
			t.Helper()
			data, err := os.ReadFile(filepath.Join(repo, "env-"+hook+".txt"))
			if err != nil {
				t.Errorf("%s did not run: %v", hook, err)
				return
			}
			parts := strings.Split(strings.TrimSpace(string(data)), "|")
			if len(parts) != 4 {
				t.Errorf("%s env = %q; want 4 |-separated values", hook, data)
				return
			}
			if resolve(parts[0]) != canonical {
				t.Errorf("%s TH_WORKTREE = %q; want %q", hook, parts[0], canonical)
			}
			if resolve(parts[1]) != mainResolved {
				t.Errorf("%s TH_MAIN = %q; want %q", hook, parts[1], mainResolved)
			}
			if parts[2] != "myapp" {
				t.Errorf("%s TH_REPO = %q; want myapp", hook, parts[2])
			}
			if parts[3] != "hk-env" {
				t.Errorf("%s TH_BRANCH = %q; want hk-env", hook, parts[3])
			}
		}
		checkEnv("pre_create")
		checkEnv("post_create")
		if _, stderr, err := th(t, home, cfgEnv, repo, "remove", "hk-env"); err != nil {
			t.Fatalf("%v\n%s", err, stderr)
		}
		// The remove-side hooks got git's resolved form of the same path;
		// it no longer exists, so resolve() passes it through unchanged.
		checkEnv("pre_remove")
		checkEnv("post_remove")
	})

	t.Run(".thrc remove hooks are gated per hook", func(t *testing.T) {
		localCfg := filepath.Join(repo, ".thrc")
		trustFile := filepath.Join(home, ".th", "trust.json")
		defer func() {
			os.Remove(localCfg)
			os.Remove(trustFile)
		}()
		preCmd := `echo pre > "$TH_MAIN/gate-pre.txt"`
		postCmd := `echo post > "$TH_MAIN/gate-post.txt"`
		writeThrc := func(t *testing.T, hooks map[string]any) {
			t.Helper()
			data, err := json.Marshal(hooks)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(localCfg, data, 0o644); err != nil {
				t.Fatal(err)
			}
		}
		seedTrust := func(t *testing.T, hooks map[string][]string) {
			t.Helper()
			type approval struct {
				Commands   []string `json:"commands"`
				ApprovedAt string   `json:"approved_at"`
			}
			entries := map[string]approval{}
			for h, cmds := range hooks {
				entries[h] = approval{Commands: cmds, ApprovedAt: "2026-01-01T00:00:00Z"}
			}
			data, err := json.Marshal(map[string]any{"repos": map[string]any{mainResolved: map[string]any{"hooks": entries}}})
			if err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(filepath.Dir(trustFile), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(trustFile, data, 0o600); err != nil {
				t.Fatal(err)
			}
		}
		marker := func(name string) bool {
			_, err := os.Stat(filepath.Join(repo, name))
			return err == nil
		}
		clearMarkers := func() {
			os.Remove(filepath.Join(repo, "gate-pre.txt"))
			os.Remove(filepath.Join(repo, "gate-post.txt"))
		}
		addAndRemove := func(t *testing.T, branch string) string {
			t.Helper()
			if _, stderr, err := th(t, home, cfg, repo, "add", branch); err != nil {
				t.Fatalf("%v\n%s", err, stderr)
			}
			_, stderr, err := th(t, home, cfg, repo, "remove", branch)
			if err != nil {
				t.Fatalf("%v\n%s", err, stderr)
			}
			return stderr
		}

		// Unapproved commands skip with a warning per hook — without a
		// terminal there is no way to ask, and a committed .thrc must
		// never be able to block removal.
		writeThrc(t, map[string]any{"pre_remove": []string{preCmd}, "post_remove": []string{postCmd}})
		stderr := addAndRemove(t, "hk-gate")
		for _, hook := range []string{"pre_remove", "post_remove"} {
			if !strings.Contains(stderr, hook+" from") || !strings.Contains(stderr, "not approved") {
				t.Errorf("stderr = %q; want a not-approved warning for %s", stderr, hook)
			}
		}
		if marker("gate-pre.txt") || marker("gate-post.txt") {
			t.Error("unapproved repo hooks ran")
		}

		// Seeded per-hook approvals let both run without prompting.
		seedTrust(t, map[string][]string{"pre_remove": {preCmd}, "post_remove": {postCmd}})
		addAndRemove(t, "hk-gate-ok")
		if !marker("gate-pre.txt") || !marker("gate-post.txt") {
			t.Error("approved repo hooks did not run")
		}
		clearMarkers()

		// Changing one hook's commands invalidates only that approval.
		writeThrc(t, map[string]any{"pre_remove": []string{`echo changed > "$TH_MAIN/gate-pre.txt"`}, "post_remove": []string{postCmd}})
		stderr = addAndRemove(t, "hk-gate-changed")
		if !strings.Contains(stderr, "pre_remove from") || !strings.Contains(stderr, "not approved") {
			t.Errorf("stderr = %q; want a not-approved warning after the commands changed", stderr)
		}
		if marker("gate-pre.txt") {
			t.Error("changed pre_remove ran without re-approval")
		}
		if !marker("gate-post.txt") {
			t.Error("unchanged post_remove should still be approved")
		}
		clearMarkers()

		// Approving pre_remove approves nothing else.
		writeThrc(t, map[string]any{"pre_remove": []string{preCmd}, "post_remove": []string{postCmd}})
		seedTrust(t, map[string][]string{"pre_remove": {preCmd}})
		stderr = addAndRemove(t, "hk-gate-partial")
		if !marker("gate-pre.txt") {
			t.Error("approved pre_remove did not run")
		}
		if marker("gate-post.txt") {
			t.Error("post_remove ran on pre_remove's approval")
		}
		if !strings.Contains(stderr, "post_remove from") || !strings.Contains(stderr, "not approved") {
			t.Errorf("stderr = %q; want a not-approved warning for post_remove", stderr)
		}
		clearMarkers()
	})

	t.Run("clean runs the remove hooks per candidate", func(t *testing.T) {
		cfgClean := writeCfg(t, "th-clean-hooks.json",
			`"pre_remove": ["echo \"pre $TH_BRANCH\" >> \"$TH_MAIN/clean-log.txt\""], "post_remove": ["echo \"post $TH_BRANCH\" >> \"$TH_MAIN/clean-log.txt\""]`)
		for _, b := range []string{"cl-a", "cl-b"} {
			// Branches at main's tip count as merged, so th clean picks
			// their worktrees up as candidates.
			git(t, home, repo, "branch", b)
			if _, stderr, err := th(t, home, cfgClean, repo, "add", b); err != nil {
				t.Fatalf("add %s: %v\n%s", b, err, stderr)
			}
		}
		if _, stderr, err := th(t, home, cfgClean, repo, "clean", "--yes"); err != nil {
			t.Fatalf("%v\n%s", err, stderr)
		}
		data, err := os.ReadFile(filepath.Join(repo, "clean-log.txt"))
		if err != nil {
			t.Fatalf("clean did not run the remove hooks: %v", err)
		}
		for _, want := range []string{"pre cl-a", "post cl-a", "pre cl-b", "post cl-b"} {
			if !strings.Contains(string(data), want) {
				t.Errorf("clean-log missing %q:\n%s", want, data)
			}
		}
		for _, b := range []string{"cl-a", "cl-b"} {
			if _, err := os.Stat(filepath.Join(trees, "myapp", b)); !os.IsNotExist(err) {
				t.Errorf("worktree dir for %s still exists", b)
			}
		}

		// A failing pre_remove leaves its candidate standing and fails the
		// clean.
		cfgCleanFail := writeCfg(t, "th-clean-fail.json", `"pre_remove": ["false"]`)
		git(t, home, repo, "branch", "cl-c")
		if _, stderr, err := th(t, home, cfgCleanFail, repo, "add", "cl-c"); err != nil {
			t.Fatalf("%v\n%s", err, stderr)
		}
		if _, _, err := th(t, home, cfgCleanFail, repo, "clean", "--yes"); err == nil {
			t.Fatal("expected clean to fail when pre_remove fails")
		}
		if _, err := os.Stat(filepath.Join(trees, "myapp", "cl-c")); err != nil {
			t.Errorf("candidate must survive a failed pre_remove: %v", err)
		}
		if _, stderr, err := th(t, home, cfgCleanFail, repo, "remove", "--no-pre-remove", "cl-c"); err != nil {
			t.Fatalf("%v\n%s", err, stderr)
		}
	})

	t.Run("multi-remove stops at the first pre_remove failure", func(t *testing.T) {
		cfgMulti := writeCfg(t, "th-multi.json", `"pre_remove": ["test \"$TH_BRANCH\" != mr-b"]`)
		for _, b := range []string{"mr-a", "mr-b", "mr-c"} {
			if _, stderr, err := th(t, home, cfgMulti, repo, "add", b); err != nil {
				t.Fatalf("add %s: %v\n%s", b, err, stderr)
			}
		}
		_, stderr, err := th(t, home, cfgMulti, repo, "remove", "mr-a", "mr-b", "mr-c")
		if err == nil {
			t.Fatal("expected the multi-removal to stop at mr-b's pre_remove failure")
		}
		if !strings.Contains(stderr, "was not removed") {
			t.Errorf("stderr = %q; want the was-not-removed note", stderr)
		}
		if _, err := os.Stat(filepath.Join(trees, "myapp", "mr-a")); !os.IsNotExist(err) {
			t.Error("mr-a should have been removed before the failure")
		}
		for _, b := range []string{"mr-b", "mr-c"} {
			if _, err := os.Stat(filepath.Join(trees, "myapp", b)); err != nil {
				t.Errorf("%s must survive the stopped multi-removal: %v", b, err)
			}
		}
		if _, stderr, err := th(t, home, cfgMulti, repo, "remove", "--no-pre-remove", "mr-b", "mr-c"); err != nil {
			t.Fatalf("%v\n%s", err, stderr)
		}
	})

	t.Run("config --effective shows all four hook rows", func(t *testing.T) {
		cfgEff := filepath.Join(work, "th-hooks-eff.json")
		cfgJSON := `{
  "pre_create": ["true"],
  "pre_remove": ["false"],
  "repos": [{"path": "` + repo + `", "post_remove": ["true"]}]
}`
		if err := os.WriteFile(cfgEff, []byte(cfgJSON), 0o644); err != nil {
			t.Fatal(err)
		}
		out, stderr, err := th(t, home, cfgEff, repo, "config", "--effective")
		if err != nil {
			t.Fatalf("%v\n%s", err, stderr)
		}
		assertEffectiveRow(t, out, "pre_create", `["true"]`, "top-level")
		assertEffectiveRow(t, out, "post_create", "(none)", "default")
		assertEffectiveRow(t, out, "pre_remove", `["false"]`, "top-level")
		assertEffectiveRow(t, out, "post_remove", `["true"]`, "repos[0]")
	})
}

// TestRunCommand exercises th run end to end: where the command runs, the
// TH_* environment, the transparent stdout and exit-code contract, the
// unset-key error, and the trust gate for .thrc-supplied commands — which,
// unlike the lifecycle hooks, errors instead of skipping when it cannot
// ask, because the command is th run's whole job.
func TestRunCommand(t *testing.T) {
	home := t.TempDir()
	work := t.TempDir()

	repo := filepath.Join(work, "myapp")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, home, repo, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(repo, "file.txt"), []byte("hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, home, repo, "add", ".")
	git(t, home, repo, "commit", "-m", "init")
	trees := filepath.Join(work, "trees")

	// git reports symlink-resolved paths (/private/var vs /var on macOS).
	mainResolved, err := filepath.EvalSymlinks(repo)
	if err != nil {
		t.Fatal(err)
	}
	resolve := func(p string) string {
		if r, err := filepath.EvalSymlinks(p); err == nil {
			return r
		}
		return p
	}

	cfg := filepath.Join(work, "th.json")
	if err := os.WriteFile(cfg, []byte(`{"worktree_dir": "`+trees+`/{repo}/{branch}"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	// writeCfg writes a config whose repos entry carries the given extra
	// settings JSON (user-owned, so a run command in it is never gated).
	writeCfg := func(t *testing.T, name, extra string) string {
		t.Helper()
		p := filepath.Join(work, name)
		cfgJSON := `{
  "worktree_dir": "` + trees + `/{repo}/{branch}",
  "repos": [{"path": "` + repo + `", ` + extra + `}]
}`
		if err := os.WriteFile(p, []byte(cfgJSON), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}

	// A linked worktree all the subtests run from.
	wt, stderr, err := th(t, home, cfg, repo, "add", "run-wt")
	if err != nil {
		t.Fatalf("%v\n%s", err, stderr)
	}

	t.Run("runs in the current worktree's root", func(t *testing.T) {
		cfgRun := writeCfg(t, "th-run-pwd.json", `"run": "printf %s \"$PWD\" > ran.txt"`)
		// From a subdirectory, so the test proves the command runs at the
		// worktree root, not in the cwd.
		sub := filepath.Join(wt, "subdir")
		if err := os.MkdirAll(sub, 0o755); err != nil {
			t.Fatal(err)
		}
		_, stderr, err := th(t, home, cfgRun, sub, "run")
		if err != nil {
			t.Fatalf("%v\n%s", err, stderr)
		}
		data, err := os.ReadFile(filepath.Join(wt, "ran.txt"))
		if err != nil {
			t.Fatalf("run did not write at the worktree root: %v", err)
		}
		if got := resolve(strings.TrimSpace(string(data))); got != resolve(wt) {
			t.Errorf("run $PWD = %q; want the worktree root %q", got, resolve(wt))
		}
		if _, err := os.Stat(filepath.Join(repo, "ran.txt")); !os.IsNotExist(err) {
			t.Error("run must execute in the current worktree, not the main one")
		}
		if !strings.Contains(stderr, "run: printf") {
			t.Errorf("stderr = %q; want the run command announced", stderr)
		}
	})

	t.Run("TH_ env vars reach the command", func(t *testing.T) {
		cfgEnv := writeCfg(t, "th-run-env.json", `"run": "echo \"$TH_WORKTREE|$TH_MAIN|$TH_REPO|$TH_BRANCH\" > env.txt"`)
		_, stderr, err := th(t, home, cfgEnv, wt, "run")
		if err != nil {
			t.Fatalf("%v\n%s", err, stderr)
		}
		data, err := os.ReadFile(filepath.Join(wt, "env.txt"))
		if err != nil {
			t.Fatalf("run did not run: %v", err)
		}
		parts := strings.Split(strings.TrimSpace(string(data)), "|")
		if len(parts) != 4 {
			t.Fatalf("env = %q; want 4 |-separated values", data)
		}
		if resolve(parts[0]) != resolve(wt) {
			t.Errorf("TH_WORKTREE = %q; want %q", parts[0], resolve(wt))
		}
		if resolve(parts[1]) != mainResolved {
			t.Errorf("TH_MAIN = %q; want %q", parts[1], mainResolved)
		}
		if parts[2] != "myapp" {
			t.Errorf("TH_REPO = %q; want myapp", parts[2])
		}
		if parts[3] != "run-wt" {
			t.Errorf("TH_BRANCH = %q; want run-wt", parts[3])
		}
	})

	t.Run("child stdout passes through untouched", func(t *testing.T) {
		cfgEcho := writeCfg(t, "th-run-echo.json", `"run": "echo hello"`)
		out, stderr, err := th(t, home, cfgEcho, wt, "run")
		if err != nil {
			t.Fatalf("%v\n%s", err, stderr)
		}
		if out != "hello" {
			t.Errorf("stdout = %q; want only the child's hello", out)
		}
		if !strings.Contains(stderr, "run: echo hello") {
			t.Errorf("stderr = %q; want the announcement on stderr", stderr)
		}
	})

	t.Run("exit code propagates silently", func(t *testing.T) {
		cfgExit := writeCfg(t, "th-run-exit.json", `"run": "exit 7"`)
		out, stderr, err := th(t, home, cfgExit, wt, "run")
		ee, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatalf("err = %v; want the child's exit error", err)
		}
		if ee.ExitCode() != 7 {
			t.Errorf("exit code = %d; want the child's 7", ee.ExitCode())
		}
		if out != "" {
			t.Errorf("stdout = %q; want empty", out)
		}
		if strings.Contains(stderr, "th:") {
			t.Errorf("stderr = %q; the child already reported, no th: line wanted", stderr)
		}
	})

	t.Run("unset run errors", func(t *testing.T) {
		_, stderr, err := th(t, home, cfg, wt, "run")
		if err == nil {
			t.Fatal("expected an error with no run command configured")
		}
		if !strings.Contains(stderr, `"run"`) {
			t.Errorf("stderr = %q; want the run key named", stderr)
		}
	})

	t.Run(".thrc run command is gated", func(t *testing.T) {
		localCfg := filepath.Join(repo, ".thrc")
		trustFile := filepath.Join(home, ".th", "trust.json")
		defer func() {
			os.Remove(localCfg)
			os.Remove(trustFile)
		}()
		gateCmd := `echo ran > "$TH_MAIN/gate.txt"`
		marker := filepath.Join(repo, "gate.txt")
		writeThrc := func(t *testing.T, command string) {
			t.Helper()
			data, err := json.Marshal(map[string]string{"run": command})
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(localCfg, data, 0o644); err != nil {
				t.Fatal(err)
			}
		}

		// Unapproved without a terminal: an error, and nothing ran — for
		// the lifecycle hooks a skip is safe because the surrounding
		// operation still did its job, but here exit 0 would lie.
		writeThrc(t, gateCmd)
		_, stderr, err := th(t, home, cfg, wt, "run")
		if err == nil {
			t.Fatal("unapproved .thrc run must fail, not skip")
		}
		if ee, ok := err.(*exec.ExitError); !ok || ee.ExitCode() != 1 {
			t.Errorf("err = %v; want exit code 1", err)
		}
		if !strings.Contains(stderr, "not approved") || !strings.Contains(stderr, ".thrc") {
			t.Errorf("stderr = %q; want a not-approved error naming the .thrc", stderr)
		}
		if _, err := os.Stat(marker); !os.IsNotExist(err) {
			t.Error("unapproved run command ran")
		}

		// A seeded per-hook approval lets it run without prompting.
		seed := map[string]any{"repos": map[string]any{mainResolved: map[string]any{
			"hooks": map[string]any{"run": map[string]any{"commands": []string{gateCmd}, "approved_at": "2026-01-01T00:00:00Z"}},
		}}}
		data, err := json.Marshal(seed)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Dir(trustFile), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(trustFile, data, 0o600); err != nil {
			t.Fatal(err)
		}
		_, stderr, err = th(t, home, cfg, wt, "run")
		if err != nil {
			t.Fatalf("approved run: %v\n%s", err, stderr)
		}
		if _, err := os.Stat(marker); err != nil {
			t.Errorf("approved run command did not run: %v", err)
		}
		os.Remove(marker)

		// Changing the command invalidates the approval.
		writeThrc(t, `echo changed > "$TH_MAIN/gate.txt"`)
		_, stderr, err = th(t, home, cfg, wt, "run")
		if err == nil {
			t.Fatal("changed .thrc run must need re-approval")
		}
		if !strings.Contains(stderr, "not approved") {
			t.Errorf("stderr = %q; want a not-approved error after the command changed", stderr)
		}
		if _, err := os.Stat(marker); !os.IsNotExist(err) {
			t.Error("changed run command ran without re-approval")
		}
	})

	t.Run("user-config run is not gated", func(t *testing.T) {
		cfgUser := writeCfg(t, "th-run-user.json", `"run": "echo ungated"`)
		out, stderr, err := th(t, home, cfgUser, wt, "run")
		if err != nil {
			t.Fatalf("%v\n%s", err, stderr)
		}
		if out != "ungated" {
			t.Errorf("stdout = %q; want ungated", out)
		}
		if strings.Contains(stderr, "approv") {
			t.Errorf("stderr = %q; a user-owned run must not mention approval", stderr)
		}
		if _, err := os.Stat(filepath.Join(home, ".th", "trust.json")); !os.IsNotExist(err) {
			t.Error("no trust store should exist for a user-owned run")
		}
	})

	t.Run("outside a repository errors", func(t *testing.T) {
		if _, _, err := th(t, home, cfg, work, "run"); err == nil {
			t.Fatal("expected an error outside a repository")
		}
	})

	t.Run("config --effective shows the run row", func(t *testing.T) {
		cfgEff := filepath.Join(work, "th-run-eff.json")
		cfgJSON := `{
  "run": "global-server",
  "repos": [{"path": "` + repo + `", "run": "repo-server"}]
}`
		if err := os.WriteFile(cfgEff, []byte(cfgJSON), 0o644); err != nil {
			t.Fatal(err)
		}
		out, stderr, err := th(t, home, cfgEff, repo, "config", "--effective")
		if err != nil {
			t.Fatalf("%v\n%s", err, stderr)
		}
		assertEffectiveRow(t, out, "run", "repo-server", "repos[0]")

		out, stderr, err = th(t, home, cfg, repo, "config", "--effective")
		if err != nil {
			t.Fatalf("%v\n%s", err, stderr)
		}
		assertEffectiveRow(t, out, "run", "(unset)", "default")
	})
}

// TestConfigSchemaVersion covers the schema-version machinery from outside:
// a file from a newer th stops the commands that need it while the soft-fail
// ones carry on, and th init writes a file at the current schema.
func TestConfigSchemaVersion(t *testing.T) {
	home := t.TempDir()
	work := t.TempDir()

	repo := filepath.Join(work, "myapp")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, home, repo, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(repo, "file.txt"), []byte("hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, home, repo, "add", ".")
	git(t, home, repo, "commit", "-m", "init")

	trees := filepath.Join(work, "trees")
	cfg := filepath.Join(work, "th.json")
	// Stamped at the current schema so the shared config is never the one
	// being migrated: the backup scan below wants an empty work dir.
	if err := os.WriteFile(cfg, []byte(`{"version": 2, "worktree_dir": "`+trees+`/{repo}/{branch}"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	thrc := filepath.Join(repo, ".thrc")

	t.Run("a .thrc from a newer th stops add but not list", func(t *testing.T) {
		local := []byte(`{"version": 99, "name": "x"}`)
		if err := os.WriteFile(thrc, local, 0o644); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.Remove(thrc) })

		_, stderr, err := th(t, home, cfg, repo, "add", "too-new")
		if err == nil {
			t.Fatal("add should refuse a .thrc written by a newer th")
		}
		if !strings.Contains(stderr, "upgrade th") || !strings.Contains(stderr, "99") {
			t.Errorf("stderr = %q; want the version and an upgrade-th hint", stderr)
		}

		// Soft-fail commands swallow it like any other config error.
		out, stderr, err := th(t, home, cfg, repo, "list")
		if err != nil {
			t.Fatalf("list must survive a too-new .thrc: %v\n%s", err, stderr)
		}
		if !strings.Contains(out, "main") {
			t.Errorf("list stdout = %q; want the main worktree listed", out)
		}

		if got, err := os.ReadFile(thrc); err != nil || string(got) != string(local) {
			t.Errorf(".thrc = %s (err %v); want it untouched", got, err)
		}
	})

	t.Run("a config.json from a newer th stops add but not list", func(t *testing.T) {
		newer := filepath.Join(work, "th-too-new.json")
		global := []byte(`{"version": 99, "worktree_dir": "` + trees + `/{repo}/{branch}"}`)
		if err := os.WriteFile(newer, global, 0o644); err != nil {
			t.Fatal(err)
		}

		_, stderr, err := th(t, home, newer, repo, "add", "too-new-global")
		if err == nil {
			t.Fatal("add should refuse a config written by a newer th")
		}
		if !strings.Contains(stderr, "upgrade th") || !strings.Contains(stderr, "99") {
			t.Errorf("stderr = %q; want the version and an upgrade-th hint", stderr)
		}

		out, stderr, err := th(t, home, newer, repo, "list")
		if err != nil {
			t.Fatalf("list must survive a too-new config: %v\n%s", err, stderr)
		}
		if !strings.Contains(out, "main") {
			t.Errorf("list stdout = %q; want the main worktree listed", out)
		}

		// A file th refuses to read is a file th must not rewrite.
		if got, err := os.ReadFile(newer); err != nil || string(got) != string(global) {
			t.Errorf("config = %s (err %v); want it untouched", got, err)
		}
		if entries, err := os.ReadDir(work); err == nil {
			for _, e := range entries {
				if strings.HasSuffix(e.Name(), ".bak") {
					t.Errorf("a failed load wrote a backup: %s", e.Name())
				}
			}
		}
	})

	t.Run("init scaffolds the current schema and commands accept it", func(t *testing.T) {
		if _, stderr, err := th(t, home, cfg, repo, "init"); err != nil {
			t.Fatalf("%v\n%s", err, stderr)
		}
		t.Cleanup(func() { os.Remove(thrc) })
		data, err := os.ReadFile(thrc)
		if err != nil {
			t.Fatal(err)
		}
		if want := fmt.Sprintf(`"version": %d`, config.CurrentLocalVersion()); !strings.Contains(string(data), want) {
			t.Errorf(".thrc = %s; want the current schema version stamped", data)
		}

		// A file already at the current schema is used as-is: no prompt to
		// skip, no note on stderr, no rewrite.
		wt, stderr, err := th(t, home, cfg, repo, "add", "scaffolded")
		if err != nil {
			t.Fatalf("add against the scaffolded .thrc: %v\n%s", err, stderr)
		}
		if strings.Contains(stderr, "schema") {
			t.Errorf("stderr = %q; want nothing said about the schema", stderr)
		}
		if got, err := os.ReadFile(thrc); err != nil || string(got) != string(data) {
			t.Errorf(".thrc = %s (err %v); want it unchanged by th add", got, err)
		}
		if _, err := os.Stat(wt); err != nil {
			t.Errorf("worktree %s: %v", wt, err)
		}
	})

	t.Run("a v1 config.json is migrated and rewritten in place", func(t *testing.T) {
		old := filepath.Join(t.TempDir(), "cfg.json")
		original := []byte(`{
  "worktree_dir": "` + trees + `/{repo}/{branch}",
  "vscode_window_color": "auto",
  "repos": [{"path": "` + repo + `", "vscode_workspace_file": true, "vscode_workspace_prefix": "acs-"}]
}`)
		if err := os.WriteFile(old, original, 0o644); err != nil {
			t.Fatal(err)
		}

		if _, stderr, err := th(t, home, old, repo, "config"); err != nil {
			t.Fatalf("%v\n%s", err, stderr)
		}

		data, err := os.ReadFile(old)
		if err != nil {
			t.Fatal(err)
		}
		var got struct {
			Version int            `json:"version"`
			VSCode  map[string]any `json:"vscode"`
			Repos   []struct {
				VSCode map[string]any `json:"vscode"`
			} `json:"repos"`
		}
		if err := json.Unmarshal(data, &got); err != nil {
			t.Fatalf("migrated config is not valid JSON: %v\n%s", err, data)
		}
		if got.Version != 2 {
			t.Errorf("version = %d; want 2\n%s", got.Version, data)
		}
		if got.VSCode["window_color"] != "auto" {
			t.Errorf("vscode = %v; want window_color auto nested at the top level\n%s", got.VSCode, data)
		}
		if len(got.Repos) != 1 || got.Repos[0].VSCode["workspace_file"] != true || got.Repos[0].VSCode["workspace_prefix"] != "acs-" {
			t.Errorf("repos = %+v; want the entry's keys nested too\n%s", got.Repos, data)
		}
		for _, flat := range []string{"vscode_window_color", "vscode_workspace_file", "vscode_workspace_prefix"} {
			if strings.Contains(string(data), flat) {
				t.Errorf("migrated config still has the flat key %q:\n%s", flat, data)
			}
		}

		// Exactly one backup, holding the file as it was.
		entries, err := os.ReadDir(filepath.Dir(old))
		if err != nil {
			t.Fatal(err)
		}
		var baks []string
		for _, e := range entries {
			if strings.HasSuffix(e.Name(), ".bak") {
				baks = append(baks, e.Name())
			}
		}
		if len(baks) != 1 || !strings.HasPrefix(baks[0], "cfg.json.v1.") {
			t.Fatalf("backups = %v; want exactly one cfg.json.v1.<timestamp>.bak", baks)
		}
		if bak, err := os.ReadFile(filepath.Join(filepath.Dir(old), baks[0])); err != nil || string(bak) != string(original) {
			t.Errorf("backup = %s (err %v); want the original bytes", bak, err)
		}
	})

	t.Run("a v1 .thrc is used as v2 without touching the file", func(t *testing.T) {
		local := []byte(`{"vscode_workspace_file": true, "vscode_window_color": "#aabbcc"}`)
		if err := os.WriteFile(thrc, local, 0o644); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.Remove(thrc) })

		wt, stderr, err := th(t, home, cfg, repo, "add", "nested-b")
		if err != nil {
			t.Fatalf("%v\n%s", err, stderr)
		}
		if !strings.Contains(stderr, "uses config schema v1") {
			t.Errorf("stderr = %q; want the in-memory migration note", stderr)
		}

		// The nested settings took effect: the workspace file carries the
		// configured color.
		data, err := os.ReadFile(filepath.Join(filepath.Dir(wt), "nested-b.code-workspace"))
		if err != nil {
			t.Fatalf("workspace file not written: %v", err)
		}
		var ws struct {
			Settings map[string]any `json:"settings"`
		}
		if err := json.Unmarshal(data, &ws); err != nil {
			t.Fatal(err)
		}
		cc, _ := ws.Settings["workbench.colorCustomizations"].(map[string]any)
		if got, _ := cc["titleBar.activeBackground"].(string); got != "#aabbcc" {
			t.Errorf("titleBar.activeBackground = %q; want #aabbcc from the migrated .thrc\n%s", got, data)
		}

		// Non-TTY: the migration stays in memory, so the file is untouched
		// and no backup is taken.
		if got, err := os.ReadFile(thrc); err != nil || string(got) != string(local) {
			t.Errorf(".thrc = %s (err %v); want it untouched", got, err)
		}
		entries, err := os.ReadDir(repo)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			if strings.HasSuffix(e.Name(), ".bak") {
				t.Errorf("an in-memory migration wrote a backup: %s", e.Name())
			}
		}
	})
}

// bakFiles lists the migration backups sitting in dir: the evidence of
// whether a th run kept a copy of a config file before rewriting it.
func bakFiles(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var baks []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".bak") {
			baks = append(baks, e.Name())
		}
	}
	return baks
}

// TestMigrateCommand covers th migrate, the explicit counterpart to the
// passive migration: what it reports, which flags a run without a terminal
// needs, and — the load-bearing one — that it inspects the global config
// without going through the load that would rewrite it behind the user's
// back.
func TestMigrateCommand(t *testing.T) {
	home := t.TempDir()
	work := t.TempDir()

	repo := filepath.Join(work, "myapp")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, home, repo, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(repo, "file.txt"), []byte("hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, home, repo, "add", ".")
	git(t, home, repo, "commit", "-m", "init")

	trees := filepath.Join(work, "trees")
	cfg := filepath.Join(work, "th.json")
	// Stamped at the current schema so the shared config is never the one
	// being migrated: the subtests that watch a global file bring their own.
	if err := os.WriteFile(cfg, []byte(`{"version": 2, "worktree_dir": "`+trees+`/{repo}/{branch}"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	thrc := filepath.Join(repo, ".thrc")

	// The real v1 fixtures: the shipped v1-to-v2 steps nest these flat
	// vscode_* keys, so the diffs and rewrites below are the genuine article.
	localV1 := []byte(`{"vscode_workspace_file": true}`)
	globalV1 := []byte(`{
  "worktree_dir": "` + trees + `/{repo}/{branch}",
  "vscode_window_color": "auto"
}
`)

	// Each subtest starts from a fresh v1 .thrc and takes its backups with
	// it, so a backup scan only ever sees what that subtest produced.
	writeLocal := func(t *testing.T, content []byte) {
		t.Helper()
		if err := os.WriteFile(thrc, content, 0o644); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			os.Remove(thrc)
			for _, b := range bakFiles(t, repo) {
				os.Remove(filepath.Join(repo, b))
			}
		})
	}
	// A v1 global config in a directory of its own, so its backups are as
	// easy to count as the repo's.
	writeGlobal := func(t *testing.T) string {
		t.Helper()
		path := filepath.Join(t.TempDir(), "config.json")
		if err := os.WriteFile(path, globalV1, 0o644); err != nil {
			t.Fatal(err)
		}
		return path
	}
	assertUnchanged := func(t *testing.T, path string, want []byte) {
		t.Helper()
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != string(want) {
			t.Errorf("%s = %s; want it untouched", filepath.Base(path), got)
		}
	}
	// assertLocalMigrated checks the .thrc came out nested at v2, the shape
	// the shipped step produces.
	assertLocalMigrated := func(t *testing.T) {
		t.Helper()
		data, err := os.ReadFile(thrc)
		if err != nil {
			t.Fatal(err)
		}
		var got struct {
			Version int            `json:"version"`
			VSCode  map[string]any `json:"vscode"`
		}
		if err := json.Unmarshal(data, &got); err != nil {
			t.Fatalf("migrated .thrc is not valid JSON: %v\n%s", err, data)
		}
		if got.Version != 2 || got.VSCode["workspace_file"] != true {
			t.Errorf(".thrc = %s; want version 2 with workspace_file nested under vscode", data)
		}
		if strings.Contains(string(data), "vscode_workspace_file") {
			t.Errorf("migrated .thrc still has the flat key:\n%s", data)
		}
	}

	t.Run("dry run reports the rewrite and changes nothing", func(t *testing.T) {
		writeLocal(t, localV1)

		out, stderr, err := th(t, home, cfg, repo, "migrate", "--dry-run")
		if err != nil {
			t.Fatalf("%v\n%s", err, stderr)
		}
		if out != "" {
			t.Errorf("stdout = %q; want th migrate to report on stderr only", out)
		}
		for _, want := range []string{"v1 -> v2", "(dry run)", ".thrc.v1.<timestamp>.bak"} {
			if !strings.Contains(stderr, want) {
				t.Errorf("stderr = %q; want it to mention %q", stderr, want)
			}
		}
		added := false
		for _, line := range strings.Split(stderr, "\n") {
			if strings.HasPrefix(line, "  + ") && strings.Contains(line, `"vscode"`) {
				added = true
			}
		}
		if !added {
			t.Errorf("stderr = %q; want a diff line adding the nested vscode object", stderr)
		}

		assertUnchanged(t, thrc, localV1)
		if baks := bakFiles(t, repo); len(baks) != 0 {
			t.Errorf("a dry run wrote backups: %v", baks)
		}
	})

	t.Run("without a terminal it names the flags it needs", func(t *testing.T) {
		writeLocal(t, localV1)

		_, stderr, err := th(t, home, cfg, repo, "migrate")
		if err == nil {
			t.Fatal("migrate should refuse to answer its own prompts without a terminal")
		}
		for _, want := range []string{"--yes", "--backup or --no-backup", "--dry-run"} {
			if !strings.Contains(stderr, want) {
				t.Errorf("stderr = %q; want it to mention %q", stderr, want)
			}
		}

		assertUnchanged(t, thrc, localV1)
		if baks := bakFiles(t, repo); len(baks) != 0 {
			t.Errorf("a refused migration wrote backups: %v", baks)
		}
	})

	t.Run("--yes alone still needs the backup decision", func(t *testing.T) {
		writeLocal(t, localV1)

		_, stderr, err := th(t, home, cfg, repo, "migrate", "--yes")
		if err == nil {
			t.Fatal("migrate --yes should still ask about the backup")
		}
		if !strings.Contains(stderr, "--backup or --no-backup") {
			t.Errorf("stderr = %q; want it to ask for the backup flags", stderr)
		}
		// The satisfied clause is dropped: --yes was given.
		if strings.Contains(stderr, "--yes to confirm") {
			t.Errorf("stderr = %q; want no mention of the answer that was given", stderr)
		}

		assertUnchanged(t, thrc, localV1)
	})

	t.Run("--yes --backup updates the file and keeps a copy", func(t *testing.T) {
		writeLocal(t, localV1)

		if _, stderr, err := th(t, home, cfg, repo, "migrate", "--yes", "--backup"); err != nil {
			t.Fatalf("%v\n%s", err, stderr)
		} else if !strings.Contains(stderr, "Updated") || !strings.Contains(stderr, "backup:") {
			t.Errorf("stderr = %q; want the update reported with its backup", stderr)
		}

		assertLocalMigrated(t)
		baks := bakFiles(t, repo)
		if len(baks) != 1 || !strings.HasPrefix(baks[0], ".thrc.v1.") {
			t.Fatalf("backups = %v; want exactly one .thrc.v1.<timestamp>.bak", baks)
		}
		if bak, err := os.ReadFile(filepath.Join(repo, baks[0])); err != nil || string(bak) != string(localV1) {
			t.Errorf("backup = %s (err %v); want the original bytes", bak, err)
		}
	})

	t.Run("--yes --no-backup updates the file without a copy", func(t *testing.T) {
		writeLocal(t, localV1)

		if _, stderr, err := th(t, home, cfg, repo, "migrate", "--yes", "--no-backup"); err != nil {
			t.Fatalf("%v\n%s", err, stderr)
		}

		assertLocalMigrated(t)
		if baks := bakFiles(t, repo); len(baks) != 0 {
			t.Errorf("--no-backup wrote backups: %v", baks)
		}
	})

	t.Run("--backup and --no-backup are mutually exclusive", func(t *testing.T) {
		writeLocal(t, localV1)

		_, stderr, err := th(t, home, cfg, repo, "migrate", "--yes", "--backup", "--no-backup")
		if err == nil {
			t.Fatal("migrate should refuse both backup flags at once")
		}
		if !strings.Contains(stderr, "none of the others can be") || !strings.Contains(stderr, "no-backup") {
			t.Errorf("stderr = %q; want cobra's mutually-exclusive-flags error", stderr)
		}

		assertUnchanged(t, thrc, localV1)
	})

	t.Run("an up-to-date .thrc needs no answers", func(t *testing.T) {
		current := []byte(`{"version": 2, "name": "myapp"}`)
		writeLocal(t, current)

		// No flags, no terminal, and still exit 0: there is nothing to
		// confirm.
		if _, stderr, err := th(t, home, cfg, repo, "migrate"); err != nil {
			t.Fatalf("%v\n%s", err, stderr)
		} else if !strings.Contains(stderr, "already at config schema v2") {
			t.Errorf("stderr = %q; want the up-to-date report", stderr)
		}

		assertUnchanged(t, thrc, current)
	})

	t.Run("a repository without a .thrc has nothing to migrate", func(t *testing.T) {
		if _, err := os.Stat(thrc); err == nil {
			t.Fatalf("%s outlived the subtest that wrote it", thrc)
		}

		if _, stderr, err := th(t, home, cfg, repo, "migrate"); err != nil {
			t.Fatalf("%v\n%s", err, stderr)
		} else if !strings.Contains(stderr, "no .thrc") || !strings.Contains(stderr, "nothing to migrate") {
			t.Errorf("stderr = %q; want the no-.thrc report", stderr)
		}
	})

	t.Run("a .thrc from a newer th is an error, dry run included", func(t *testing.T) {
		tooNew := []byte(`{"version": 99, "name": "x"}`)
		writeLocal(t, tooNew)

		for _, args := range [][]string{
			{"migrate", "--yes", "--backup"},
			{"migrate", "--dry-run"},
		} {
			_, stderr, err := th(t, home, cfg, repo, args...)
			if err == nil {
				t.Fatalf("th %v should refuse a .thrc written by a newer th", args)
			}
			if !strings.Contains(stderr, "upgrade th") || !strings.Contains(stderr, "99") {
				t.Errorf("th %v stderr = %q; want the version and an upgrade-th hint", args, stderr)
			}
		}

		assertUnchanged(t, thrc, tooNew)
		if baks := bakFiles(t, repo); len(baks) != 0 {
			t.Errorf("a refused migration wrote backups: %v", baks)
		}
	})

	t.Run("--global updates both files, global first", func(t *testing.T) {
		writeLocal(t, localV1)
		global := writeGlobal(t)

		_, stderr, err := th(t, home, global, repo, "migrate", "--global", "--yes", "--backup")
		if err != nil {
			t.Fatalf("%v\n%s", err, stderr)
		}

		var updated []string
		for _, line := range strings.Split(stderr, "\n") {
			if strings.HasPrefix(line, "Updated ") {
				updated = append(updated, line)
			}
		}
		if len(updated) != 2 {
			t.Fatalf("update lines = %v; want one per file\n%s", updated, stderr)
		}
		// The global config is the layer a .thrc overrides, so it goes first.
		if !strings.Contains(updated[0], "config.json") || !strings.Contains(updated[1], ".thrc") {
			t.Errorf("update lines = %v; want the global config reported first", updated)
		}

		assertLocalMigrated(t)
		data, err := os.ReadFile(global)
		if err != nil {
			t.Fatal(err)
		}
		var got struct {
			Version int            `json:"version"`
			VSCode  map[string]any `json:"vscode"`
		}
		if err := json.Unmarshal(data, &got); err != nil {
			t.Fatalf("migrated config is not valid JSON: %v\n%s", err, data)
		}
		if got.Version != 2 || got.VSCode["window_color"] != "auto" {
			t.Errorf("config = %s; want version 2 with window_color nested under vscode", data)
		}

		if baks := bakFiles(t, repo); len(baks) != 1 {
			t.Errorf(".thrc backups = %v; want exactly one", baks)
		}
		if baks := bakFiles(t, filepath.Dir(global)); len(baks) != 1 || !strings.HasPrefix(baks[0], "config.json.v1.") {
			t.Errorf("config backups = %v; want exactly one config.json.v1.<timestamp>.bak", baks)
		}
	})

	t.Run("--global --dry-run touches neither file", func(t *testing.T) {
		writeLocal(t, localV1)
		global := writeGlobal(t)

		out, stderr, err := th(t, home, global, repo, "migrate", "--global", "--dry-run")
		if err != nil {
			t.Fatalf("%v\n%s", err, stderr)
		}
		if out != "" {
			t.Errorf("stdout = %q; want th migrate to report on stderr only", out)
		}
		if !strings.Contains(stderr, "config.json") || !strings.Contains(stderr, ".thrc") {
			t.Errorf("stderr = %q; want both files previewed", stderr)
		}

		// The proof that th migrate inspects the global config instead of
		// loading it: loading would have migrated and rewritten it here.
		assertUnchanged(t, global, globalV1)
		assertUnchanged(t, thrc, localV1)
		if baks := bakFiles(t, filepath.Dir(global)); len(baks) != 0 {
			t.Errorf("a dry run backed up the global config: %v", baks)
		}
		if baks := bakFiles(t, repo); len(baks) != 0 {
			t.Errorf("a dry run backed up the .thrc: %v", baks)
		}
	})

	t.Run("--no-backup does not apply to the global config", func(t *testing.T) {
		writeLocal(t, localV1)
		global := writeGlobal(t)

		if _, stderr, err := th(t, home, global, repo, "migrate", "--global", "--yes", "--no-backup"); err != nil {
			t.Fatalf("%v\n%s", err, stderr)
		}

		assertLocalMigrated(t)
		if baks := bakFiles(t, repo); len(baks) != 0 {
			t.Errorf("--no-backup wrote a .thrc backup: %v", baks)
		}
		// The global file is backed up whenever th rewrites it, exactly as
		// it is when a load migrates it.
		if baks := bakFiles(t, filepath.Dir(global)); len(baks) != 1 {
			t.Errorf("config backups = %v; want the global config backed up anyway", baks)
		}
	})

	t.Run("--global outside a repository migrates the global config only", func(t *testing.T) {
		global := writeGlobal(t)
		outside := t.TempDir()

		_, stderr, err := th(t, home, global, outside, "migrate", "--global", "--yes")
		if err != nil {
			t.Fatalf("%v\n%s", err, stderr)
		}
		if !strings.Contains(stderr, "not inside a git repository") {
			t.Errorf("stderr = %q; want the note about the missing repository", stderr)
		}
		if !strings.Contains(stderr, "Updated") {
			t.Errorf("stderr = %q; want the global config reported as updated", stderr)
		}

		data, err := os.ReadFile(global)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(data), `"version": 2`) {
			t.Errorf("config = %s; want it migrated to v2", data)
		}
		if baks := bakFiles(t, filepath.Dir(global)); len(baks) != 1 {
			t.Errorf("config backups = %v; want exactly one", baks)
		}
	})
}
