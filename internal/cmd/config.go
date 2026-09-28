package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
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

var (
	configEffective bool
	configJSON      bool
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Print the config file location and content",
	Long: `Print the config file location and its content. The location goes to stderr
and the content to stdout, so th config | jq works. If no file exists at the
default location, the built-in defaults are printed instead.

With --effective, print instead the fully merged settings for the current
repository and the layer each value came from: built-in defaults, the config
file's top-level settings, its matching repos entry, and the repo's .thrc,
layered in that order. --json turns that table into one JSON document.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if configJSON && !configEffective {
			return errors.New("--json requires --effective (th config already prints JSON)")
		}
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
			return printJSON(def)
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
// merge. The layer preamble goes to stderr, the table — or, with --json, one
// document — to stdout.
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

	_, statErr := os.Stat(path)
	exists := statErr == nil
	if os.IsNotExist(statErr) && !explicit {
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

	repoName := ""
	if mainPath != "" {
		if repoName = res.RepoName; repoName == "" {
			repoName = filepath.Base(mainPath)
		}
	}

	if configJSON {
		report := effectiveReport{
			ConfigFile: effectiveConfigFile{Path: path, Exists: exists, FromEnv: explicit},
			Settings:   effectiveSettings(res),
			Sources:    effectiveSources(prov),
		}
		// Outside a repository the repo layers never applied, so there is
		// nothing to report about one.
		if mainPath != "" {
			report.Repo = &effectiveRepo{
				Name:       repoName,
				MainPath:   mainPath,
				LocalFile:  res.LocalFile,
				ReposIndex: prov.ReposIndex,
				ReposPath:  prov.ReposPath,
			}
		}
		return printJSON(report)
	}

	var buf bytes.Buffer
	tw := tabwriter.NewWriter(&buf, 0, 0, 2, ' ', 0)
	styles := []string{ansiBold}
	fmt.Fprintln(tw, "SETTING\tVALUE\tSOURCE")
	for _, r := range effectiveRows(res, prov, repoName) {
		styles = append(styles, sourceStyle(r.source))
		fmt.Fprintf(tw, "%s\t%s\t%s\n", r.name, r.value, r.source)
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	return printStyled(os.Stdout, buf.String(), styles)
}

// effectiveFieldNames lists every setting the effective view reports, in
// display order, under the dotted name the table's SETTING column and the
// --json document's sources object share. One list, so the two renderings
// cannot drift apart (TestEffectiveViewsAgree).
var effectiveFieldNames = []string{
	"worktree_dir",
	"default_base",
	"branch_prefix",
	"prefix_separator",
	"copy_hooks",
	"copy_files",
	"link_files",
	"vscode.open",
	"vscode.workspace_file",
	"vscode.workspace_prefix",
	"vscode.window_title",
	"vscode.window_color",
	"vscode.workspace_paths",
	"full_paths",
	"auto_cd",
	"pre_create",
	"post_create",
	"pre_remove",
	"post_remove",
	"run",
}

// effectiveConfigFile locates the global config file the merge started from.
type effectiveConfigFile struct {
	Path    string `json:"path"`
	Exists  bool   `json:"exists"`
	FromEnv bool   `json:"from_env"`
}

// effectiveRepo identifies the repository whose layers were applied.
type effectiveRepo struct {
	Name       string `json:"name"`
	MainPath   string `json:"main_path"`
	LocalFile  string `json:"local_file,omitempty"`
	ReposIndex int    `json:"repos_index"`
	ReposPath  string `json:"repos_path,omitempty"`
}

// effectiveReport is the --effective --json document: where the settings
// came from, what they resolved to, and which layer set each one.
type effectiveReport struct {
	ConfigFile effectiveConfigFile `json:"config_file"`
	Repo       *effectiveRepo      `json:"repo,omitempty"`
	Settings   map[string]any      `json:"settings"`
	Sources    map[string]string   `json:"sources"`
}

type effectiveRow struct{ name, value, source string }

// effectiveRows renders the settings for the table, one row per
// effectiveFieldNames entry, led by the repo name inside a repository.
func effectiveRows(res config.Resolved, prov config.Provenance, repoName string) []effectiveRow {
	display := effectiveDisplay(res)
	rows := make([]effectiveRow, 0, len(effectiveFieldNames)+1)
	if repoName != "" {
		rows = append(rows, effectiveRow{"name", repoName, prov.Source("name")})
	}
	for _, name := range effectiveFieldNames {
		rows = append(rows, effectiveRow{name, display[name], prov.Source(name)})
	}
	return rows
}

// effectiveDisplay renders every setting as the table shows it, keyed by
// dotted name.
func effectiveDisplay(res config.Resolved) map[string]string {
	vs := res.VSCodeSettings()
	return map[string]string{
		"worktree_dir":            res.WorktreeDir,
		"default_base":            effectiveString(res.DefaultBase),
		"branch_prefix":           effectiveString(res.BranchPrefix),
		"prefix_separator":        effectiveSeparator(res),
		"copy_hooks":              strconv.FormatBool(res.CopyHooksEnabled()),
		"copy_files":              effectiveList(res.CopyFiles),
		"link_files":              effectiveList(res.LinkFiles),
		"vscode.open":             strconv.FormatBool(res.VSCodeOpenEnabled()),
		"vscode.workspace_file":   strconv.FormatBool(res.VSCodeWorkspaceFileEnabled()),
		"vscode.workspace_prefix": effectiveString(vs.WorkspacePrefix),
		"vscode.window_title":     effectiveString(vs.WindowTitle),
		"vscode.window_color":     effectiveString(vs.WindowColor),
		"vscode.workspace_paths":  effectiveList(vs.WorkspacePaths),
		"full_paths":              strconv.FormatBool(res.FullPathsEnabled()),
		"auto_cd":                 strconv.FormatBool(res.AutoCDEnabled()),
		"pre_create":              effectiveList(res.PreCreate),
		"post_create":             effectiveList(res.PostCreate),
		"pre_remove":              effectiveList(res.PreRemove),
		"post_remove":             effectiveList(res.PostRemove),
		"run":                     effectiveString(res.Run),
	}
}

// effectiveSettings builds the --json settings object: the same values as
// the table, typed, and nested under "vscode" the way the config file nests
// them. Every key is present with a concrete value — pointers resolved, no
// nulls — so a consumer never has to re-apply th's defaulting rules.
func effectiveSettings(res config.Resolved) map[string]any {
	vs := res.VSCodeSettings()
	return map[string]any{
		"worktree_dir":     res.WorktreeDir,
		"default_base":     res.DefaultBase,
		"branch_prefix":    res.BranchPrefix,
		"prefix_separator": effectiveSeparator(res),
		"copy_hooks":       res.CopyHooksEnabled(),
		"copy_files":       orEmpty(res.CopyFiles),
		"link_files":       orEmpty(res.LinkFiles),
		"vscode": map[string]any{
			"open":             res.VSCodeOpenEnabled(),
			"workspace_file":   res.VSCodeWorkspaceFileEnabled(),
			"workspace_prefix": vs.WorkspacePrefix,
			"window_title":     vs.WindowTitle,
			"window_color":     vs.WindowColor,
			"workspace_paths":  orEmpty(vs.WorkspacePaths),
		},
		"full_paths":  res.FullPathsEnabled(),
		"auto_cd":     res.AutoCDEnabled(),
		"pre_create":  orEmpty(res.PreCreate),
		"post_create": orEmpty(res.PostCreate),
		"pre_remove":  orEmpty(res.PreRemove),
		"post_remove": orEmpty(res.PostRemove),
		"run":         res.Run,
	}
}

// effectiveSources maps every reported setting to the layer that set it.
func effectiveSources(prov config.Provenance) map[string]string {
	sources := make(map[string]string, len(effectiveFieldNames))
	for _, name := range effectiveFieldNames {
		sources[name] = prov.Source(name)
	}
	return sources
}

// effectiveSeparator applies prefix_separator's lazy default (see
// EffectivePrefix), so the effective view shows the value that would
// actually join.
func effectiveSeparator(res config.Resolved) string {
	if res.PrefixSeparator == "" {
		return config.DefaultPrefixSeparator
	}
	return res.PrefixSeparator
}

// orEmpty keeps a list setting's JSON an empty array rather than null.
func orEmpty[T any](list []T) []T {
	if list == nil {
		return []T{}
	}
	return list
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
	configCmd.Flags().BoolVar(&configJSON, "json", false, "with --effective: output the merged settings and their sources as JSON")
	rootCmd.AddCommand(configCmd)
}
