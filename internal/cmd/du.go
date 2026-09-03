package cmd

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/AirConditionedSoftware/treehouse/internal/config"
	"github.com/AirConditionedSoftware/treehouse/internal/gitx"
	"github.com/spf13/cobra"
)

var duUnit string

var duCmd = &cobra.Command{
	Use:     "du",
	Aliases: []string{"disk"},
	Short:   "Show disk usage of each worktree",
	Long: `Show the disk space used by each worktree of the current repository,
largest first, with a total. Sizes count the working files; the repository
database inside .git is not attributed to any worktree.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		unit, err := normalizeUnit(duUnit)
		if err != nil {
			return err
		}
		wts, err := gitx.ListWorktrees(".")
		if err != nil {
			return err
		}
		if res, err := config.Resolve(wts[0].Path); err == nil {
			applyDisplayConfig(res.Settings)
		}

		type row struct {
			wt   gitx.Worktree
			size int64
			err  error
		}
		rows := make([]row, len(wts))
		for i, w := range wts {
			size, err := dirSize(w.Path)
			rows[i] = row{w, size, err}
		}
		sort.SliceStable(rows, func(i, j int) bool { return rows[i].size > rows[j].size })

		var buf bytes.Buffer
		tw := tabwriter.NewWriter(&buf, 0, 0, 2, ' ', 0)
		styles := []string{ansiBold}
		fmt.Fprintln(tw, "BRANCH\tPATH\tSIZE")
		var total int64
		for _, r := range rows {
			branch := r.wt.Branch
			switch {
			case r.wt.Bare:
				branch = "(bare)"
			case r.wt.Detached:
				branch = "(detached)"
			}
			sizeStr := "?"
			if r.err == nil {
				sizeStr = formatSize(r.size, unit)
				total += r.size
			}
			styles = append(styles, "")
			fmt.Fprintf(tw, "%s\t%s\t%s\n", branch, displayPath(r.wt.Path), sizeStr)
		}
		styles = append(styles, ansiBold)
		fmt.Fprintf(tw, "TOTAL\t\t%s\n", formatSize(total, unit))
		if err := tw.Flush(); err != nil {
			return err
		}
		return printStyled(os.Stdout, buf.String(), styles)
	},
}

func normalizeUnit(u string) (string, error) {
	u = strings.ToLower(u)
	switch u {
	case "", "kb", "mb", "gb":
		return u, nil
	}
	return "", fmt.Errorf("invalid unit %q: use KB, MB, or GB", u)
}

func formatSize(n int64, unit string) string {
	switch unit {
	case "kb":
		return fmt.Sprintf("%.1f KB", float64(n)/(1<<10))
	case "mb":
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case "gb":
		return fmt.Sprintf("%.2f GB", float64(n)/(1<<30))
	}
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.2f GB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%d B", n)
	}
}

// dirSize sums the file sizes under root, skipping the .git entry at the
// root (the repo database in the main worktree, the gitdir pointer file in
// linked ones). Unreadable entries are skipped rather than failing the walk.
func dirSize(root string) (int64, error) {
	if _, err := os.Stat(root); err != nil {
		return 0, err
	}
	gitEntry := filepath.Join(root, ".git")
	var total int64
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if p == root {
				return err
			}
			return nil
		}
		if p == gitEntry {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		total += info.Size()
		return nil
	})
	return total, err
}

func init() {
	duCmd.Flags().StringVarP(&duUnit, "unit", "u", "", "display sizes in KB, MB, or GB (default: auto per row)")
	rootCmd.AddCommand(duCmd)
}
