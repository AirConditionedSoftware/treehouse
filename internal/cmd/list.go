package cmd

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/AirConditionedSoftware/treehouse/internal/config"
	"github.com/AirConditionedSoftware/treehouse/internal/gitx"
	"github.com/spf13/cobra"
)

var listJSON bool

// listEntry is a gitx.Worktree (a pure porcelain parse) extended with the
// computed upstream facts --json adds. Pointer counts keep three states
// apart: a live upstream carries both keys even at 0, no upstream omits
// them, and a deleted upstream omits them and sets upstream_gone instead.
type listEntry struct {
	gitx.Worktree
	Ahead        *int `json:"ahead,omitempty"`
	Behind       *int `json:"behind,omitempty"`
	UpstreamGone bool `json:"upstream_gone,omitempty"`
	// Dirty is the number of changed paths (staged, unstaged, untracked);
	// 0 is a clean worktree, absent means it could not be probed (a bare
	// entry, or a directory that is gone).
	Dirty *int `json:"dirty,omitempty"`
	// Merged reports whether the branch is contained in the default
	// branch. Absent when merge status does not apply: a bare or detached
	// worktree, the default branch itself, or no default branch at all.
	Merged *bool `json:"merged,omitempty"`
}

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List worktrees of the current repository",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		wts, err := gitx.ListWorktrees(".")
		if err != nil {
			return err
		}
		// Display preferences only — a broken config must not break list.
		if res, err := config.Resolve(wts[0].Path); err == nil {
			applyDisplayConfig(res.Settings)
		}
		// The same facts the human format shows, so the two views cannot
		// drift apart.
		defBranch := gitx.DefaultBranch(".")
		if listJSON {
			entries := make([]listEntry, len(wts))
			for i, w := range wts {
				entries[i] = listEntry{Worktree: w}
				f := gatherFacts(w, defBranch)
				if f.syncKnown {
					ahead, behind := f.ahead, f.behind
					entries[i].Ahead, entries[i].Behind = &ahead, &behind
				} else if f.upstreamGone {
					entries[i].UpstreamGone = true
				}
				if f.changesOK {
					dirty := f.changes
					entries[i].Dirty = &dirty
				}
				if f.mergeKnown {
					merged := f.merged
					entries[i].Merged = &merged
				}
			}
			return printJSON(entries)
		}

		current, _ := gitx.Toplevel(".")
		infos := worktreeInfos(wts)

		var b strings.Builder
		for _, w := range wts {
			marker := "  "
			if current != "" && samePath(current, w.Path) {
				marker = "* "
			}
			line1, line2 := worktreeLines(w, infos, defBranch, gatherFacts(w, defBranch), 60)
			fmt.Fprintln(&b, marker+line1)
			if line2 != "" {
				fmt.Fprintln(&b, "  "+line2)
			}
		}
		fmt.Print(b.String())
		return nil
	},
}

// samePath compares two paths after resolving symlinks (e.g. /tmp vs
// /private/tmp on macOS).
func samePath(a, b string) bool {
	if r, err := filepath.EvalSymlinks(a); err == nil {
		a = r
	}
	if r, err := filepath.EvalSymlinks(b); err == nil {
		b = r
	}
	return a == b
}

func init() {
	listCmd.Flags().BoolVar(&listJSON, "json", false, "output as JSON")
	rootCmd.AddCommand(listCmd)
}
