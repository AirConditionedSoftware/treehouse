package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/AirConditionedSoftware/treehouse/internal/config"
	"github.com/AirConditionedSoftware/treehouse/internal/gitx"
	"github.com/spf13/cobra"
)

var (
	initName          string
	initWorktreeDir   string
	initBase          string
	initPrefix        string
	initSeparator     string
	initCopyFile      []string
	initPostCreate    []string
	initCopyHooks     bool
	initOpen          bool
	initWorkspaceFile bool
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Create a repo-local .thrc",
	Long: `Create a starter .thrc at the root of the repository's main worktree —
even when run from a linked worktree, since that is where th reads it from.
The file holds per-repo settings that override the global config and its
repos entry; it can be committed to share with a team.

Flags pre-fill fields in the created file; fields whose flag is not given
are left out, so the file stays a small starting point to edit:

  th init --prefix peter --separator - --base develop

The created path is the only output on stdout, so $EDITOR "$(th init)"
opens it directly.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		wts, err := gitx.ListWorktrees(".")
		if err != nil {
			return err
		}
		mainPath := wts[0].Path
		path := filepath.Join(mainPath, config.LocalFileName)
		if _, err := os.Stat(path); err == nil {
			return fmt.Errorf("%s already exists; edit it directly", displayPath(path))
		}

		name := initName
		if name == "" {
			name = filepath.Base(mainPath)
		}
		// The vscode object is allocated only when a flag asked for one of its
		// fields, so a flagless init writes no "vscode" key at all.
		open := flagBool(cmd, "open", &initOpen)
		wsFile := flagBool(cmd, "workspace-file", &initWorkspaceFile)
		var vs *config.VSCode
		if open != nil || wsFile != nil {
			vs = &config.VSCode{Open: open, WorkspaceFile: wsFile}
		}
		starter := config.LocalConfig{
			Version: config.CurrentLocalVersion(),
			Name:    name,
			Settings: config.Settings{
				WorktreeDir:     initWorktreeDir,
				DefaultBase:     initBase,
				BranchPrefix:    initPrefix,
				PrefixSeparator: initSeparator,
				CopyFiles:       initCopyFile,
				PostCreate:      initPostCreate,
				CopyHooks:       flagBool(cmd, "copy-hooks", &initCopyHooks),
				VSCode:          vs,
			},
		}
		// The omitempty tags keep unflagged fields out of the file, so a
		// flagless init still writes just the name.
		data, err := json.MarshalIndent(starter, "", "  ")
		if err != nil {
			return err
		}
		if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
			return err
		}
		// Prove the starter loads through the real resolver.
		if _, err := config.Resolve(mainPath); err != nil {
			return err
		}

		fmt.Fprintf(os.Stderr, "Created %s\n", displayPath(path))
		fmt.Fprintln(os.Stderr, "Its values override the global config and its repos entry for this repo.")
		fmt.Fprintln(os.Stderr, "Fields: worktree_dir, default_base, branch_prefix, prefix_separator,")
		fmt.Fprintln(os.Stderr, "  copy_hooks, copy_files, full_paths, post_create, and the vscode")
		fmt.Fprintln(os.Stderr, "  object — see the README's Configuration section.")
		fmt.Fprintln(os.Stderr, "If committed, post_create commands from this file need one-time approval on th add.")
		fmt.Println(path)
		return nil
	},
}

// flagBool returns v when the named bool flag was passed and nil otherwise,
// so a flag left alone keeps its field out of the scaffolded file rather than
// writing a false that would override the global config.
func flagBool(cmd *cobra.Command, name string, v *bool) *bool {
	if !cmd.Flags().Changed(name) {
		return nil
	}
	return v
}

func init() {
	initCmd.Flags().StringVar(&initName, "name", "", "repo name for {repo} in templates (default: the main worktree's directory name)")
	initCmd.Flags().StringVar(&initWorktreeDir, "worktree-dir", "", "worktree_dir path template, e.g. ~/worktrees/{repo}/{branch}")
	initCmd.Flags().StringVar(&initBase, "base", "", "default_base ref for newly created branches")
	initCmd.Flags().StringVar(&initPrefix, "prefix", "", "branch_prefix prepended to newly created branches")
	initCmd.Flags().StringVar(&initSeparator, "separator", "", "prefix_separator joining the prefix to the branch name")
	initCmd.Flags().StringArrayVar(&initCopyFile, "copy-file", nil, "copy_files entry: file or glob to copy into new worktrees; repeatable")
	initCmd.Flags().StringArrayVar(&initPostCreate, "post-create", nil, "post_create entry: command to run in new worktrees; repeatable")
	initCmd.Flags().BoolVar(&initCopyHooks, "copy-hooks", false, "set copy_hooks: copy the repo's git hooks into new worktrees")
	initCmd.Flags().BoolVar(&initOpen, "open", false, "set vscode.open: open new worktrees in VS Code")
	initCmd.Flags().BoolVar(&initWorkspaceFile, "workspace-file", false, "set vscode.workspace_file: write a .code-workspace file per worktree")
	rootCmd.AddCommand(initCmd)
}
