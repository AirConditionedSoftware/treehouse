package cmd

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// copyFile tries a filesystem clone before streaming (reflink_*.go). These
// tests assert behavior — content, mode, progress totals — not which
// mechanism ran: the CI matrix is the mechanism coverage, with macOS
// runners on APFS exercising the clone path and ubuntu's ext4 forcing the
// streamed fallback, and the same assertions must hold on both. A darwin
// "the clone really happened" check (Stat_t.Blocks or free-space deltas)
// is deliberately omitted: APFS space accounting and a TMPDIR on a
// non-APFS volume make it fragile.

func TestCopyFileContent(t *testing.T) {
	// 0o755 also covers "hooks stay executable". All three modes survive a
	// standard umask, so the clone path (mode copied from src) and the
	// streamed path (perm passed to OpenFile) must agree exactly.
	for _, perm := range []fs.FileMode{0o600, 0o644, 0o755} {
		t.Run(fmt.Sprintf("%04o", perm), func(t *testing.T) {
			dir := t.TempDir()
			src := filepath.Join(dir, "src")
			content := []byte("clone or stream, same bytes\n")
			if err := os.WriteFile(src, content, 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(src, perm); err != nil {
				t.Fatal(err)
			}
			dst := filepath.Join(dir, "sub", "dst")
			if err := copyFile(src, dst, perm, nil); err != nil {
				t.Fatal(err)
			}
			got, err := os.ReadFile(dst)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, content) {
				t.Errorf("dst content = %q; want %q", got, content)
			}
			info, err := os.Stat(dst)
			if err != nil {
				t.Fatal(err)
			}
			if info.Mode().Perm() != perm {
				t.Errorf("dst mode = %04o; want %04o", info.Mode().Perm(), perm)
			}
		})
	}
}

func TestCopyFileOverwritesExisting(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "dst")
	if err := os.WriteFile(src, []byte("new content"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Stale content longer than src's proves the overwrite truncates. On
	// darwin an existing dst makes clonefile fail with EEXIST, exercising
	// the fallback; elsewhere O_TRUNC overwrites directly. Mode is not
	// asserted exactly: an overwrite keeps dst's existing mode, which
	// depends on the umask that created it.
	if err := os.WriteFile(dst, []byte("stale, and longer than the new content"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := copyFile(src, dst, 0o644, nil); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "new content" {
		t.Errorf("dst content = %q; want %q", got, "new content")
	}
}

func TestCopyFileProgressTotals(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	// An odd size defeats any block-size rounding: the clone path reports
	// Stat's size in one advance, the streamed path counts bytes through
	// progressWriter, and both must total exactly len(content) — the
	// "Copied N file(s) (SIZE)" summary depends on it.
	content := bytes.Repeat([]byte{0xA5}, 3<<20+17)
	if err := os.WriteFile(src, content, 0o644); err != nil {
		t.Fatal(err)
	}
	prog := &copyProgress{w: io.Discard, live: false}
	if err := copyFile(src, filepath.Join(dir, "dst"), 0o644, prog); err != nil {
		t.Fatal(err)
	}
	if prog.files != 1 {
		t.Errorf("files = %d; want 1", prog.files)
	}
	if prog.bytes != int64(len(content)) {
		t.Errorf("bytes = %d; want %d", prog.bytes, len(content))
	}
}

func TestCopyFileNilProgress(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.WriteFile(src, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Hook copying passes a nil progress; the clone path's advance must
	// tolerate it like the streamed path always has.
	if err := copyFile(src, filepath.Join(dir, "dst"), 0o644, nil); err != nil {
		t.Fatal(err)
	}
}

func TestCopyFileMissingSrc(t *testing.T) {
	dir := t.TempDir()
	// The clone attempt is gated on a successful Stat, so a missing src
	// must still surface os.Open's error, not be masked by the fallback.
	err := copyFile(filepath.Join(dir, "missing"), filepath.Join(dir, "dst"), 0o644, nil)
	if err == nil {
		t.Fatal("copyFile succeeded for a missing src; want an error")
	}
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("err = %v; want a does-not-exist error", err)
	}
}
