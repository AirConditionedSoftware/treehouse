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

var openCmd = &cobra.Command{
	Use:   "open [branch]",
	Short: "Open a worktree in VS Code",
	Long: `Open a worktree of the current repository in VS Code. With no argument an
interactive picker lists the worktrees; with a branch (or path) it opens that
worktree directly. The th-generated .code-workspace file is opened when the
worktree has one, the folder otherwise.

Only available when vscode.open is enabled for the repo in the th config.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runOpen,
}

func runOpen(cmd *cobra.Command, args []string) error {
	wts, err := gitx.ListWorktrees(".")
	if err != nil {
		return err
	}

	res, err := config.Resolve(wts[0].Path)
	if err != nil {
		return err
	}
	if err := finalizeLocalMigration(res); err != nil {
		return err
	}
	settings := res.Settings
	applyDisplayConfig(settings)
	if !settings.VSCodeOpenEnabled() {
		cfgPath, _, _ := config.Path()
		return fmt.Errorf("vscode.open is not enabled for this repo; set \"vscode\": {\"open\": true} (globally or in this repo's entry) in %s to use th open", cfgPath)
	}

	var target *gitx.Worktree
	if len(args) == 1 {
		if target = findWorktree(wts, args[0]); target == nil {
			return fmt.Errorf("no worktree found for %q (see th list)", args[0])
		}
	} else {
		if target, err = pickWorktree(wts, "Select a worktree to open"); err != nil {
			return err
		}
		if target == nil {
			fmt.Fprintln(os.Stderr, "Nothing selected.")
			return nil
		}
	}

	openInVSCode(openTargetFor(*target, settings))
	return nil
}

// openTargetFor returns the th-generated workspace file when the worktree
// has one, else the worktree folder.
func openTargetFor(w gitx.Worktree, settings config.Settings) string {
	if ws := workspaceFilePath(settings, w.Path, w.Branch); ws != "" {
		if _, err := os.Stat(ws); err == nil {
			return ws
		}
	}
	return w.Path
}

// pickWorktree shows a single-select of all worktrees with the same labels
// as the remove picker.
func pickWorktree(wts []gitx.Worktree, title string) (*gitx.Worktree, error) {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return nil, errors.New("interactive selection needs a terminal; pass a branch name (see th list)")
	}

	infos := worktreeInfos(wts)
	defBranch := gitx.DefaultBranch(".")

	opts := make([]huh.Option[int], 0, len(wts))
	for i, w := range wts {
		opts = append(opts, huh.NewOption(worktreeOption(w, infos, defBranch, 40), i))
	}

	selected := -1
	form := huh.NewForm(huh.NewGroup(
		huh.NewSelect[int]().
			Title(title).
			Options(opts...).
			Value(&selected),
	))
	if err := form.Run(); err != nil {
		if errors.Is(err, huh.ErrUserAborted) {
			return nil, errors.New("aborted")
		}
		return nil, err
	}
	if selected < 0 || selected >= len(wts) {
		return nil, nil
	}
	return &wts[selected], nil
}

func init() {
	rootCmd.AddCommand(openCmd)
}
