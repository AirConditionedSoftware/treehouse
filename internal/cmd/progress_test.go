package cmd

import (
	"errors"
	"strings"
	"testing"
)

var errFake = errors.New("fake")

func TestCopyProgressLive(t *testing.T) {
	var buf strings.Builder
	p := &copyProgress{w: &buf, label: "big.bin", live: true}

	p.advance(1<<20, false) // first tick draws immediately
	p.advance(1<<20, false) // within the throttle interval: no draw
	p.advance(0, true)
	p.done()

	out := buf.String()
	if got := strings.Count(out, "\r\x1b[KCopying big.bin:"); got != 1 {
		t.Errorf("drew %d live updates; want 1 (throttled)\n%q", got, out)
	}
	if !strings.Contains(out, "Copying big.bin: 0 file(s), 1.0 MB") {
		t.Errorf("missing first live update in %q", out)
	}
	if !strings.HasSuffix(out, "\r\x1b[KCopied big.bin (1 file(s), 2.0 MB)\n") {
		t.Errorf("missing final summary in %q", out)
	}
}

func TestCopyProgressPiped(t *testing.T) {
	var buf strings.Builder
	p := &copyProgress{w: &buf, label: ".env", live: false}

	p.advance(12, true)
	p.done()

	// Without a terminal only the constructor's "Copying ..." line appears;
	// advance and done stay silent so piped logs aren't flooded.
	if buf.Len() != 0 {
		t.Errorf("piped progress wrote %q; want nothing after construction", buf.String())
	}
}

func TestCopyProgressNil(t *testing.T) {
	var p *copyProgress
	p.advance(5, true) // must not panic; hook copying passes nil
	p.done()
}

func TestRunWithStatusLive(t *testing.T) {
	var buf strings.Builder
	err := runWithStatusOn(&buf, true, "Removing x (2 B)", func() error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.HasPrefix(out, "\r\x1b[KRemoving x (2 B)") {
		t.Errorf("missing initial live line in %q", out)
	}
	// The line is erased when fn returns so the caller's outcome message
	// starts on a clean line.
	if !strings.HasSuffix(out, "\r\x1b[K") {
		t.Errorf("missing trailing erase in %q", out)
	}
}

func TestRunWithStatusPiped(t *testing.T) {
	var buf strings.Builder
	err := runWithStatusOn(&buf, false, "Removing x (2 B)", func() error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if got := buf.String(); got != "Removing x (2 B)\n" {
		t.Errorf("piped output = %q; want the label line only", got)
	}
}

func TestRunWithStatusError(t *testing.T) {
	var buf strings.Builder
	wantErr := runWithStatusOn(&buf, true, "x", func() error { return errFake })
	if wantErr != errFake {
		t.Errorf("err = %v; want fn's error passed through", wantErr)
	}
}
