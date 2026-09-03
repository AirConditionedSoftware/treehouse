package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/AirConditionedSoftware/treehouse/internal/config"
	"github.com/AirConditionedSoftware/treehouse/internal/gitx"
	"github.com/spf13/cobra"
)

var configEffective bool

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Print the config file location and content",
	Long: `Print the config file location and its content. The location goes to stderr
and the content to stdout, so th config | jq works. If no file exists at the
default location, the built-in defaults are printed instead.

With --effective, print instead the fully merged settings for the current
repository and the layer each value came from: built-in defaults, the config
file's top-level settings, its matching repos entry, and the repo's .thrc,
layered in that order.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if configEffective {
			return runConfigEffective()
		}
		path, explicit, err := config.Path()
		if err != nil {
			return err
		}
		src := "default location"
		if explicit {
			src = "from $" + config.EnvVar
		}

		data, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			if explicit {
				return fmt.Errorf("config %s (%s) does not exist", displayPath(path), src)
			}
			fmt.Fprintf(os.Stderr, "%s (%s) does not exist; built-in defaults apply:\n", displayPath(path), src)
			def := config.File{
				Version:  config.CurrentGlobalVersion(),
				Settings: config.Settings{WorktreeDir: config.DefaultWorktreeDir},
			}
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(def)
		}
		if err != nil {
			return err
		}

		fmt.Fprintf(os.Stderr, "%s (%s)\n", displayPath(path), src)
		os.Stdout.Write(data)
		if len(data) > 0 && data[len(data)-1] != '\n' {
			fmt.Println()
		}

		// A repo's own .thrc is part of its effective config, so print it
		// as a second document (the stdout stream stays jq-parseable).
		// Outside a repository there is nothing to look for.
		if wts, err := gitx.ListWorktrees("."); err == nil && len(wts) > 0 {
			mainPath := wts[0].Path
			localPath := filepath.Join(mainPath, config.LocalFileName)
			if local, err := os.ReadFile(localPath); err == nil {
				fmt.Fprintf(os.Stderr, "%s (repo-local)\n", displayPath(localPath))
				os.Stdout.Write(local)
				if len(local) > 0 && local[len(local)-1] != '\n' {
					fmt.Println()
				}
				// Resolve parses the repo-local file, so th config validates
				// it just like the global one.
				res, err := config.Resolve(mainPath)
				if err != nil {
					return err
				}
				// th config doubles as the repair command: it is where an
				// out-of-date .thrc gets offered the update.
				if err := finalizeLocalMigration(res); err != nil {
					return err
				}
			}
		}

		// Surface parse errors so th config doubles as a validity check.
		if _, err := config.Load(); err != nil {
			return err
		}
		return nil
	},
}

// runConfigEffective prints the merged settings for the current repository
// with the source layer of each value — the debuggable view of the per-repo
// merge. The layer preamble goes to stderr, the table to stdout.
func runConfigEffective() error {
	path, explicit, err := config.Path()
	if err != nil {
		return err
	}
	src := "default location"
	if explicit {
		src = "from $" + config.EnvVar
	}

	// Outside a repository the repo layers don't apply, but the global
	// merge is still worth debugging.
	mainPath := ""
	if wts, err := gitx.ListWorktrees("."); err == nil && len(wts) > 0 {
		mainPath = wts[0].Path
	}

	var (
		res  config.Resolved
		prov config.Provenance
	)
	if mainPath != "" {
		res, prov, err = config.ResolveDetailed(mainPath)
	} else {
		res, prov, err = config.ResolveGlobal()
	}
	if err != nil {
		return err
	}
	if err := finalizeLocalMigration(res); err != nil {
		return err
	}
	applyDisplayConfig(res.Settings)

	if _, statErr := os.Stat(path); os.IsNotExist(statErr) && !explicit {
		fmt.Fprintf(os.Stderr, "%s (%s) does not exist; built-in defaults apply\n", displayPath(path), src)
	} else {
		fmt.Fprintf(os.Stderr, "%s (%s)\n", displayPath(path), src)
	}
	if mainPath == "" {
		fmt.Fprintln(os.Stderr, "not inside a git repository; showing global settings only")
	} else if prov.ReposIndex >= 0 {
		fmt.Fprintf(os.Stderr, "repos[%d] matches (%s)\n", prov.ReposIndex, displayPath(prov.ReposPath))
	} else {
		fmt.Fprintf(os.Stderr, "no repos entry matches %s\n", displayPath(mainPath))
	}
	if res.LocalFile != "" {
		fmt.Fprintf(os.Stderr, "%s (repo-local)\n", displayPath(res.LocalFile))
	}

	// prefix_separator applies its default lazily (see EffectivePrefix), so
	// the effective view shows the value that would actually join.
	sep := res.PrefixSeparator
	if sep == "" {
		sep = config.DefaultPrefixSeparator
	}
	type row struct{ name, value, source string }
	vs := res.VSCodeSettings()
	rows := []row{
		{"worktree_dir", res.WorktreeDir, prov.Source("worktree_dir")},
		{"default_base", effectiveString(res.DefaultBase), prov.Source("default_base")},
		{"branch_prefix", effectiveString(res.BranchPrefix), prov.Source("branch_prefix")},
		{"prefix_separator", sep, prov.Source("prefix_separator")},
		{"copy_hooks", strconv.FormatBool(res.CopyHooksEnabled()), prov.Source("copy_hooks")},
		{"copy_files", effectiveList(res.CopyFiles), prov.Source("copy_files")},
		{"link_files", effectiveList(res.LinkFiles), prov.Source("link_files")},
		{"vscode.open", strconv.FormatBool(res.VSCodeOpenEnabled()), prov.Source("vscode.open")},
		{"vscode.workspace_file", strconv.FormatBool(res.VSCodeWorkspaceFileEnabled()), prov.Source("vscode.workspace_file")},
		{"vscode.workspace_prefix", effectiveString(vs.WorkspacePrefix), prov.Source("vscode.workspace_prefix")},
		{"vscode.window_title", effectiveString(vs.WindowTitle), prov.Source("vscode.window_title")},
		{"vscode.window_color", effectiveString(vs.WindowColor), prov.Source("vscode.window_color")},
		{"vscode.workspace_paths", effectiveList(vs.WorkspacePaths), prov.Source("vscode.workspace_paths")},
		{"full_paths", strconv.FormatBool(res.FullPathsEnabled()), prov.Source("full_paths")},
		{"auto_cd", strconv.FormatBool(res.AutoCDEnabled()), prov.Source("auto_cd")},
		{"pre_create", effectiveList(res.PreCreate), prov.Source("pre_create")},
		{"post_create", effectiveList(res.PostCreate), prov.Source("post_create")},
		{"pre_remove", effectiveList(res.PreRemove), prov.Source("pre_remove")},
		{"post_remove", effectiveList(res.PostRemove), prov.Source("post_remove")},
		{"run", effectiveString(res.Run), prov.Source("run")},
	}
	if mainPath != "" {
		name := res.RepoName
		if name == "" {
			name = filepath.Base(mainPath)
		}
		rows = append([]row{{"name", name, prov.Source("name")}}, rows...)
	}

	var buf bytes.Buffer
	tw := tabwriter.NewWriter(&buf, 0, 0, 2, ' ', 0)
	styles := []string{ansiBold}
	fmt.Fprintln(tw, "SETTING\tVALUE\tSOURCE")
	for _, r := range rows {
		styles = append(styles, sourceStyle(r.source))
		fmt.Fprintf(tw, "%s\t%s\t%s\n", r.name, r.value, r.source)
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	return printStyled(os.Stdout, buf.String(), styles)
}

// sourceStyle colors a row by the layer that set it, so a glance separates
// config-driven values from defaults: defaults gray, repos-entry cyan,
// .thrc green; top-level plain.
func sourceStyle(source string) string {
	switch {
	case source == config.SourceDefault:
		return ansiGray
	case source == config.SourceLocal:
		return ansiGreen
	case strings.HasPrefix(source, "repos["):
		return ansiCyan
	}
	return ""
}

func effectiveString(s string) string {
	if s == "" {
		return "(unset)"
	}
	return s
}

// effectiveList renders a list setting: (none) when never set, JSON
// otherwise — so an explicit clearing [] stays visible.
func effectiveList[T any](list []T) string {
	if list == nil {
		return "(none)"
	}
	data, err := json.Marshal(list)
	if err != nil {
		return "?"
	}
	return string(data)
}

func init() {
	configCmd.Flags().BoolVar(&configEffective, "effective", false, "show the merged settings for the current repository and where each value came from")
	rootCmd.AddCommand(configCmd)
}
