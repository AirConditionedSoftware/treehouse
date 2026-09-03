package cmd

import (
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"
)

var noColor bool

// stdoutIsTerminal is a variable so tests can exercise the enabled path
// without a real TTY.
var stdoutIsTerminal = func() bool {
	return term.IsTerminal(int(os.Stdout.Fd()))
}

const (
	ansiReset  = "\x1b[0m"
	ansiBold   = "\x1b[1m"
	ansiDim    = "\x1b[2m"
	ansiGreen  = "\x1b[32m"
	ansiYellow = "\x1b[33m"
	ansiCyan   = "\x1b[36m"
	ansiGray   = "\x1b[90m"
)

func colorsEnabled() bool {
	if noColor || os.Getenv("NO_COLOR") != "" {
		return false
	}
	return stdoutIsTerminal()
}

// colorText styles an inline segment. Safe only where column widths are
// computed BEFORE styling — tabwriter-formatted output must use colorLine
// on whole lines instead.
func colorText(s, style string) string {
	return colorLine(s, style)
}

// colorLine wraps a complete, already-formatted line in an ANSI style.
// Whole lines only: escape codes inside cells would inflate the widths
// tabwriter computes and break column alignment.
func colorLine(line, style string) string {
	if style == "" || !colorsEnabled() {
		return line
	}
	return style + line + ansiReset
}

// printStyled prints tabwriter-formatted output line by line, wrapping each
// line in the style of its row (styles[i] belongs to line i; "" is plain).
func printStyled(w io.Writer, table string, styles []string) error {
	if table == "" {
		return nil
	}
	for i, line := range strings.Split(strings.TrimRight(table, "\n"), "\n") {
		style := ""
		if i < len(styles) {
			style = styles[i]
		}
		if _, err := fmt.Fprintln(w, colorLine(line, style)); err != nil {
			return err
		}
	}
	return nil
}
