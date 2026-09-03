package gitx

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// gitRun executes git in dir with an environment isolated from the
// developer's real global/system config (HOME pinned to home), failing the
// test on error, and returns the trimmed output.
func gitRun(t *testing.T, home, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"HOME="+home,
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_AUTHOR_NAME=th-test",
		"GIT_AUTHOR_EMAIL=th@test.invalid",
		"GIT_COMMITTER_NAME=th-test",
		"GIT_COMMITTER_EMAIL=th@test.invalid",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func testRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	gitRun(t, dir, dir, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, dir, "add", ".")
	gitRun(t, dir, dir, "commit", "-m", "first commit")
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, dir, "add", ".")
	gitRun(t, dir, dir, "commit", "-m", "second commit with a longer subject line")
	return dir
}

func TestCommitInfos(t *testing.T) {
	dir := testRepo(t)
	head, err := Run(dir, "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	prev, err := Run(dir, "rev-parse", "HEAD~1")
	if err != nil {
		t.Fatal(err)
	}

	infos, err := CommitInfos(dir, []string{head, prev, head})
	if err != nil {
		t.Fatal(err)
	}
	if len(infos) != 2 {
		t.Fatalf("got %d infos; want 2 (duplicates collapsed): %+v", len(infos), infos)
	}
	for sha, wantSubject := range map[string]string{
		head: "second commit with a longer subject line",
		prev: "first commit",
	} {
		info, ok := infos[sha]
		if !ok {
			t.Fatalf("no info for %s", sha)
		}
		if info.Subject != wantSubject {
			t.Errorf("subject for %s = %q; want %q", sha, info.Subject, wantSubject)
		}
		if !strings.HasPrefix(sha, info.ShortHash) || info.ShortHash == "" {
			t.Errorf("short hash %q is not a prefix of %s", info.ShortHash, sha)
		}
		if info.When == "" {
			t.Errorf("empty relative date for %s", sha)
		}
	}
}

func TestCommitInfosEmpty(t *testing.T) {
	infos, err := CommitInfos(t.TempDir(), nil)
	if err != nil || len(infos) != 0 {
		t.Errorf("CommitInfos with no shas = %v, %v; want empty map, nil", infos, err)
	}
}

func TestCommitInfosBadSha(t *testing.T) {
	dir := testRepo(t)
	if _, err := CommitInfos(dir, []string{"0000000000000000000000000000000000000000"}); err == nil {
		t.Error("CommitInfos with unknown sha should fail")
	}
}

func TestDefaultBranch(t *testing.T) {
	dir := testRepo(t)
	if got := DefaultBranch(dir); got != "main" {
		t.Errorf("DefaultBranch = %q; want main", got)
	}
}

func TestChangeCount(t *testing.T) {
	dir := testRepo(t)
	n, err := ChangeCount(dir)
	if err != nil || n != 0 {
		t.Errorf("ChangeCount clean = %d, %v; want 0", n, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "new.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	n, err = ChangeCount(dir)
	if err != nil || n != 2 {
		t.Errorf("ChangeCount with untracked+modified = %d, %v; want 2", n, err)
	}
}

func TestIsAncestor(t *testing.T) {
	dir := testRepo(t)
	head, _ := Run(dir, "rev-parse", "HEAD")
	prev, _ := Run(dir, "rev-parse", "HEAD~1")
	if !IsAncestor(dir, prev, head) {
		t.Error("HEAD~1 should be an ancestor of HEAD")
	}
	if IsAncestor(dir, head, prev) {
		t.Error("HEAD must not be an ancestor of HEAD~1")
	}
}

// cloneFixture builds a src repo with one commit on main and a clone of it
// whose main tracks origin/main, both under a shared isolated home.
func cloneFixture(t *testing.T) (home, src, clone string) {
	t.Helper()
	home = t.TempDir()
	src = filepath.Join(home, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	gitRun(t, home, src, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(src, "f.txt"), []byte("1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, home, src, "add", ".")
	gitRun(t, home, src, "commit", "-m", "base")
	gitRun(t, home, home, "clone", src, "clone")
	return home, src, filepath.Join(home, "clone")
}

func TestAheadBehind(t *testing.T) {
	home, src, clone := cloneFixture(t)

	if a, b, ok := AheadBehind(clone, "main"); !ok || a != 0 || b != 0 {
		t.Errorf("in sync = %d, %d, %v; want 0, 0, true", a, b, ok)
	}

	// A local commit the upstream lacks.
	if err := os.WriteFile(filepath.Join(clone, "local.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, home, clone, "add", ".")
	gitRun(t, home, clone, "commit", "-m", "local")
	if a, b, ok := AheadBehind(clone, "main"); !ok || a != 1 || b != 0 {
		t.Errorf("after a local commit = %d, %d, %v; want 1, 0, true", a, b, ok)
	}

	// A commit on origin, fetched: ahead and behind at once.
	if err := os.WriteFile(filepath.Join(src, "remote.txt"), []byte("y\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, home, src, "add", ".")
	gitRun(t, home, src, "commit", "-m", "remote")
	gitRun(t, home, clone, "fetch", "origin")
	if a, b, ok := AheadBehind(clone, "main"); !ok || a != 1 || b != 1 {
		t.Errorf("after diverging = %d, %d, %v; want 1, 1, true", a, b, ok)
	}

	// A branch without an upstream has no counts.
	gitRun(t, home, clone, "branch", "no-upstream")
	if _, _, ok := AheadBehind(clone, "no-upstream"); ok {
		t.Error("AheadBehind without an upstream should report ok=false")
	}
}

func TestUpstreamGone(t *testing.T) {
	home, src, clone := cloneFixture(t)
	gitRun(t, home, src, "branch", "topic")
	gitRun(t, home, clone, "fetch", "origin")
	gitRun(t, home, clone, "branch", "--track", "topic", "origin/topic")

	if UpstreamGone(clone, "topic") {
		t.Error("UpstreamGone with a live upstream should be false")
	}
	gitRun(t, home, clone, "branch", "no-upstream")
	if UpstreamGone(clone, "no-upstream") {
		t.Error("UpstreamGone without an upstream should be false")
	}

	gitRun(t, home, src, "branch", "-D", "topic")
	gitRun(t, home, clone, "fetch", "--prune", "origin")
	if !UpstreamGone(clone, "topic") {
		t.Error("UpstreamGone after deletion on the remote plus prune should be true")
	}
	if _, _, ok := AheadBehind(clone, "topic"); ok {
		t.Error("AheadBehind with a gone upstream should report ok=false")
	}
}

func TestTrackedFiles(t *testing.T) {
	dir := testRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "untracked.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sub", "with space.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "add", "sub")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git add: %v\n%s", err, out)
	}

	tracked, err := TrackedFiles(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !tracked["f.txt"] {
		t.Error("committed f.txt should be tracked")
	}
	// -z output must survive names with spaces unmangled.
	if !tracked["sub/with space.txt"] {
		t.Errorf("staged sub/with space.txt should be tracked; got %v", tracked)
	}
	if tracked["untracked.txt"] {
		t.Error("untracked.txt must not be tracked")
	}
}
