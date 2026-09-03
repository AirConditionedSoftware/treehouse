package cmd

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/AirConditionedSoftware/treehouse/internal/config"
)

var (
	fullPathsFlag bool // --full-paths
	fullPathsCfg  bool // full_paths from the effective config
)

// applyDisplayConfig applies display-affecting settings once a command has
// resolved its effective config.
func applyDisplayConfig(s config.Settings) {
	fullPathsCfg = s.FullPathsEnabled()
}

func fullPathsEnabled() bool {
	return fullPathsFlag || fullPathsCfg
}

// displayPath abbreviates the user's home directory to ~ for human-facing
// output (tables, prompts, stderr messages) unless full paths are requested
// via --full-paths or the full_paths config setting. Machine-facing output —
// the path th add prints on stdout, --json — keeps full paths.
func displayPath(p string) string {
	if fullPathsEnabled() {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" || home == "/" {
		return p
	}
	sep := string(os.PathSeparator)
	for _, h := range homeVariants(home) {
		if p == h {
			return "~"
		}
		if rest, ok := strings.CutPrefix(p, h+sep); ok {
			return "~" + sep + rest
		}
	}
	return p
}

// homeVariants returns the home directory as configured and with symlinks
// resolved — git reports resolved paths, so both spellings must match.
func homeVariants(home string) []string {
	vs := []string{home}
	if r, err := filepath.EvalSymlinks(home); err == nil && r != home {
		vs = append(vs, r)
	}
	return vs
}
