package cmd

import (
	"fmt"
	"hash/fnv"
	"math"
	"strings"
)

// effectiveWindowColor resolves vscode.window_color to a concrete
// "#rrggbb": "" stays "", "auto" derives a color from repo and branch, and
// a fixed hex is normalized to lowercase.
func effectiveWindowColor(setting, repo, branch string) string {
	switch setting {
	case "":
		return ""
	case "auto":
		return deriveWindowColor(repo, branch)
	default:
		return strings.ToLower(setting)
	}
}

// deriveWindowColor returns a deterministic "#rrggbb" for a worktree.
// FNV-1a of "repo\x00branch" picks the hue (NUL is illegal in both names,
// so the join is unambiguous), so every branch of every repo gets its own
// color, stable across machines and th versions. S and L are fixed at
// values dark enough to read as window chrome while keeping hues apart.
func deriveWindowColor(repo, branch string) string {
	h := fnv.New32a()
	h.Write([]byte(repo + "\x00" + branch))
	hue := float64(h.Sum32() % 360)
	r, g, b := hslToRGB(hue, 0.5, 0.35)
	return fmt.Sprintf("#%02x%02x%02x", r, g, b)
}

// hslToRGB converts hue in [0,360) and s, l in [0,1] to 8-bit sRGB.
func hslToRGB(h, s, l float64) (r, g, b uint8) {
	c := (1 - math.Abs(2*l-1)) * s
	hp := h / 60
	x := c * (1 - math.Abs(math.Mod(hp, 2)-1))
	var r1, g1, b1 float64
	switch {
	case hp < 1:
		r1, g1, b1 = c, x, 0
	case hp < 2:
		r1, g1, b1 = x, c, 0
	case hp < 3:
		r1, g1, b1 = 0, c, x
	case hp < 4:
		r1, g1, b1 = 0, x, c
	case hp < 5:
		r1, g1, b1 = x, 0, c
	default:
		r1, g1, b1 = c, 0, x
	}
	m := l - c/2
	to8 := func(v float64) uint8 { return uint8(math.Round((v + m) * 255)) }
	return to8(r1), to8(g1), to8(b1)
}

// contrastForeground returns "#ffffff" or "#000000", whichever reads
// against bg ("#rrggbb"). Rec. 709 luma on the raw sRGB bytes with a 50%
// threshold — an approximation that is plenty for a binary choice.
func contrastForeground(bg string) string {
	var r, g, b uint8
	if _, err := fmt.Sscanf(strings.ToLower(bg), "#%02x%02x%02x", &r, &g, &b); err != nil {
		return "#ffffff" // unreachable after config validation; fail bright
	}
	lum := 0.2126*float64(r) + 0.7152*float64(g) + 0.0722*float64(b)
	if lum > 127.5 {
		return "#000000"
	}
	return "#ffffff"
}

// windowColorCustomizations builds the workbench.colorCustomizations
// object for a background color: title bar and status bar, foreground
// picked for contrast, the inactive title bar at 60% opacity via VS Code's
// "#rrggbbaa" form.
func windowColorCustomizations(bg string) map[string]string {
	fg := contrastForeground(bg)
	return map[string]string{
		"titleBar.activeBackground":   bg,
		"titleBar.activeForeground":   fg,
		"titleBar.inactiveBackground": bg + "99",
		"titleBar.inactiveForeground": fg + "99",
		"statusBar.background":        bg,
		"statusBar.foreground":        fg,
	}
}
