package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/AirConditionedSoftware/treehouse/internal/config"
	"github.com/AirConditionedSoftware/treehouse/internal/gitx"
	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var (
	removeForce        bool
	removeDeleteBranch bool
	removeNoPreRemove  bool
	removeNoPostRemove bool
)

var removeCmd = &cobra.Command{
	Use:     "remove [branch...]",
	Aliases: []string{"rm"},
	Short:   "Remove worktrees",
	Long: `Remove the worktrees that have the given branches checked out. The branches
themselves are kept unless --delete-branch is passed. Paths may be given
instead of branch names.

With no arguments, an interactive picker lists the removable worktrees and
lets you select one or more to delete.

--delete-branch deletes each removed worktree's branch with git branch -d.
When git refuses because the branch is not fully merged, an interactive run
asks before forcing with -D; a non-interactive run keeps the branch, says
so, and still succeeds. The default branch is never deleted, and --force
does not extend to branch deletion.`,
	Args: cobra.ArbitraryArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runRemove(args)
	},
}

func runRemove(args []string) error {
	if len(args) == 0 {
		return removeInteractive()
	}
	for i, name := range args {
		if err := removeWorktree(name, i+1, len(args)); err != nil {
			return err
		}
	}
	return nil
}

// removeInteractive shows a multi-select of every worktree except the main
// one and the one the user is standing in.
func removeInteractive() error {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return errors.New("interactive selection needs a terminal; pass branch names instead (see th list)")
	}

	wts, err := gitx.ListWorktrees(".")
	if err != nil {
		return err
	}
	cur, _ := gitx.Toplevel(".")

	var candidates []gitx.Worktree
	for i, w := range wts {
		if i == 0 || w.Bare {
			continue
		}
		if cur != "" && samePath(cur, w.Path) {
			continue
		}
		candidates = append(candidates, w)
	}
	if len(candidates) == 0 {
		fmt.Fprintln(os.Stderr, "No removable worktrees (the main worktree and the one you're in don't count).")
		return nil
	}

	infos := worktreeInfos(candidates)
	defBranch := gitx.DefaultBranch(".")
	opts := make([]huh.Option[string], 0, len(candidates))
	for _, w := range candidates {
		opts = append(opts, huh.NewOption(worktreeOption(w, infos, defBranch, 40), w.Path))
	}

	var selected []string
	form := huh.NewForm(huh.NewGroup(
		huh.NewMultiSelect[string]().
			Title("Select worktrees to remove").
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

	for i, path := range selected {
		if err := removeWorktree(path, i+1, len(selected)); err != nil {
			return err
		}
	}
	return nil
}

// confirmForceRemoval asks whether a dirty worktree should be removed
// anyway. Prompted per worktree, so multi-removals decide each one.
func confirmForceRemoval(w gitx.Worktree) (bool, error) {
	ok := false
	form := huh.NewForm(huh.NewGroup(
		huh.NewConfirm().
			Title(fmt.Sprintf("%s has modified or untracked files. Remove anyway?", branchLabel(w))).
			Description(displayPath(w.Path)).
			Affirmative("Force remove").
			Negative("Skip").
			Value(&ok),
	))
	if err := form.Run(); err != nil {
		if errors.Is(err, huh.ErrUserAborted) {
			return false, errors.New("aborted")
		}
		return false, err
	}
	return ok, nil
}

// confirmBranchDelete asks whether an unmerged branch should be force
// deleted after its worktree was removed. Prompted per branch, so
// multi-removals decide each one.
func confirmBranchDelete(branch string) (bool, error) {
	ok := false
	form := huh.NewForm(huh.NewGroup(
		huh.NewConfirm().
			Title(fmt.Sprintf("Branch %q is not fully merged. Delete it anyway?", branch)).
			Description("git branch -D discards its unmerged commits.").
			Affirmative("Delete branch").
			Negative("Keep branch").
			Value(&ok),
	))
	if err := form.Run(); err != nil {
		if errors.Is(err, huh.ErrUserAborted) {
			return false, errors.New("aborted")
		}
		return false, err
	}
	return ok, nil
}

func branchLabel(w gitx.Worktree) string {
	switch {
	case w.Branch != "":
		return w.Branch
	case w.Bare:
		return "(bare)"
	default:
		return "(detached)"
	}
}

// findWorktree matches name against worktree branches first, then paths.
func findWorktree(wts []gitx.Worktree, name string) *gitx.Worktree {
	for i := range wts {
		if wts[i].Branch == name {
			return &wts[i]
		}
	}
	if abs, err := filepath.Abs(name); err == nil {
		for i := range wts {
			if samePath(wts[i].Path, abs) {
				return &wts[i]
			}
		}
	}
	return nil
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

// removeWorktree removes the worktree matching name. step and total drive
// the [step/total] progress prefix; multi-removals pass their position,
// single removals 1, 1 (no prefix).
func removeWorktree(name string, step, total int) error {
	wts, err := gitx.ListWorktrees(".")
	if err != nil {
		return err
	}

	// Display preferences and (later) workspace-file cleanup; a broken
	// config must not block removal.
	res, cfgErr := config.Resolve(wts[0].Path)
	if cfgErr == nil {
		applyDisplayConfig(res.Settings)
	}

	target := findWorktree(wts, name)
	if target == nil {
		return fmt.Errorf("no worktree found for %q (see th list)", name)
	}

	if target.Path == wts[0].Path {
		return fmt.Errorf("refusing to remove the main worktree at %s", displayPath(target.Path))
	}
	if cur, err := gitx.Toplevel("."); err == nil && samePath(cur, target.Path) {
		return fmt.Errorf("cannot remove the worktree you are in; cd out of %s first", displayPath(target.Path))
	}

	force := removeForce
	if !force {
		// A dirty-worktree check failure (e.g. the directory is gone) just
		// means there is nothing to warn about; let git decide below.
		if dirty, err := gitx.IsDirty(target.Path); err == nil && dirty {
			if !term.IsTerminal(int(os.Stdin.Fd())) {
				return fmt.Errorf("worktree %s has modified or untracked files; re-run with --force", displayPath(target.Path))
			}
			ok, err := confirmForceRemoval(*target)
			if err != nil {
				return err
			}
			if !ok {
				fmt.Fprintf(os.Stderr, "Skipped %s\n", displayPath(target.Path))
				return nil
			}
			force = true
		}
	}

	// A broken config must not make worktrees unremovable, but silently
	// dropping configured teardown would be worse — say so once.
	if cfgErr != nil {
		fmt.Fprintf(os.Stderr, "Warning: config could not be read (%v); skipping any pre_remove/post_remove commands\n", cfgErr)
	}
	repo := ""
	if cfgErr == nil {
		if repo = res.RepoName; repo == "" {
			repo = filepath.Base(wts[0].Path)
		}
	}

	// pre_remove runs inside the worktree, after the guards and the dirty
	// decision — only once removal is committed (--force answers
	// cleanliness, not teardown, so it still runs) — and before anything
	// is deleted, so a failure leaves the worktree intact. A hook that
	// dirties a clean tree can make the non-forced git removal below fail;
	// the remedy is --force.
	if cfgErr == nil && len(res.PreRemove) > 0 && !removeNoPreRemove {
		cmds, err := gateRepoHook(wts[0].Path, res, "pre_remove", res.PreRemove)
		if err != nil {
			return err
		}
		if len(cmds) > 0 {
			if err := runHook("pre_remove", target.Path, target.Path, wts[0].Path, repo, target.Branch, cmds); err != nil {
				return fmt.Errorf("%w; %s was not removed (skip teardown with --no-pre-remove)", err, displayPath(target.Path))
			}
		}
	}

	// Deleting a big worktree can take a while with no output from git, so
	// narrate what is being removed and how much; on a terminal the line
	// ticks with elapsed time until git finishes.
	label := "Removing " + displayPath(target.Path)
	if total > 1 {
		label = fmt.Sprintf("[%d/%d] %s", step, total, label)
	}
	if size, err := dirSize(target.Path); err == nil && size > 0 {
		label += " (" + formatSize(size, "") + ")"
	}

	rmArgs := []string{"worktree", "remove"}
	if force {
		rmArgs = append(rmArgs, "--force")
	}
	rmArgs = append(rmArgs, target.Path)
	if err := runWithStatus(label, func() error {
		_, err := gitx.Run(".", rmArgs...)
		return err
	}); err != nil {
		return err
	}

	// The th-generated workspace file lives next to the worktree; clean it
	// up too so it doesn't orphan. Only when th manages workspace files for
	// this repo — never delete files th didn't create.
	if cfgErr == nil && res.VSCodeWorkspaceFileEnabled() {
		if ws := workspaceFilePath(res.Settings, target.Path, target.Branch); ws != "" {
			if err := os.Remove(ws); err == nil {
				fmt.Fprintf(os.Stderr, "Removed workspace file %s\n", displayPath(ws))
			}
		}
	}

	note, noteErr := resolveBranch(*target)
	if note != "" {
		fmt.Fprintf(os.Stderr, "Removed worktree %s %s\n", displayPath(target.Path), note)
	} else {
		fmt.Fprintf(os.Stderr, "Removed worktree %s\n", displayPath(target.Path))
	}
	if noteErr != nil {
		// The user aborted the branch prompt; surface the abort and skip
		// the post_remove observer.
		return noteErr
	}

	// post_remove runs last, once the removal has fully settled — after
	// branch resolution. The worktree directory is gone, so it runs in the
	// main worktree with TH_WORKTREE carrying the former path — enough to
	// clean up external resources keyed on it or the branch. A failure is
	// reported, but the removal stands.
	if cfgErr == nil && len(res.PostRemove) > 0 && !removeNoPostRemove {
		cmds, err := gateRepoHook(wts[0].Path, res, "post_remove", res.PostRemove)
		if err != nil {
			return err
		}
		if len(cmds) > 0 {
			if err := runHook("post_remove", wts[0].Path, target.Path, wts[0].Path, repo, target.Branch, cmds); err != nil {
				return fmt.Errorf("worktree removed, but %w", err)
			}
		}
	}
	return nil
}

// resolveBranch decides what happens to the branch of a just-removed
// worktree and returns the parenthesized note for the Removed line (""
// for a detached worktree or a branch that vanished since listing). With
// --delete-branch it tries git branch -d and reacts to a refusal: on a
// terminal it asks before escalating to -D, otherwise the branch is kept
// with a note — the worktree is already gone, so an error here would
// misreport a succeeded removal and strand later candidates in
// multi-remove loops. err is non-nil only when the confirm prompt is
// aborted or fails; the note is still printed first.
func resolveBranch(target gitx.Worktree) (string, error) {
	if target.Branch == "" {
		return "", nil
	}
	if !removeDeleteBranch {
		return fmt.Sprintf("(branch %q kept)", target.Branch), nil
	}
	if target.Branch == gitx.DefaultBranch(".") {
		// Reachable when the default branch was checked out in a linked
		// worktree; deleting it is never what anyone wants.
		return fmt.Sprintf("(branch %q kept: default branch)", target.Branch), nil
	}
	if !gitx.LocalBranchExists(".", target.Branch) {
		return "", nil
	}
	if _, err := gitx.Run(".", "branch", "-d", target.Branch); err == nil {
		return fmt.Sprintf("(branch %q deleted)", target.Branch), nil
	}
	// git refused -d, which means the branch is not fully merged in git's
	// eyes (merged into HEAD or its upstream). Force deletion is one
	// deliberate step away: a prompt on a terminal, git branch -D by hand
	// otherwise — --force never escalates.
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return fmt.Sprintf("(branch %q kept: not fully merged; use git branch -D to force)", target.Branch), nil
	}
	ok, err := confirmBranchDelete(target.Branch)
	if err != nil {
		return fmt.Sprintf("(branch %q kept)", target.Branch), err
	}
	if !ok {
		return fmt.Sprintf("(branch %q kept)", target.Branch), nil
	}
	if _, err := gitx.Run(".", "branch", "-D", target.Branch); err != nil {
		return fmt.Sprintf("(branch %q kept: %v)", target.Branch, err), nil
	}
	return fmt.Sprintf("(branch %q deleted)", target.Branch), nil
}

func init() {
	removeCmd.Flags().BoolVarP(&removeForce, "force", "f", false, "remove even if the worktree is dirty or locked")
	removeCmd.Flags().BoolVarP(&removeDeleteBranch, "delete-branch", "d", false, "also delete the branch (git branch -d; asks before forcing an unmerged one)")
	removeCmd.Flags().BoolVar(&removeNoPreRemove, "no-pre-remove", false, "skip the config's pre_remove commands")
	removeCmd.Flags().BoolVar(&removeNoPostRemove, "no-post-remove", false, "skip the config's post_remove commands")
	rootCmd.AddCommand(removeCmd)
}
