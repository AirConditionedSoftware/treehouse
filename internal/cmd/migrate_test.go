package cmd

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AirConditionedSoftware/treehouse/internal/config"
)

// captureStderr runs fn with os.Stderr replaced by a pipe and returns what
// was written to it.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	saved := os.Stderr
	os.Stderr = w
	done := make(chan string, 1)
	go func() {
		out, _ := io.ReadAll(r)
		done <- string(out)
	}()
	fn()
	os.Stderr = saved
	w.Close()
	out := <-done
	r.Close()
	return out
}

func TestFinalizeLocalMigrationNoPending(t *testing.T) {
	out := captureStderr(t, func() {
		if err := finalizeLocalMigration(config.Resolved{}); err != nil {
			t.Errorf("finalizeLocalMigration with no pending migration = %v; want nil", err)
		}
	})
	if out != "" {
		t.Errorf("stderr = %q; want nothing said about an up-to-date file", out)
	}
}

// Tests never run with a terminal attached, so this exercises the non-TTY
// path: the note goes to stderr and the file on disk is left alone.
func TestFinalizeLocalMigrationWithoutTerminal(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, config.LocalFileName)
	original := []byte("{\n  \"name\": \"myapp\"\n}\n")
	if err := os.WriteFile(path, original, 0o644); err != nil {
		t.Fatal(err)
	}

	res := config.Resolved{LocalMigration: &config.PendingMigration{
		Path:     path,
		From:     1,
		To:       2,
		Original: original,
		Migrated: []byte("{\n  \"version\": 2,\n  \"name\": \"myapp\"\n}\n"),
	}}

	var err error
	out := captureStderr(t, func() { err = finalizeLocalMigration(res) })
	if err != nil {
		t.Fatalf("finalizeLocalMigration = %v; want nil without a terminal", err)
	}
	for _, want := range []string{"schema v1", "th now uses v2", "th migrate"} {
		if !strings.Contains(out, want) {
			t.Errorf("stderr = %q; want it to mention %q", out, want)
		}
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(original) {
		t.Errorf("%s = %q; want it untouched without a terminal to ask at", config.LocalFileName, got)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("directory holds %d files; want only the %s (no backup written)", len(entries), config.LocalFileName)
	}
}
