package cmd

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var rootRemove bool

var rootCmd = &cobra.Command{
	Use:   "th",
	Short: "Manage git worktrees",
	Long: `treehouse (th) manages git worktrees: list the worktrees of the current
repository and create new ones in locations driven by ~/.th/config.json (or
$TH_CONFIG).`,
	SilenceUsage:  true,
	SilenceErrors: true,
	Args:          cobra.ArbitraryArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if rootRemove {
			return runRemove(args)
		}
		if len(args) > 0 {
			return fmt.Errorf("unknown command %q for \"th\"", args[0])
		}
		return cmd.Help()
	},
}

func init() {
	rootCmd.PersistentFlags().BoolVar(&noColor, "no-color", false, "disable colored output (also disabled when NO_COLOR is set or stdout is not a terminal)")
	rootCmd.PersistentFlags().BoolVar(&fullPathsFlag, "full-paths", false, "show absolute paths instead of abbreviating the home directory to ~")
	rootCmd.Flags().BoolVarP(&rootRemove, "remove", "r", false, "remove worktrees (shorthand for th remove); with no branch, select interactively")
	rootCmd.Flags().BoolVarP(&removeForce, "force", "f", false, "with -r: remove even if the worktree is dirty or locked")
	rootCmd.Flags().BoolVarP(&removeDeleteBranch, "delete-branch", "d", false, "with -r: also delete the branch")
	rootCmd.Flags().BoolVar(&removeNoPreRemove, "no-pre-remove", false, "with -r: skip the config's pre_remove commands")
	rootCmd.Flags().BoolVar(&removeNoPostRemove, "no-post-remove", false, "with -r: skip the config's post_remove commands")
}

func Execute(version string) {
	rootCmd.Version = version
	if err := rootCmd.Execute(); err != nil {
		// th run's child already reported its own failure; adopt its exit
		// code silently instead of adding a th: line.
		var ec exitCodeError
		if errors.As(err, &ec) {
			os.Exit(ec.code)
		}
		fmt.Fprintln(os.Stderr, "th: "+err.Error())
		os.Exit(1)
	}
	if versionRequested(os.Args[1:]) {
		if notice := updateNotice(version); notice != "" {
			fmt.Fprintln(os.Stderr, notice)
		}
	}
}
