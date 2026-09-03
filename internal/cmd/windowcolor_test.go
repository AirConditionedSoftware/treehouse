package cmd

import (
	"regexp"
	"testing"
)

func TestDeriveWindowColorDeterministic(t *testing.T) {
	a := deriveWindowColor("myapp", "fix-login")
	b := deriveWindowColor("myapp", "fix-login")
	if a != b {
		t.Errorf("deriveWindowColor not deterministic: %q vs %q", a, b)
	}
}

func TestDeriveWindowColorDistinct(t *testing.T) {
	inputs := []struct{ repo, branch string }{
		{"myapp", "main"},
		{"myapp", "fix-login"},
		{"myapp", "feature/x"},
		{"myapp", "release-2"},
		{"otherapp", "main"}, // the repo participates in the hash
	}
	seen := map[string]string{}
	for _, in := range inputs {
		c := deriveWindowColor(in.repo, in.branch)
		key := in.repo + "/" + in.branch
		if prev, ok := seen[c]; ok {
			t.Errorf("color %s collides: %s and %s", c, prev, key)
		}
		seen[c] = key
	}
}

func TestDeriveWindowColorFormat(t *testing.T) {
	re := regexp.MustCompile(`^#[0-9a-f]{6}$`)
	for _, branch := range []string{"main", "feature/login", "über-branch", ""} {
		if c := deriveWindowColor("myapp", branch); !re.MatchString(c) {
			t.Errorf("deriveWindowColor(myapp, %q) = %q; want #rrggbb", branch, c)
		}
	}
}

func TestHSLToRGB(t *testing.T) {
	tests := []struct {
		h, s, l float64
		r, g, b uint8
	}{
		{0, 1, 0.5, 255, 0, 0},
		{120, 1, 0.5, 0, 255, 0},
		{240, 1, 0.5, 0, 0, 255},
		{0, 0, 0.5, 128, 128, 128}, // 127.5 rounds to 128
	}
	for _, tt := range tests {
		r, g, b := hslToRGB(tt.h, tt.s, tt.l)
		if r != tt.r || g != tt.g || b != tt.b {
			t.Errorf("hslToRGB(%v, %v, %v) = (%d, %d, %d); want (%d, %d, %d)",
				tt.h, tt.s, tt.l, r, g, b, tt.r, tt.g, tt.b)
		}
	}
}

func TestContrastForeground(t *testing.T) {
	tests := []struct{ bg, want string }{
		{"#000000", "#ffffff"},
		{"#ffffff", "#000000"},
		{"#ffff00", "#000000"}, // yellow is bright despite full-intensity channels
		{"#00007f", "#ffffff"},
	}
	for _, tt := range tests {
		if got := contrastForeground(tt.bg); got != tt.want {
			t.Errorf("contrastForeground(%s) = %s; want %s", tt.bg, got, tt.want)
		}
	}
}

func TestWindowColorCustomizations(t *testing.T) {
	got := windowColorCustomizations("#336699")
	want := map[string]string{
		"titleBar.activeBackground":   "#336699",
		"titleBar.activeForeground":   "#ffffff",
		"titleBar.inactiveBackground": "#33669999",
		"titleBar.inactiveForeground": "#ffffff99",
		"statusBar.background":        "#336699",
		"statusBar.foreground":        "#ffffff",
	}
	if len(got) != len(want) {
		t.Errorf("got %d keys; want %d", len(got), len(want))
	}
	for k, w := range want {
		if got[k] != w {
			t.Errorf("%s = %q; want %q", k, got[k], w)
		}
	}
}

func TestEffectiveWindowColor(t *testing.T) {
	if got := effectiveWindowColor("", "myapp", "main"); got != "" {
		t.Errorf("unset: got %q; want empty", got)
	}
	if got, want := effectiveWindowColor("auto", "myapp", "main"), deriveWindowColor("myapp", "main"); got != want {
		t.Errorf("auto: got %q; want %q", got, want)
	}
	if got := effectiveWindowColor("#AABBCC", "myapp", "main"); got != "#aabbcc" {
		t.Errorf("fixed hex: got %q; want #aabbcc", got)
	}
}
