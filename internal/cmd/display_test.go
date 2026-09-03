package cmd

import "testing"

func TestDisplayPath(t *testing.T) {
	t.Setenv("HOME", "/Users/dev")
	tests := []struct{ in, want string }{
		{"/Users/dev", "~"},
		{"/Users/dev/worktrees/myapp/fix-login", "~/worktrees/myapp/fix-login"},
		{"/Users/developer/code", "/Users/developer/code"},
		{"/opt/homebrew/bin", "/opt/homebrew/bin"},
		{"", ""},
	}
	for _, tt := range tests {
		if got := displayPath(tt.in); got != tt.want {
			t.Errorf("displayPath(%q) = %q; want %q", tt.in, got, tt.want)
		}
	}
}

func TestDisplayPathFullPaths(t *testing.T) {
	t.Setenv("HOME", "/Users/dev")
	in := "/Users/dev/worktrees/x"

	fullPathsFlag = true
	t.Cleanup(func() { fullPathsFlag = false })
	if got := displayPath(in); got != in {
		t.Errorf("displayPath with --full-paths = %q; want %q", got, in)
	}
	fullPathsFlag = false

	fullPathsCfg = true
	t.Cleanup(func() { fullPathsCfg = false })
	if got := displayPath(in); got != in {
		t.Errorf("displayPath with full_paths config = %q; want %q", got, in)
	}
	fullPathsCfg = false

	if got := displayPath(in); got != "~/worktrees/x" {
		t.Errorf("displayPath without full paths = %q; want abbreviated", got)
	}
}
