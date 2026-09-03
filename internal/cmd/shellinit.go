package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// The wrappers hand th a private temp file via TH_CD_FILE and cd to
// whatever th writes there. `command` bypasses the th function itself and
// any user aliases on the coreutils; the status is captured before the cd
// and only degraded if the cd itself fails; the file is only acted on when
// non-empty and always removed. TH_CD_FILE is set for the one th process
// only (the fish prefix-assignment form needs fish >= 3.1).

const posixWrapper = `# th shell integration — wraps th so it can change your shell's directory.
# Install: eval "$(th shell-init zsh)" in ~/.zshrc (or bash in ~/.bashrc).
th() {
  local _th_cd_file _th_status
  _th_cd_file="$(command mktemp 2>/dev/null)" || { command th "$@"; return $?; }
  TH_CD_FILE="$_th_cd_file" command th "$@"
  _th_status=$?
  if [ -s "$_th_cd_file" ]; then
    cd -- "$(command cat "$_th_cd_file")" || _th_status=$?
  fi
  command rm -f -- "$_th_cd_file"
  return $_th_status
}
`

const fishWrapper = `# th shell integration — wraps th so it can change your shell's directory.
# Install: th shell-init fish | source in ~/.config/fish/config.fish.
function th --wraps th
    set -l _th_cd_file (command mktemp 2>/dev/null)
    if test -z "$_th_cd_file"
        command th $argv
        return $status
    end
    TH_CD_FILE=$_th_cd_file command th $argv
    set -l _th_status $status
    if test -s "$_th_cd_file"
        cd (command cat "$_th_cd_file"); or set _th_status $status
    end
    command rm -f "$_th_cd_file"
    return $_th_status
end
`

// shellInitSetup describes how one shell installs the th wrapper.
type shellInitSetup struct{ name, rcFile, snippet string }

func shellInitSetups() []shellInitSetup {
	return []shellInitSetup{
		{"zsh", "~/.zshrc", `eval "$(th shell-init zsh)"`},
		{"bash", "~/.bashrc", `eval "$(th shell-init bash)"`},
		{"fish", "~/.config/fish/config.fish", "th shell-init fish | source"},
	}
}

// shellInitScript returns the wrapper for shell; zsh and bash share the
// POSIX function, fish has its own.
func shellInitScript(shell string) (string, error) {
	switch shell {
	case "zsh", "bash":
		return posixWrapper, nil
	case "fish":
		return fishWrapper, nil
	}
	return "", fmt.Errorf("unsupported shell %q", shell)
}

var shellInitCmd = &cobra.Command{
	Use:   "shell-init [zsh|bash|fish]",
	Short: "Print the shell wrapper that lets th change your directory",
	Long: `Print a wrapper function for your shell that lets th change your shell's
directory: th cd jumps into a worktree, and th add leaves your terminal
inside the worktree it just created (unless the add opens VS Code — the
open wins and the terminal stays put; see auto_cd in the README).

Install by adding one line to your shell's startup file:

  zsh   (~/.zshrc):                   eval "$(th shell-init zsh)"
  bash  (~/.bashrc):                  eval "$(th shell-init bash)"
  fish  (~/.config/fish/config.fish): th shell-init fish | source

With no argument the shell is detected from $SHELL. The wrapper runs the
real th binary with a private TH_CD_FILE temp file and cds to whatever th
writes there; stdout and stderr pass through untouched, so scripts like
cd "$(th add my-branch)" keep working with or without it. PowerShell is
not supported yet.`,
	ValidArgs: []string{"zsh", "bash", "fish"},
	Args:      cobra.MatchAll(cobra.MaximumNArgs(1), cobra.OnlyValidArgs),
	RunE:      runShellInit,
}

func runShellInit(cmd *cobra.Command, args []string) error {
	setups := shellInitSetups()
	var shell string
	if len(args) == 1 {
		shell = args[0]
	} else {
		names := make([]string, len(setups))
		for i, s := range setups {
			names[i] = s.name
		}
		shell = detectShell(names)
	}
	script, err := shellInitScript(shell)
	if err != nil {
		return err
	}
	fmt.Print(script)
	// A bare interactive run is someone looking, not eval'ing — teach the
	// install step. Under eval or a pipe stdout is not a terminal.
	if term.IsTerminal(int(os.Stdout.Fd())) {
		for _, s := range setups {
			if s.name == shell {
				fmt.Fprintf(os.Stderr, "\nTo install, add this line to %s:\n\n    %s\n", s.rcFile, s.snippet)
			}
		}
	}
	return nil
}

func init() {
	rootCmd.AddCommand(shellInitCmd)
}
