package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/AirConditionedSoftware/treehouse/internal/config"
	"github.com/AirConditionedSoftware/treehouse/internal/gitx"
	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var (
	migrateGlobal   bool
	migrateDryRun   bool
	migrateYes      bool
	migrateBackup   bool
	migrateNoBackup bool
)

var migrateCmd = &cobra.Command{
	Use:   "migrate",
	Short: "Update .thrc (and with --global the config file) to the current schema",
	Long: `Bring this repository's .thrc up to the config schema version th uses now,
and with --global the global config file too. Migrating is otherwise
passive: the global file rewrites itself whenever th loads it, and a .thrc
waits for a command that needs it to offer the update in a terminal.
th migrate is how to do it deliberately, one repository at a time.

Each file asks twice — whether to update it, then whether to keep a copy
of the old one beside it. --yes answers the first, --backup or --no-backup
the second, so scripts and machines without a terminal can run the update
unattended; without a terminal to ask at, a missing answer is an error
naming the flag that would have supplied it. The global config is always
backed up, exactly as when th migrates it on its own, so the backup flags
do not apply to it.

--dry-run reports what each file would become — the versions, the backup
it would leave, and a line diff of the rewrite — and changes nothing. All
of th migrate's output goes to stderr; stdout stays empty.`,
	Args: cobra.NoArgs,
	RunE: runMigrate,
}

// migrateTarget is one file with a migration outstanding. global files are
// backed up unconditionally, as Load does when it migrates one behind the
// user's back, so the backup flags and the backup prompt only ever concern
// a .thrc.
type migrateTarget struct {
	pending *config.PendingMigration
	global  bool
}

// runMigrate collects the files with a pending schema migration, global
// first — it is the layer a .thrc overrides — and rewrites each one after
// the answers it needs are in.
//
// It deliberately goes through config's read-only inspection API instead of
// Load or Resolve: loading migrates an out-of-date global file and rewrites
// it as a side effect, which would make --dry-run a lie and a plain
// th migrate touch the global file nobody asked it to. The cost is that the
// full_paths setting lives behind Load and so is not honored here; --full-paths
// still is.
func runMigrate(cmd *cobra.Command, args []string) error {
	var targets []migrateTarget

	if migrateGlobal {
		p, path, exists, err := config.PendingGlobalMigration()
		if err != nil {
			return err
		}
		switch {
		case !exists:
			fmt.Fprintf(os.Stderr, "no global config file at %s; nothing to migrate\n", displayPath(path))
		case p == nil:
			fmt.Fprintf(os.Stderr, "%s is already at config schema v%d\n", displayPath(path), config.CurrentGlobalVersion())
		default:
			targets = append(targets, migrateTarget{pending: p, global: true})
		}
	}

	wts, err := gitx.ListWorktrees(".")
	if err != nil {
		if !migrateGlobal {
			return err
		}
		// With --global there is still a file to migrate, so being outside a
		// repository is a note, not a failure (as in th config --effective).
		fmt.Fprintln(os.Stderr, "not inside a git repository; migrating the global config only")
	} else {
		mainPath := wts[0].Path
		p, path, exists, err := config.PendingLocalMigration(mainPath)
		if err != nil {
			return err
		}
		switch {
		case !exists:
			fmt.Fprintf(os.Stderr, "no %s in %s; nothing to migrate (th init creates one)\n", config.LocalFileName, displayPath(mainPath))
		case p == nil:
			fmt.Fprintf(os.Stderr, "%s is already at config schema v%d\n", displayPath(path), config.CurrentLocalVersion())
		default:
			targets = append(targets, migrateTarget{pending: p})
		}
	}

	// Nothing outstanding is a success, with or without flags: there is no
	// answer to miss, so a bare non-TTY th migrate on an up-to-date
	// repository exits 0 and --backup/--no-backup are a harmless no-op.
	if len(targets) == 0 {
		return nil
	}

	if migrateDryRun {
		for _, t := range targets {
			p := t.pending
			fmt.Fprintf(os.Stderr, "%s: config schema v%d -> v%d (dry run)\n", displayPath(p.Path), p.From, p.To)
			if !t.global && migrateNoBackup {
				fmt.Fprintln(os.Stderr, "  no backup (--no-backup)")
			} else {
				fmt.Fprintf(os.Stderr, "  backup would be %s.v%d.<timestamp>.bak beside it\n", filepath.Base(p.Path), p.From)
			}
			fmt.Fprintln(os.Stderr, diffFileLines(p.Original, p.Migrated))
		}
		return nil
	}

	// Stricter than a stdin-only check, like finalizeLocalMigration: a
	// prompt is useless when the output it prints on is being captured. The
	// answers are checked for every target before the first write, so a
	// --global run that cannot finish never half-finishes either.
	if !term.IsTerminal(int(os.Stdin.Fd())) || !term.IsTerminal(int(os.Stdout.Fd())) {
		if err := requireMigrateAnswers(targets); err != nil {
			return err
		}
	}

	for _, t := range targets {
		p := t.pending
		base := filepath.Base(p.Path)

		// Flags pre-answer the prompts on a terminal too, so a scripted
		// invocation behaves the same wherever it runs.
		if !migrateYes {
			update := false
			detail := fmt.Sprintf("If the %s is committed, the update shows up as a modification in git.", config.LocalFileName)
			if t.global {
				detail = "The file as it is now is kept as a backup beside it."
			}
			form := huh.NewForm(huh.NewGroup(
				huh.NewConfirm().
					Title(fmt.Sprintf("Update %s to config schema v%d?", base, p.To)).
					Description(fmt.Sprintf("%s is schema v%d; th now uses v%d.\n%s", displayPath(p.Path), p.From, p.To, detail)).
					Affirmative("Update").
					Negative("Skip").
					Value(&update),
			))
			if err := form.Run(); err != nil {
				if errors.Is(err, huh.ErrUserAborted) {
					// Nothing was written, so the offer stands next run.
					return errors.New("aborted")
				}
				return err
			}
			if !update {
				// A declined file is a choice, not a failure: the rest of
				// the run carries on and th migrate still exits 0.
				fmt.Fprintf(os.Stderr, "Skipped %s (still schema v%d)\n", displayPath(p.Path), p.From)
				continue
			}
		}

		// The global file is always backed up; only a .thrc gets a say.
		backup := true
		if !t.global {
			switch {
			case migrateBackup:
				backup = true
			case migrateNoBackup:
				backup = false
			default:
				backup = false
				form := huh.NewForm(huh.NewGroup(
					huh.NewConfirm().
						Title(fmt.Sprintf("Keep a copy of the old %s?", base)).
						Description(fmt.Sprintf("The backup would be %s.v%d.<timestamp>.bak beside it.", base, p.From)).
						Affirmative("Back up, then update").
						Negative("Update without backup").
						Value(&backup),
				))
				if err := form.Run(); err != nil {
					if errors.Is(err, huh.ErrUserAborted) {
						return errors.New("aborted")
					}
					return err
				}
			}
		}

		backupPath := ""
		if backup {
			var err error
			// The file is still untouched, so a failed backup costs nothing
			// but the command.
			if backupPath, err = p.WriteBackup(); err != nil {
				return err
			}
		}
		if err := p.Persist(); err != nil {
			return err
		}
		msg := fmt.Sprintf("Updated %s to config schema v%d", displayPath(p.Path), p.To)
		if backupPath != "" {
			msg += fmt.Sprintf(" (backup: %s)", displayPath(backupPath))
		}
		fmt.Fprintln(os.Stderr, msg)
	}
	return nil
}

// requireMigrateAnswers reports whether the flags already answer every
// question the pending targets would raise, as one error naming exactly the
// flags that are missing. Without a terminal there is nobody to ask, and
// guessing at a rewrite of a file the user may have committed is not th's
// call to make.
func requireMigrateAnswers(targets []migrateTarget) error {
	local := false
	for _, t := range targets {
		if !t.global {
			local = true
		}
	}
	needBackup := local && !migrateBackup && !migrateNoBackup
	if migrateYes && !needBackup {
		return nil
	}

	var paths, asks []string
	for _, t := range targets {
		paths = append(paths, displayPath(t.pending.Path))
	}
	if !migrateYes {
		ask := "--yes to confirm the update"
		if !local {
			ask += " (the global config is backed up automatically)"
		}
		asks = append(asks, ask)
	}
	if needBackup {
		asks = append(asks, "--backup or --no-backup to decide the backup")
	}
	return fmt.Errorf("updating %s needs answers there is no terminal to ask for: pass %s, or preview with --dry-run",
		strings.Join(paths, " and "), strings.Join(asks, " and "))
}

func init() {
	migrateCmd.Flags().BoolVarP(&migrateGlobal, "global", "g", false, "also update the global config file (always backed up)")
	migrateCmd.Flags().BoolVarP(&migrateDryRun, "dry-run", "n", false, "show what would change, with a diff, without writing anything")
	migrateCmd.Flags().BoolVar(&migrateYes, "yes", false, "update without asking for confirmation")
	migrateCmd.Flags().BoolVar(&migrateBackup, "backup", false, "keep a copy of the old .thrc beside it, without asking")
	migrateCmd.Flags().BoolVar(&migrateNoBackup, "no-backup", false, "update the .thrc without keeping a copy")
	migrateCmd.MarkFlagsMutuallyExclusive("backup", "no-backup")
	rootCmd.AddCommand(migrateCmd)
}

// finalizeLocalMigration writes back a .thrc that was migrated in memory,
// after offering to keep a copy of the old file. The settings this run uses
// are already migrated either way — the only question is what lands on
// disk. A no-op when the file was already at the current schema.
//
// Only interactive hard-fail commands call it. The .thrc belongs to the
// repository and may well be committed, so rewriting it is the user's call
// and happens only where there is a terminal to ask at.
func finalizeLocalMigration(res config.Resolved) error {
	p := res.LocalMigration
	if p == nil {
		return nil
	}

	// Stricter than the hook approval gate, which only checks stdin: stdout
	// must be a terminal too. cd "$(th cd b)" runs with a TTY stdin but a
	// captured stdout, and there the shell is blocked waiting for a path
	// while a prompt asks about something else entirely. A schema update
	// can wait for a run whose output nobody is capturing.
	if !term.IsTerminal(int(os.Stdin.Fd())) || !term.IsTerminal(int(os.Stdout.Fd())) {
		fmt.Fprintf(os.Stderr, "Note: %s uses config schema v%d (th now uses v%d); using it as v%d for this run. Run th migrate to update the file.\n",
			displayPath(p.Path), p.From, p.To, p.To)
		return nil
	}

	backup := false
	form := huh.NewForm(huh.NewGroup(
		huh.NewConfirm().
			Title(fmt.Sprintf("%s needs a one-time update to config schema v%d. Back it up first?", config.LocalFileName, p.To)).
			Description(fmt.Sprintf("%s is schema v%d; th now uses v%d.\nThe backup would be %s beside it.\nIf the %s is committed, the update shows up as a modification in git.",
				displayPath(p.Path), p.From, p.To,
				fmt.Sprintf("%s.v%d.<timestamp>.bak", filepath.Base(p.Path), p.From),
				config.LocalFileName)).
			Affirmative("Back up, then update").
			Negative("Update without backup").
			Value(&backup),
	))
	if err := form.Run(); err != nil {
		if errors.Is(err, huh.ErrUserAborted) {
			// Nothing was written, so the offer stands next run.
			return errors.New("aborted")
		}
		return err
	}

	backupPath := ""
	if backup {
		var err error
		// The .thrc is still untouched, so a failed backup costs nothing
		// but the command.
		if backupPath, err = p.WriteBackup(); err != nil {
			return err
		}
	}
	if err := p.Persist(); err != nil {
		return err
	}
	msg := fmt.Sprintf("Updated %s to config schema v%d", displayPath(p.Path), p.To)
	if backupPath != "" {
		msg += fmt.Sprintf(" (backup: %s)", displayPath(backupPath))
	}
	fmt.Fprintln(os.Stderr, msg)
	return nil
}
