package cmd

import (
	"errors"
	"fmt"
	"os"

	"github.com/AirConditionedSoftware/treehouse/internal/config"
	"github.com/AirConditionedSoftware/treehouse/internal/gitx"
	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var (
	cleanDryRun bool
	cleanYes    bool
)

var cleanCmd = &cobra.Command{
	Use:   "clean",
	Short: "Remove worktrees whose branches are merged or gone from origin",
	Long: `Find worktrees that look finished — the branch is merged into the default
branch, or it was deleted on origin — and offer to remove them in an
interactive multi-select with every candidate preselected. Branches are
kept, like th remove; --delete-branches deletes them too, with the same
rules as th remove --delete-branch.

Remote-tracking refs are refreshed with a best-effort fetch --prune first,
so a branch deleted on origin counts even if you haven't fetched lately.
The main worktree, the worktree you are in, and the default branch are
never candidates. Complements th prune, which only cleans up bookkeeping
for directories that are already gone.

--dry-run lists the candidates and changes nothing; --yes removes all
candidates without the picker (the non-interactive mode); --force removes
dirty candidates without prompting, like th remove --force. Under --yes an
unmerged branch is never force-deleted: git's refusal keeps it with a note,
so a gone-from-origin candidate with local-only commits survives.`,
	Args: cobra.NoArgs,
	RunE: runClean,
}

// staleCandidate is a worktree th clean proposes to remove, with why.
type staleCandidate struct {
	wt     gitx.Worktree
	reason string
}

func runClean(cmd *cobra.Command, args []string) error {
	wts, err := gitx.ListWorktrees(".")
	if err != nil {
		return err
	}
	if res, err := config.Resolve(wts[0].Path); err == nil {
		applyDisplayConfig(res.Settings)
	}

	// Refresh remote-tracking refs so branches deleted on origin show as
	// gone. Best-effort, like RemoteBranchExists' fetch: no origin or no
	// network just means the gone check works from local knowledge.
	_, _ = gitx.Run(".", "fetch", "--prune", "--quiet", "origin")

	cur, _ := gitx.Toplevel(".")
	defBranch := gitx.DefaultBranch(".")

	var stale []staleCandidate
	for i, w := range wts {
		// The default branch would always count as merged into itself;
		// detached worktrees have no branch to assess.
		if i == 0 || w.Bare || w.Branch == "" || w.Branch == defBranch {
			continue
		}
		if cur != "" && samePath(cur, w.Path) {
			continue
		}
		switch {
		case gatherFacts(w, defBranch).merged:
			stale = append(stale, staleCandidate{w, "merged into " + defBranch})
		case gitx.UpstreamGone(".", w.Branch):
			stale = append(stale, staleCandidate{w, "gone from origin"})
		}
	}
	if len(stale) == 0 {
		fmt.Fprintf(os.Stderr, "Nothing to clean: no worktree branch is merged into %s or deleted on origin.\n", defBranch)
		return nil
	}

	if cleanDryRun {
		for _, c := range stale {
			fmt.Fprintf(os.Stderr, "%s  %s (%s)\n", c.wt.Branch, displayPath(c.wt.Path), c.reason)
		}
		noun := "worktrees"
		if len(stale) == 1 {
			noun = "worktree"
		}
		fate := "branches are kept"
		if removeDeleteBranch {
			fate = "merged branches would be deleted too"
		}
		fmt.Fprintf(os.Stderr, "Would remove %d %s (dry run); %s.\n", len(stale), noun, fate)
		return nil
	}

	var selected []string
	if cleanYes {
		for _, c := range stale {
			selected = append(selected, c.wt.Path)
		}
	} else {
		if !term.IsTerminal(int(os.Stdin.Fd())) {
			return errors.New("interactive selection needs a terminal; use --dry-run to preview or --yes to remove all candidates")
		}
		candidates := make([]gitx.Worktree, len(stale))
		for i, c := range stale {
			candidates[i] = c.wt
		}
		infos := worktreeInfos(candidates)
		opts := make([]huh.Option[string], 0, len(stale))
		for _, c := range stale {
			opts = append(opts, huh.NewOption(worktreeOption(c.wt, infos, defBranch, 40), c.wt.Path).Selected(true))
		}
		fate := "branches are kept"
		if removeDeleteBranch {
			fate = "branches will be deleted"
		}
		form := huh.NewForm(huh.NewGroup(
			huh.NewMultiSelect[string]().
				Title(fmt.Sprintf("Remove these worktrees? (merged into %s or gone from origin; %s)", defBranch, fate)).
				Options(opts...).
				Value(&selected),
		))
		if err := form.Run(); err != nil {
			if errors.Is(err, huh.ErrUserAborted) {
				return errors.New("aborted")
			}
			return err
		}
		if len(selected) == 0 {
			fmt.Fprintln(os.Stderr, "Nothing selected.")
			return nil
		}
	}

	for i, path := range selected {
		if err := removeWorktree(path, i+1, len(selected)); err != nil {
			return err
		}
	}
	return nil
}

func init() {
	cleanCmd.Flags().BoolVarP(&cleanDryRun, "dry-run", "n", false, "show what would be removed without removing")
	cleanCmd.Flags().BoolVar(&cleanYes, "yes", false, "remove all candidates without the interactive picker")
	cleanCmd.Flags().BoolVarP(&removeForce, "force", "f", false, "remove dirty candidates without prompting")
	cleanCmd.Flags().BoolVarP(&removeDeleteBranch, "delete-branches", "d", false, "also delete the branches (git branch -d; unmerged branches are kept under --yes)")
	cleanCmd.Flags().BoolVar(&removeNoPreRemove, "no-pre-remove", false, "skip the config's pre_remove commands")
	cleanCmd.Flags().BoolVar(&removeNoPostRemove, "no-post-remove", false, "skip the config's post_remove commands")
	rootCmd.AddCommand(cleanCmd)
}
