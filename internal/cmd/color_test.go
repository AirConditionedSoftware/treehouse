package cmd

import (
	"strings"
	"testing"
)

// withTerminal fakes a TTY on stdout for the duration of the test.
func withTerminal(t *testing.T, isTTY bool) {
	t.Helper()
	prev := stdoutIsTerminal
	stdoutIsTerminal = func() bool { return isTTY }
	t.Cleanup(func() { stdoutIsTerminal = prev })
}

func TestColorLine(t *testing.T) {
	t.Setenv("NO_COLOR", "")

	withTerminal(t, true)
	if got := colorLine("row", ansiGreen); got != ansiGreen+"row"+ansiReset {
		t.Errorf("colorLine on a terminal = %q; want styled", got)
	}
	if got := colorLine("row", ""); got != "row" {
		t.Errorf("colorLine with empty style = %q; want plain", got)
	}

	noColor = true
	t.Cleanup(func() { noColor = false })
	if got := colorLine("row", ansiGreen); got != "row" {
		t.Errorf("colorLine with --no-color = %q; want plain", got)
	}
	noColor = false

	t.Setenv("NO_COLOR", "1")
	if got := colorLine("row", ansiGreen); got != "row" {
		t.Errorf("colorLine with NO_COLOR = %q; want plain", got)
	}
	t.Setenv("NO_COLOR", "")

	withTerminal(t, false)
	if got := colorLine("row", ansiGreen); got != "row" {
		t.Errorf("colorLine without a terminal = %q; want plain", got)
	}
}

func TestPrintStyled(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	withTerminal(t, true)

	var sb strings.Builder
	table := "HEADER\nrow one\nrow two\n"
	if err := printStyled(&sb, table, []string{ansiBold, "", ansiGreen}); err != nil {
		t.Fatal(err)
	}
	want := ansiBold + "HEADER" + ansiReset + "\n" +
		"row one\n" +
		ansiGreen + "row two" + ansiReset + "\n"
	if sb.String() != want {
		t.Errorf("printStyled = %q; want %q", sb.String(), want)
	}

	sb.Reset()
	if err := printStyled(&sb, "only\n", nil); err != nil {
		t.Fatal(err)
	}
	if sb.String() != "only\n" {
		t.Errorf("printStyled without styles = %q; want plain line", sb.String())
	}
}
