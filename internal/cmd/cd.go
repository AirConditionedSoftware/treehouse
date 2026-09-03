package cmd

import (
	"fmt"
	"os"

	"github.com/AirConditionedSoftware/treehouse/internal/config"
	"github.com/AirConditionedSoftware/treehouse/internal/gitx"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var cdCmd = &cobra.Command{
	Use:   "cd [branch]",
	Short: "Change directory to a worktree (with shell integration)",
	Long: `Resolve a worktree of the current repository and cd there. With no argument
an interactive picker lists the worktrees; with a branch (or path) it
resolves that worktree directly.

The resolved path is the only stdout output, so cd "$(th cd my-branch)"
works anywhere. With the shell wrapper from th shell-init installed, plain
th cd my-branch changes your shell's directory itself — it always does,
regardless of the auto_cd and vscode.open settings, because th cd is an
explicit request to navigate.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runCd,
}

func runCd(cmd *cobra.Command, args []string) error {
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
	applyDisplayConfig(res.Settings)

	var target *gitx.Worktree
	if len(args) == 1 {
		if target = findWorktree(wts, args[0]); target == nil {
			return fmt.Errorf("no worktree found for %q (see th list)", args[0])
		}
	} else {
		if target, err = pickWorktree(wts, "Select a worktree to cd into"); err != nil {
			return err
		}
		if target == nil {
			fmt.Fprintln(os.Stderr, "Nothing selected.")
			return nil
		}
	}

	fmt.Println(target.Path)
	writeCDFile(target.Path)
	if os.Getenv("TH_CD_FILE") == "" && term.IsTerminal(int(os.Stdout.Fd())) {
		fmt.Fprintln(os.Stderr, `hint: th printed the path but can't change your directory; install the wrapper with th shell-init (see th shell-init --help)`)
	}
	return nil
}

// writeCDFile hands the shell wrapper (see th shell-init) a directory to
// cd into by writing it to $TH_CD_FILE. Without the wrapper the variable
// is unset and this is a no-op; a failed write is a warning, never an
// error — navigation must not fail the command that did the real work.
func writeCDFile(path string) {
	f := os.Getenv("TH_CD_FILE")
	if f == "" {
		return
	}
	if err := os.WriteFile(f, []byte(path), 0o600); err != nil {
		fmt.Fprintf(os.Stderr, "writing TH_CD_FILE failed: %v\n", err)
	}
}

func init() {
	rootCmd.AddCommand(cdCmd)
}
