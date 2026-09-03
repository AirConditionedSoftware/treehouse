package cmd

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"slices"
	"syscall"

	"github.com/AirConditionedSoftware/treehouse/internal/config"
	"github.com/AirConditionedSoftware/treehouse/internal/gitx"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Run the configured run command in the current worktree",
	Long: `Run the command configured as "run" — the one command you keep coming back
to in a repository (a dev server, a test watcher) — via sh -c in the root
of the current worktree, so it works from any subdirectory.

The command runs in the foreground with your terminal's stdin, stdout, and
stderr: th prints nothing on stdout (its own output goes to stderr, so
th run | grep sees only the command's output), Ctrl-C reaches the command,
and th exits with the command's exit code. Worktree metadata is available
to it as the TH_WORKTREE, TH_MAIN, TH_REPO, and TH_BRANCH environment
variables.

Set "run" in the global config (top level or a repos entry) or the repo's
.thrc; it resolves through the same four layers as every other setting. A
command supplied by the repo's .thrc requires your approval before it
executes — and without a terminal to ask at, an unapproved command is an
error, not a skip: unlike the lifecycle hooks, the command is the whole
job, and exiting zero without running it would masquerade as success.`,
	Args: cobra.NoArgs,
	RunE: runRun,
}

func runRun(cmd *cobra.Command, args []string) error {
	wts, err := gitx.ListWorktrees(".")
	if err != nil {
		return err
	}
	mainPath := wts[0].Path
	res, err := config.Resolve(mainPath)
	if err != nil {
		return err
	}
	if err := finalizeLocalMigration(res); err != nil {
		return err
	}
	applyDisplayConfig(res.Settings)

	if res.Run == "" {
		return fmt.Errorf(`no run command configured; set "run" in .thrc or ~/.th/config.json`)
	}

	// The worktree root, not the cwd: th run works from any subdirectory,
	// and the command always runs at the top of the current worktree.
	top, err := gitx.Toplevel(".")
	if err != nil {
		return err
	}
	branch := ""
	for _, w := range wts {
		if samePath(w.Path, top) {
			branch = w.Branch
			break
		}
	}
	repo := res.RepoName
	if repo == "" {
		repo = filepath.Base(mainPath)
	}

	if err := gateRunCommand(mainPath, res); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "run: %s\n", res.Run)
	return execRunCommand(top, mainPath, repo, branch, res.Run)
}

// gateRunCommand enforces the trust gate for a .thrc-supplied run command;
// commands from the user-owned config pass unasked. Unlike gateRepoHook,
// missing approval is an error, never a skip: a skipped lifecycle hook
// still leaves the surrounding add or remove having done its job, but the
// run command IS th run's job, and a warn-and-exit-0 would masquerade as
// success. So: a stored approval matching the command proceeds; without a
// terminal to ask at, th run fails naming the .thrc; on a terminal it
// prompts, and a declined prompt fails without storing anything, so the
// next run asks again.
func gateRunCommand(mainPath string, res config.Resolved) error {
	if !res.HookFromRepo("run") {
		return nil
	}
	cmds := []string{res.Run}
	if approved, ok := config.ApprovedCommands(mainPath, "run"); ok && slices.Equal(approved, cmds) {
		return nil
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return fmt.Errorf("the run command from %s is not approved; run th run in a terminal to review it", displayPath(res.LocalFile))
	}
	allowed, err := approveRepoCommands(mainPath, res.LocalFile, "run", cmds)
	if err != nil {
		return err
	}
	if !allowed {
		return fmt.Errorf("the run command from %s was not approved", displayPath(res.LocalFile))
	}
	return nil
}

// execRunCommand executes the run command as a transparent foreground
// process in the spirit of env or nice: the terminal's stdin, stdout, and
// stderr are the child's (th reserves nothing on stdout), Ctrl-C reaches
// the child, and the child's exit code becomes th's via exitCodeError.
//
// The child deliberately gets no process group of its own: it stays in
// th's foreground group, so the kernel delivers Ctrl-C to it directly. th
// swallows its own SIGINT (the child already got it) and forwards
// SIGTERM, which targets th alone. Signal deaths map to the shell
// convention 128+signal, so Ctrl-C exits 130.
func execRunCommand(worktreePath, mainPath, repo, branch, command string) error {
	cmd := exec.Command("sh", "-c", command)
	cmd.Dir = worktreePath
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = thEnv(worktreePath, mainPath, repo, branch)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("run command %q: %w", command, err)
	}

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM)
	go func() {
		for sig := range sigs {
			if sig == syscall.SIGTERM {
				cmd.Process.Signal(syscall.SIGTERM)
			}
		}
	}()

	err := cmd.Wait()
	// After Stop no more signals reach the channel, so the close below
	// safely drains the goroutine.
	signal.Stop(sigs)
	close(sigs)

	var ee *exec.ExitError
	if errors.As(err, &ee) {
		if ws, ok := ee.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
			return exitCodeError{128 + int(ws.Signal())}
		}
		return exitCodeError{ee.ExitCode()}
	}
	return err
}

// exitCodeError carries the child's exit code to Execute, which os.Exits
// with it without printing a th: line — the child already reported its own
// failure, and an echo would be noise that breaks stderr-parsing scripts.
type exitCodeError struct{ code int }

func (e exitCodeError) Error() string { return fmt.Sprintf("exit status %d", e.code) }

func init() {
	rootCmd.AddCommand(runCmd)
}
