package cmd

import (
	"strings"
	"testing"
)

func TestShellInitScript(t *testing.T) {
	zsh, err := shellInitScript("zsh")
	if err != nil {
		t.Fatal(err)
	}
	bash, err := shellInitScript("bash")
	if err != nil {
		t.Fatal(err)
	}
	if zsh != bash {
		t.Error("zsh and bash should share the POSIX wrapper")
	}
	for _, want := range []string{"TH_CD_FILE", `command th "$@"`, "th() {", "mktemp", "rm -f"} {
		if !strings.Contains(zsh, want) {
			t.Errorf("POSIX wrapper missing %q:\n%s", want, zsh)
		}
	}

	fish, err := shellInitScript("fish")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"function th", "TH_CD_FILE", "command th $argv"} {
		if !strings.Contains(fish, want) {
			t.Errorf("fish wrapper missing %q:\n%s", want, fish)
		}
	}

	for _, shell := range []string{"powershell", "sh", ""} {
		if _, err := shellInitScript(shell); err == nil || !strings.Contains(err.Error(), "unsupported shell") {
			t.Errorf("shellInitScript(%q): err = %v; want unsupported-shell error", shell, err)
		}
	}
}

func TestDetectShell(t *testing.T) {
	names := []string{"zsh", "bash", "fish"}

	t.Setenv("SHELL", "/usr/bin/fish")
	if got := detectShell(names); got != "fish" {
		t.Errorf("detectShell with SHELL=/usr/bin/fish = %q; want fish", got)
	}

	t.Setenv("SHELL", "/bin/tcsh")
	if got := detectShell(names); got != "zsh" {
		t.Errorf("detectShell with unknown SHELL = %q; want the zsh fallback", got)
	}

	t.Setenv("SHELL", "")
	if got := detectShell(names); got != "zsh" {
		t.Errorf("detectShell with empty SHELL = %q; want the zsh fallback", got)
	}
}
