package cmd

import (
	"fmt"
	"os"

	"github.com/AirConditionedSoftware/treehouse/internal/config"
	"github.com/AirConditionedSoftware/treehouse/internal/gitx"
	"github.com/spf13/cobra"
)

var pruneDryRun bool

var pruneCmd = &cobra.Command{
	Use:   "prune",
	Short: "Clean up stale worktree entries",
	Long: `Remove git's bookkeeping for stale worktrees — entries whose directories
have been deleted manually, shown as "prunable" in th list. Only
.git/worktrees metadata is cleaned; branches are kept.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		wts, err := gitx.ListWorktrees(".")
		if err != nil {
			return err
		}
		if res, err := config.Resolve(wts[0].Path); err == nil {
			applyDisplayConfig(res.Settings)
		}
		var stale []gitx.Worktree
		for _, w := range wts {
			if w.Prunable {
				stale = append(stale, w)
			}
		}
		if len(stale) == 0 {
			fmt.Fprintln(os.Stderr, "Nothing to prune.")
			return nil
		}

		for _, w := range stale {
			label := w.Branch
			if label == "" {
				label = "(detached)"
			}
			reason := w.PrunableReason
			if reason == "" {
				reason = "stale"
			}
			fmt.Fprintf(os.Stderr, "%s  %s (%s)\n", label, displayPath(w.Path), reason)
		}

		noun := "entries"
		if len(stale) == 1 {
			noun = "entry"
		}
		if pruneDryRun {
			fmt.Fprintf(os.Stderr, "Would prune %d stale worktree %s (dry run).\n", len(stale), noun)
			return nil
		}
		if _, err := gitx.Run(".", "worktree", "prune"); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "Pruned %d stale worktree %s; branches are kept.\n", len(stale), noun)
		return nil
	},
}

func init() {
	pruneCmd.Flags().BoolVarP(&pruneDryRun, "dry-run", "n", false, "show what would be pruned without pruning")
	rootCmd.AddCommand(pruneCmd)
}
