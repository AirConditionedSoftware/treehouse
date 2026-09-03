package cmd

import (
	"path/filepath"
	"strconv"
	"strings"

	"github.com/AirConditionedSoftware/treehouse/internal/gitx"
)

// worktreeFacts are the live-state details the rich entry format shows.
type worktreeFacts struct {
	changes      int // pending files: staged, unstaged, and untracked
	changesOK    bool
	mergeKnown   bool // merge status applies (not the default branch) and was computable
	merged       bool
	ahead        int  // commits the branch has that its upstream lacks
	behind       int  // commits the upstream has that the branch lacks
	syncKnown    bool // a live upstream exists and the counts were computable
	upstreamGone bool // an upstream is configured but was deleted on the remote
}

// gatherFacts runs the per-worktree git queries behind the entry format.
// Everything is best-effort: a failing query just omits its segment.
func gatherFacts(w gitx.Worktree, defBranch string) worktreeFacts {
	var f worktreeFacts
	if w.Bare {
		return f
	}
	if n, err := gitx.ChangeCount(w.Path); err == nil {
		f.changes, f.changesOK = n, true
	}
	if defBranch != "" && w.Branch != "" && w.Branch != defBranch && w.Head != "" {
		ref := "refs/heads/" + defBranch
		if !gitx.LocalBranchExists(".", defBranch) {
			ref = "refs/remotes/origin/" + defBranch
		}
		f.mergeKnown = true
		f.merged = gitx.IsAncestor(".", w.Head, ref)
	}
	f.ahead, f.behind, f.syncKnown, f.upstreamGone = syncState(w.Branch)
	return f
}

// syncState reports how branch relates to its upstream: ahead/behind counts
// when a live upstream exists (ok), or gone when the upstream was deleted
// on the remote. An empty branch (bare or detached) yields all zero values.
// Local knowledge only — nothing here fetches.
func syncState(branch string) (ahead, behind int, ok, gone bool) {
	if branch == "" {
		return 0, 0, false, false
	}
	if a, b, ok := gitx.AheadBehind(".", branch); ok {
		return a, b, true, false
	}
	return 0, 0, false, gitx.UpstreamGone(".", branch)
}

// worktreeLines renders the two-line rich entry shared by list and the
// pickers:
//
//	name [branch]
//	hash subject (age) | N unstaged | ↑2 ↓1 | ✓ merged into main
//
// Name is bright, the branch green, commit metadata gray, ↑ (commits to
// push) cyan and ↓ (to pull) yellow — shown only when the branch has a
// live upstream and is out of sync, with a yellow `upstream gone` tag in
// the same slot when the upstream was deleted on the remote — merge status
// green when merged and yellow when not; locked/prunable tags close the
// line. Any segment whose data is unavailable is omitted.
func worktreeLines(w gitx.Worktree, infos map[string]gitx.CommitInfo, defBranch string, f worktreeFacts, subjectLimit int) (string, string) {
	name := colorText(filepath.Base(w.Path), ansiBold)
	switch {
	case w.Branch != "":
		name += " " + colorText("["+w.Branch+"]", ansiGreen)
	case w.Bare:
		name += " " + colorText("[bare]", ansiGray)
	case w.Detached:
		name += " " + colorText("[detached]", ansiGray)
	}

	var meta string
	sep := colorText(" | ", ansiGray)
	if head := w.Head; head != "" {
		if len(head) > 8 {
			head = head[:8]
		}
		commit := head
		if info, ok := infos[w.Head]; ok {
			if info.Subject != "" {
				commit += " " + truncate(info.Subject, subjectLimit)
			}
			if info.When != "" {
				commit += " (" + info.When + ")"
			}
		}
		meta = colorText(commit, ansiGray)
	}
	if f.changesOK {
		if meta != "" {
			meta += sep
		}
		meta += strconv.Itoa(f.changes) + " unstaged"
	}
	switch {
	case f.syncKnown && (f.ahead > 0 || f.behind > 0):
		var arrows []string
		if f.ahead > 0 {
			arrows = append(arrows, colorText("↑"+strconv.Itoa(f.ahead), ansiCyan))
		}
		if f.behind > 0 {
			arrows = append(arrows, colorText("↓"+strconv.Itoa(f.behind), ansiYellow))
		}
		if meta != "" {
			meta += sep
		}
		meta += strings.Join(arrows, " ")
	case f.upstreamGone:
		if meta != "" {
			meta += sep
		}
		meta += colorText("upstream gone", ansiYellow)
	}
	if f.mergeKnown {
		if meta != "" {
			meta += sep
		}
		if f.merged {
			meta += colorText("✓ merged into "+defBranch, ansiGreen)
		} else {
			meta += colorText("✗ not merged into "+defBranch, ansiYellow)
		}
	}
	if w.Locked {
		if meta != "" {
			meta += sep
		}
		meta += colorText("locked", ansiCyan)
	}
	if w.Prunable {
		if meta != "" {
			meta += sep
		}
		meta += colorText("prunable", ansiYellow)
	}
	return name, meta
}

// worktreeOption renders the two-line rich entry as a picker label — the
// name/branch line with the metadata line underneath, matching th list.
func worktreeOption(w gitx.Worktree, infos map[string]gitx.CommitInfo, defBranch string, subjectLimit int) string {
	line1, line2 := worktreeLines(w, infos, defBranch, gatherFacts(w, defBranch), subjectLimit)
	if line2 == "" {
		return line1
	}
	return line1 + "\n  " + line2
}

// worktreeInfos fetches commit display info for the worktrees, skipping
// headless (bare) entries. Best-effort: on error the entries just render
// without subjects and ages.
func worktreeInfos(wts []gitx.Worktree) map[string]gitx.CommitInfo {
	var shas []string
	for _, w := range wts {
		if w.Head != "" {
			shas = append(shas, w.Head)
		}
	}
	infos, _ := gitx.CommitInfos(".", shas)
	return infos
}
