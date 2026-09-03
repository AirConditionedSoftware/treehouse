package cmd

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"

	"github.com/AirConditionedSoftware/treehouse/internal/config"
	"github.com/charmbracelet/huh"
	"golang.org/x/term"
)

// runHook runs the configured commands of one lifecycle hook, in order,
// stopping at the first failure. dir is the working directory — the
// worktree itself for post_create and pre_remove, the main worktree for
// pre_create and post_remove, whose side of the transition has no worktree
// directory to run in. worktreePath is what TH_WORKTREE reports: the
// worktree the operation is about, which for pre_create does not exist yet
// and for post_remove no longer does.
func runHook(hook, dir, worktreePath, mainPath, repo, branch string, cmds []string) error {
	for _, c := range cmds {
		fmt.Fprintf(os.Stderr, "%s: %s\n", hook, c)
		cmd := exec.Command("sh", "-c", c)
		cmd.Dir = dir
		// stdout stays reserved for machine output (th add prints only
		// the created path).
		cmd.Stdout = os.Stderr
		cmd.Stderr = os.Stderr
		cmd.Env = thEnv(worktreePath, mainPath, repo, branch)
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("%s command %q: %w", hook, c, err)
		}
	}
	return nil
}

// thEnv is the environment hook commands and th run execute with: the
// current process environment plus the TH_* worktree metadata variables.
// Metadata travels as environment variables and is never interpolated into
// the command string, since branch names may legally contain shell
// metacharacters like $( ).
func thEnv(worktreePath, mainPath, repo, branch string) []string {
	return append(os.Environ(),
		"TH_WORKTREE="+worktreePath,
		"TH_MAIN="+mainPath,
		"TH_REPO="+repo,
		"TH_BRANCH="+branch,
	)
}

// gateRepoHook filters one hook's commands through the trust gate:
// commands from the user-owned global config pass unasked, commands the
// repository's .thrc supplied run only if approveRepoCommands allows them.
// Unapproved commands come back as an empty list — skipped, never a reason
// to fail the surrounding operation (a committed .thrc must not be able to
// block removing a worktree). The error is the user's own doing: an
// aborted prompt, or a failure recording the approval.
func gateRepoHook(mainPath string, res config.Resolved, hook string, cmds []string) ([]string, error) {
	if len(cmds) == 0 || !res.HookFromRepo(hook) {
		return cmds, nil
	}
	allowed, err := approveRepoCommands(mainPath, res.LocalFile, hook, cmds)
	if err != nil {
		return nil, err
	}
	if !allowed {
		return nil, nil
	}
	return cmds, nil
}

// approveRepoCommands gates hook commands that came from the repository's
// own .thrc instead of the user-owned config file, and reports whether
// they may run. Commands identical to a stored approval run without
// prompting; anything else asks for confirmation and records the answer,
// per hook, so approving one hook never approves another. Without a
// terminal to ask at, the commands are skipped with a warning rather than
// run — the operation itself proceeds either way.
func approveRepoCommands(mainPath, localFile, hook string, cmds []string) (bool, error) {
	approved, hadApproval := config.ApprovedCommands(mainPath, hook)
	if hadApproval && slices.Equal(approved, cmds) {
		return true, nil
	}

	if !term.IsTerminal(int(os.Stdin.Fd())) {
		fmt.Fprintf(os.Stderr, "Warning: %s from %s is not approved; skipping (run %s interactively to review).\n", hook, displayPath(localFile), hookCommandName(hook))
		return false, nil
	}

	title := fmt.Sprintf("%s in %s wants to run commands. Allow them?", hook, displayPath(localFile))
	if hadApproval {
		title = fmt.Sprintf("%s in %s changed. Allow these commands?", hook, displayPath(localFile))
	}
	allow := false
	form := huh.NewForm(huh.NewGroup(
		huh.NewConfirm().
			Title(title).
			Description(diffCommands(approved, cmds)).
			Affirmative("Allow and remember").
			Negative("Skip this time").
			Value(&allow),
	))
	if err := form.Run(); err != nil {
		if errors.Is(err, huh.ErrUserAborted) {
			return false, errors.New("aborted")
		}
		return false, err
	}
	if !allow {
		// Nothing is stored, so the next run asks again.
		fmt.Fprintf(os.Stderr, "Skipped %s (not approved)\n", hook)
		return false, nil
	}
	if err := config.ApproveCommands(mainPath, hook, cmds); err != nil {
		return false, fmt.Errorf("recording the approval failed: %w", err)
	}
	return true, nil
}

// hookCommandName names the th command whose interactive run can review a
// hook's approval, for the non-TTY warning.
func hookCommandName(hook string) string {
	switch {
	case hook == "run":
		return "th run"
	case strings.HasSuffix(hook, "_remove"):
		return "th remove"
	}
	return "th add"
}

// diffCommands renders old against new as an ordered line diff for the
// approval prompt: commands in both are indented, commands only in new are
// marked "+", commands only in old "-". Matches are claimed greedily in
// order, and removals print where they disappeared. A nil old list (a first
// approval) marks every command as added.
func diffCommands(old, new []string) string {
	// match[j] is the index in old that new[j] pairs with, -1 if none.
	match := make([]int, len(new))
	cursor := 0
	for j, cmd := range new {
		match[j] = -1
		for i := cursor; i < len(old); i++ {
			if old[i] == cmd {
				match[j], cursor = i, i+1
				break
			}
		}
	}

	var b strings.Builder
	i := 0
	removeUpTo := func(upto int) {
		for ; i < upto; i++ {
			fmt.Fprintf(&b, "  - %s\n", old[i])
		}
	}
	for j, cmd := range new {
		if match[j] >= 0 {
			removeUpTo(match[j])
			fmt.Fprintf(&b, "    %s\n", cmd)
			i++
			continue
		}
		// An added command: everything of old that cannot appear before
		// the next paired command is gone, so show those removals first.
		upto := len(old)
		for k := j + 1; k < len(new); k++ {
			if match[k] >= 0 {
				upto = match[k]
				break
			}
		}
		removeUpTo(upto)
		fmt.Fprintf(&b, "  + %s\n", cmd)
	}
	removeUpTo(len(old))
	return strings.TrimRight(b.String(), "\n")
}

// diffFileLines renders a file rewrite as the same ordered line diff the
// approval prompt uses — th migrate previews a schema migration with it.
// The greedy matching can pair repeated lines (a JSON file's closing braces)
// with the wrong twin, but for the small documents th owns the result still
// reads as the change that was made; an LCS would buy nothing here.
func diffFileLines(old, new []byte) string {
	return diffCommands(splitDiffLines(old), splitDiffLines(new))
}

// splitDiffLines cuts file content into diffable lines, dropping the trailing
// newline so a well-formed file does not end in a phantom empty line. Empty
// content is no lines at all, which diffCommands reads as "everything added".
func splitDiffLines(b []byte) []string {
	if len(b) == 0 {
		return nil
	}
	return strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")
}
