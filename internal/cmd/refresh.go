package cmd

import (
	"errors"
	"fmt"
	"os"

	"github.com/AirConditionedSoftware/treehouse/internal/gitx"
	"github.com/spf13/cobra"
)

var refreshPostCreate bool

var refreshCmd = &cobra.Command{
	Use:   "refresh [branch...]",
	Short: "Re-run provisioning on existing worktrees",
	Long: `Re-run the config-driven provisioning on worktrees that already exist, so
they catch up with config changes made since they were created: the hooks
copy, copy_files and link_files placement, and the workspace file (window
color included) are applied again. Paths may be given instead of branch
names; with no arguments the worktree you are standing in is refreshed.

Files git tracks are never touched. Copied files are overwritten, so an
updated .env in the main worktree propagates; link_files paths that already
exist in the worktree are noted and left alone, while missing ones get
their symlink.

post_create does not re-run by default — setup commands like npm install
can be expensive or stateful — pass --post-create to opt in; commands a
repo's .thrc supplied still need your approval. pre_create never re-runs:
its before-anything-is-created contract cannot hold for an existing
worktree. The main worktree is never refreshed; it is the source copies
and links come from.

th refresh prints nothing on stdout; all narration goes to stderr.`,
	Args: cobra.ArbitraryArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runRefresh(args)
	},
}

func runRefresh(args []string) error {
	ctx, err := newAddContext()
	if err != nil {
		return err
	}

	// Resolve every target before touching anything: refresh mutates
	// nothing until the loop below, so a typo in the second argument fails
	// the whole run instead of leaving the first target half-narrated.
	var targets []gitx.Worktree
	if len(args) == 0 {
		top, err := gitx.Toplevel(".")
		if err != nil {
			return err
		}
		if samePath(top, ctx.mainPath) {
			return errors.New("you are in the main worktree; pass a branch to refresh (see th list)")
		}
		w := findWorktree(ctx.wts, top)
		if w == nil {
			return fmt.Errorf("no worktree found for %q (see th list)", top)
		}
		targets = append(targets, *w)
	} else {
		for _, name := range args {
			w := findWorktree(ctx.wts, name)
			if w == nil {
				return fmt.Errorf("no worktree found for %q (see th list)", name)
			}
			if samePath(w.Path, ctx.mainPath) {
				return fmt.Errorf("refusing to refresh the main worktree at %s (it is the source copies and links come from)", displayPath(w.Path))
			}
			targets = append(targets, *w)
		}
	}

	for i, w := range targets {
		if len(targets) > 1 {
			fmt.Fprintf(os.Stderr, "[%d/%d] Refreshing %s\n", i+1, len(targets), displayPath(w.Path))
		}
		if _, err := provisionWorktree(ctx, w.Path, w.Branch, refreshPostCreate); err != nil {
			return fmt.Errorf("refreshing %s: %w", displayPath(w.Path), err)
		}
		fmt.Fprintf(os.Stderr, "Refreshed %s\n", displayPath(w.Path))
	}
	return nil
}

func init() {
	// Refresh registers its own flag instances bound to the same package
	// vars th add uses, so provisionWorktree reads them unchanged (the
	// precedent is removeForce et al., bound on three commands). It is not
	// a subcommand of add: add's persistent --path/--open/--cd have no
	// meaning for an existing worktree.
	refreshCmd.Flags().BoolVar(&addCopyHooks, "copy-hooks", false, "copy the repo's git hooks into the worktree")
	refreshCmd.Flags().BoolVar(&addNoCopyHooks, "no-copy-hooks", false, "do not copy hooks even if the config enables copy_hooks")
	refreshCmd.Flags().StringArrayVar(&addCopyFile, "copy-file", nil, "extra file or glob (relative to the main worktree) to copy into the worktree; repeatable")
	refreshCmd.Flags().BoolVar(&addNoCopyFiles, "no-copy-files", false, "skip the config's copy_files list")
	refreshCmd.Flags().StringArrayVar(&addLinkFile, "link-file", nil, "extra file or glob (relative to the main worktree) to symlink into the worktree; repeatable")
	refreshCmd.Flags().BoolVar(&addNoLinkFiles, "no-link-files", false, "skip the config's link_files list")
	refreshCmd.Flags().BoolVar(&refreshPostCreate, "post-create", false, "also run the config's post_create commands (asks approval when they come from .thrc)")
	rootCmd.AddCommand(refreshCmd)
}
