package cmd

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// shellSetup describes how one shell enables th completion.
type shellSetup struct {
	name    string
	rcFile  string
	snippet string // the line to add to rcFile
	reload  string // command to apply it to the current session
	note    string
}

func shellSetups() []shellSetup {
	return []shellSetup{
		{
			name:    "zsh",
			rcFile:  "~/.zshrc",
			snippet: "source <(th completion zsh)",
			reload:  "source ~/.zshrc",
			note:    `if completion isn't already enabled in your zsh, add "autoload -U compinit; compinit" above that line.`,
		},
		{
			name:    "bash",
			rcFile:  "~/.bashrc",
			snippet: "source <(th completion bash)",
			reload:  "source ~/.bashrc",
			note:    "on macOS your terminal may read ~/.bash_profile instead of ~/.bashrc.",
		},
		{
			name:    "fish",
			rcFile:  "~/.config/fish/config.fish",
			snippet: "th completion fish | source",
			reload:  "source ~/.config/fish/config.fish",
		},
		{
			name:    "powershell",
			rcFile:  "$PROFILE",
			snippet: "th completion powershell | Out-String | Invoke-Expression",
			reload:  ". $PROFILE",
		},
	}
}

var completionCmd = &cobra.Command{
	Use:   "completion [bash|zsh|fish|powershell]",
	Short: "Set up shell completion",
	Long: `Set up shell completion for th.

With no argument, an interactive wizard asks for your shell, shows the line
to add to its startup file, and copies that line to your clipboard.

With a shell argument, prints the raw completion script for that shell —
this is what the installed line sources; you rarely need to call it
yourself.`,
	ValidArgs: []string{"bash", "zsh", "fish", "powershell"},
	Args:      cobra.MatchAll(cobra.MaximumNArgs(1), cobra.OnlyValidArgs),
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) == 1 {
			return writeCompletionScript(args[0])
		}
		return completionWizard()
	},
}

func writeCompletionScript(shell string) error {
	switch shell {
	case "bash":
		return rootCmd.GenBashCompletionV2(os.Stdout, true)
	case "zsh":
		return rootCmd.GenZshCompletion(os.Stdout)
	case "fish":
		return rootCmd.GenFishCompletion(os.Stdout, true)
	case "powershell":
		return rootCmd.GenPowerShellCompletionWithDesc(os.Stdout)
	}
	return fmt.Errorf("unsupported shell %q", shell)
}

func completionWizard() error {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return errors.New("interactive setup needs a terminal; run th completion <bash|zsh|fish|powershell> instead")
	}

	setups := shellSetups()
	names := make([]string, len(setups))
	for i, s := range setups {
		names[i] = s.name
	}
	selected := detectShell(names)
	opts := make([]huh.Option[string], len(setups))
	for i, s := range setups {
		opts[i] = huh.NewOption(s.name, s.name)
	}
	form := huh.NewForm(huh.NewGroup(
		huh.NewSelect[string]().
			Title("Which shell do you use?").
			Description("th will show the line that enables completion and copy it for you.").
			Options(opts...).
			Value(&selected),
	))
	if err := form.Run(); err != nil {
		if errors.Is(err, huh.ErrUserAborted) {
			return errors.New("aborted")
		}
		return err
	}

	var setup shellSetup
	for _, s := range setups {
		if s.name == selected {
			setup = s
		}
	}

	fmt.Printf("\nTo enable th completion for %s, add this line to %s:\n\n", setup.name, setup.rcFile)
	fmt.Println("    " + colorLine(setup.snippet, ansiBold))
	fmt.Println()

	copied := copyToClipboard(setup.snippet) == nil
	if copied {
		fmt.Println("The line has been copied to your clipboard.")
	} else {
		fmt.Println("(No clipboard tool found — copy the line above by hand.)")
	}

	fmt.Println("\nTo finish:")
	if copied {
		fmt.Printf("  1. Open %s and paste the line from your clipboard\n", setup.rcFile)
	} else {
		fmt.Printf("  1. Open %s and add the line shown above\n", setup.rcFile)
	}
	fmt.Printf("  2. Restart your shell, or run: %s\n", setup.reload)
	fmt.Println("  3. Try it: th <TAB>")
	if setup.note != "" {
		fmt.Println("\nNote: " + setup.note)
	}
	return nil
}

// detectShell picks a shell choice from $SHELL; the first name is the
// fallback.
func detectShell(names []string) string {
	shell := os.Getenv("SHELL")
	base := shell[strings.LastIndex(shell, "/")+1:]
	for _, n := range names {
		if n == base {
			return n
		}
	}
	return names[0]
}

// copyToClipboard pipes text to the first clipboard tool found on PATH.
func copyToClipboard(text string) error {
	candidates := [][]string{
		{"pbcopy"},
		{"wl-copy"},
		{"xclip", "-selection", "clipboard"},
		{"xsel", "--clipboard", "--input"},
		{"clip"},
	}
	for _, c := range candidates {
		bin, err := exec.LookPath(c[0])
		if err != nil {
			continue
		}
		cmd := exec.Command(bin, c[1:]...)
		cmd.Stdin = strings.NewReader(text)
		return cmd.Run()
	}
	return errors.New("no clipboard tool found")
}

func init() {
	rootCmd.CompletionOptions.DisableDefaultCmd = true
	rootCmd.AddCommand(completionCmd)
}
