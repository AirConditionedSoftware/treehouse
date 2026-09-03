package cmd

import (
	"fmt"
	"io"
	"os"
	"time"

	"golang.org/x/term"
)

// stderrIsTerminal is a variable so tests can exercise the live-progress
// path without a real terminal.
var stderrIsTerminal = func() bool {
	return term.IsTerminal(int(os.Stderr.Fd()))
}

const progressInterval = 100 * time.Millisecond

// copyProgress narrates one copy_files match on stderr. On a terminal it
// rewrites a single status line in place as bytes stream, so big files and
// directories show movement; piped output gets one "Copying ..." line up
// front instead, so logs aren't flooded.
type copyProgress struct {
	w        io.Writer
	label    string
	files    int
	bytes    int64
	live     bool
	lastDraw time.Time
}

func newCopyProgress(label string) *copyProgress {
	p := &copyProgress{w: os.Stderr, label: label, live: stderrIsTerminal()}
	if !p.live {
		fmt.Fprintf(p.w, "Copying %s\n", label)
	}
	return p
}

// advance records n more copied bytes, plus a completed file when file is
// set. A nil receiver (hook copying) is silent.
func (p *copyProgress) advance(n int64, file bool) {
	if p == nil {
		return
	}
	p.bytes += n
	if file {
		p.files++
	}
	if p.live && time.Since(p.lastDraw) >= progressInterval {
		p.lastDraw = time.Now()
		fmt.Fprintf(p.w, "\r\x1b[KCopying %s: %s", p.label, p.status())
	}
}

// done replaces the live line with the final per-match summary.
func (p *copyProgress) done() {
	if p == nil || !p.live {
		return
	}
	fmt.Fprintf(p.w, "\r\x1b[KCopied %s (%s)\n", p.label, p.status())
}

func (p *copyProgress) status() string {
	return fmt.Sprintf("%d file(s), %s", p.files, formatSize(p.bytes, ""))
}

// progressWriter counts bytes into prog as they pass through.
type progressWriter struct {
	w    io.Writer
	prog *copyProgress
}

func (pw progressWriter) Write(b []byte) (int, error) {
	n, err := pw.w.Write(b)
	pw.prog.advance(int64(n), false)
	return n, err
}

// runWithStatus prints label on stderr and runs fn. On a terminal the line
// ticks with elapsed time in place while fn runs and is erased when it
// returns — the caller prints the outcome; piped output gets the label once
// up front, so logs show what a long operation is working on.
func runWithStatus(label string, fn func() error) error {
	return runWithStatusOn(os.Stderr, stderrIsTerminal(), label, fn)
}

func runWithStatusOn(w io.Writer, live bool, label string, fn func() error) error {
	if !live {
		fmt.Fprintln(w, label)
		return fn()
	}
	fmt.Fprintf(w, "\r\x1b[K%s", label)
	start := time.Now()
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				fmt.Fprintf(w, "\r\x1b[K%s: %ds", label, int(time.Since(start).Seconds()))
			}
		}
	}()
	err := fn()
	close(stop)
	<-done
	fmt.Fprint(w, "\r\x1b[K")
	return err
}
