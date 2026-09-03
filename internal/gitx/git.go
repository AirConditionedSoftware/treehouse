// Package gitx shells out to the git binary. It deliberately avoids go-git:
// its worktree support is incomplete, while `git worktree list --porcelain`
// is a stable scripting interface.
package gitx

import (
	"bytes"
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// Run executes git with args in dir and returns stdout with the trailing
// newline removed. On failure the error carries git's stderr.
func Run(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), msg)
	}
	return strings.TrimRight(stdout.String(), "\n"), nil
}

// Toplevel returns the root of the worktree containing dir.
func Toplevel(dir string) (string, error) {
	return Run(dir, "rev-parse", "--show-toplevel")
}

// HooksDir returns the absolute path of the effective hooks directory for
// the worktree at dir, honoring core.hooksPath.
func HooksDir(dir string) (string, error) {
	out, err := Run(dir, "rev-parse", "--git-path", "hooks")
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(out) {
		out = filepath.Join(dir, out)
	}
	return filepath.Abs(out)
}

// LocalBranchExists reports whether refs/heads/<branch> exists.
func LocalBranchExists(dir, branch string) bool {
	_, err := Run(dir, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch)
	return err == nil
}

// IsDirty reports whether the worktree at dir has modified or untracked
// files.
func IsDirty(dir string) (bool, error) {
	out, err := Run(dir, "status", "--porcelain")
	if err != nil {
		return false, err
	}
	return out != "", nil
}

// DefaultBranch returns the repository's default branch: origin's HEAD when
// set, else main or master when one exists locally, else "main".
func DefaultBranch(dir string) string {
	if out, err := Run(dir, "symbolic-ref", "--short", "refs/remotes/origin/HEAD"); err == nil {
		if b, ok := strings.CutPrefix(out, "origin/"); ok && b != "" {
			return b
		}
	}
	for _, b := range []string{"main", "master"} {
		if LocalBranchExists(dir, b) {
			return b
		}
	}
	return "main"
}

// ChangeCount returns the number of pending files in the worktree at dir:
// staged, unstaged, and untracked.
func ChangeCount(dir string) (int, error) {
	out, err := Run(dir, "status", "--porcelain")
	if err != nil {
		return 0, err
	}
	if out == "" {
		return 0, nil
	}
	return len(strings.Split(out, "\n")), nil
}

// IsAncestor reports whether commit is reachable from ref — i.e. merged.
func IsAncestor(dir, commit, ref string) bool {
	_, err := Run(dir, "merge-base", "--is-ancestor", commit, ref)
	return err == nil
}

// CommitInfo describes a commit for display.
type CommitInfo struct {
	ShortHash string
	When      string // relative, e.g. "3 days ago"
	Subject   string
}

// CommitInfos returns display info for each sha, keyed by full sha.
func CommitInfos(dir string, shas []string) (map[string]CommitInfo, error) {
	infos := make(map[string]CommitInfo, len(shas))
	if len(shas) == 0 {
		return infos, nil
	}
	args := append([]string{"show", "-s", "--format=%H%x00%h%x00%cr%x00%s"}, shas...)
	out, err := Run(dir, args...)
	if err != nil {
		return nil, err
	}
	for line := range strings.SplitSeq(out, "\n") {
		parts := strings.SplitN(line, "\x00", 4)
		if len(parts) != 4 {
			continue
		}
		infos[parts[0]] = CommitInfo{ShortHash: parts[1], When: parts[2], Subject: parts[3]}
	}
	return infos, nil
}

// fetchedOrigin caps RemoteBranchExists at one fetch per process, so probing
// several candidate branch names doesn't hit the network repeatedly.
var fetchedOrigin bool

// RemoteBranchExists reports whether origin has branch, fetching once if the
// remote-tracking ref is not yet known locally. A failed fetch (e.g. no
// origin remote) just means the branch is treated as not existing remotely.
func RemoteBranchExists(dir, branch string) bool {
	ref := "refs/remotes/origin/" + branch
	if _, err := Run(dir, "rev-parse", "--verify", "--quiet", ref); err == nil {
		return true
	}
	if fetchedOrigin {
		return false
	}
	fetchedOrigin = true
	if _, err := Run(dir, "fetch", "--quiet", "origin"); err != nil {
		return false
	}
	_, err := Run(dir, "rev-parse", "--verify", "--quiet", ref)
	return err == nil
}

// AheadBehind counts the commits branch has that its upstream lacks (ahead,
// to push) and vice versa (behind, to pull). ok is false when there is no
// usable upstream or the counts cannot be computed — e.g. no upstream
// configured, or the upstream's remote-tracking ref was pruned away.
func AheadBehind(dir, branch string) (ahead, behind int, ok bool) {
	out, err := Run(dir, "rev-list", "--left-right", "--count",
		"refs/heads/"+branch+"..."+branch+"@{upstream}")
	if err != nil {
		return 0, 0, false
	}
	counts := strings.Fields(out)
	if len(counts) != 2 {
		return 0, 0, false
	}
	ahead, aerr := strconv.Atoi(counts[0])
	behind, berr := strconv.Atoi(counts[1])
	if aerr != nil || berr != nil {
		return 0, 0, false
	}
	return ahead, behind, true
}

// UpstreamGone reports whether branch has an upstream configured whose
// remote-tracking ref no longer exists — the branch was deleted on the
// remote. Callers that want fresh knowledge fetch with --prune first;
// without that, a stale remote-tracking ref hides the deletion.
func UpstreamGone(dir, branch string) bool {
	out, err := Run(dir, "for-each-ref", "--format=%(upstream:track)", "refs/heads/"+branch)
	return err == nil && out == "[gone]"
}

// TrackedFiles returns the set of paths git tracks in the worktree at dir,
// relative to its root, as reported by ls-files.
func TrackedFiles(dir string) (map[string]bool, error) {
	out, err := Run(dir, "ls-files", "-z")
	if err != nil {
		return nil, err
	}
	tracked := make(map[string]bool)
	for _, p := range strings.Split(out, "\x00") {
		if p != "" {
			tracked[p] = true
		}
	}
	return tracked, nil
}
