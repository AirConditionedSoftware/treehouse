package gitx

import "strings"

// Worktree is one entry of `git worktree list --porcelain`.
type Worktree struct {
	Path           string `json:"path"`
	Head           string `json:"head,omitempty"`
	Branch         string `json:"branch,omitempty"`
	Bare           bool   `json:"bare,omitempty"`
	Detached       bool   `json:"detached,omitempty"`
	Locked         bool   `json:"locked,omitempty"`
	LockedReason   string `json:"locked_reason,omitempty"`
	Prunable       bool   `json:"prunable,omitempty"`
	PrunableReason string `json:"prunable_reason,omitempty"`
}

// ListWorktrees returns the worktrees of the repository containing dir. The
// first entry is always the main worktree.
func ListWorktrees(dir string) ([]Worktree, error) {
	out, err := Run(dir, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}
	return ParseWorktrees(out), nil
}

// ParseWorktrees parses `git worktree list --porcelain` output: entries are
// separated by blank lines, each starting with a "worktree <path>" line.
func ParseWorktrees(out string) []Worktree {
	var wts []Worktree
	var cur *Worktree
	for _, line := range strings.Split(out, "\n") {
		if line == "" {
			cur = nil
			continue
		}
		if path, ok := strings.CutPrefix(line, "worktree "); ok {
			wts = append(wts, Worktree{Path: path})
			cur = &wts[len(wts)-1]
			continue
		}
		if cur == nil {
			continue
		}
		switch {
		case strings.HasPrefix(line, "HEAD "):
			cur.Head = strings.TrimPrefix(line, "HEAD ")
		case strings.HasPrefix(line, "branch "):
			cur.Branch = strings.TrimPrefix(strings.TrimPrefix(line, "branch "), "refs/heads/")
		case line == "bare":
			cur.Bare = true
		case line == "detached":
			cur.Detached = true
		case line == "locked":
			cur.Locked = true
		case strings.HasPrefix(line, "locked "):
			cur.Locked = true
			cur.LockedReason = strings.TrimPrefix(line, "locked ")
		case line == "prunable":
			cur.Prunable = true
		case strings.HasPrefix(line, "prunable "):
			cur.Prunable = true
			cur.PrunableReason = strings.TrimPrefix(line, "prunable ")
		}
	}
	return wts
}
