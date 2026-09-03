package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNormalizeUnit(t *testing.T) {
	for in, want := range map[string]string{"": "", "KB": "kb", "Mb": "mb", "gb": "gb"} {
		got, err := normalizeUnit(in)
		if err != nil || got != want {
			t.Errorf("normalizeUnit(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	if _, err := normalizeUnit("TB"); err == nil {
		t.Error("normalizeUnit(\"TB\") should fail")
	}
}

func TestFormatSize(t *testing.T) {
	tests := []struct {
		n    int64
		unit string
		want string
	}{
		{512, "", "512 B"},
		{2048, "", "2.0 KB"},
		{3 << 20, "", "3.0 MB"},
		{3 << 30, "", "3.00 GB"},
		{1536, "kb", "1.5 KB"},
		{1 << 20, "kb", "1024.0 KB"},
		{5 << 20, "mb", "5.0 MB"},
		{1 << 30, "gb", "1.00 GB"},
	}
	for _, tt := range tests {
		if got := formatSize(tt.n, tt.unit); got != tt.want {
			t.Errorf("formatSize(%d, %q) = %q; want %q", tt.n, tt.unit, got, tt.want)
		}
	}
}

func TestDirSize(t *testing.T) {
	root := t.TempDir()
	write := func(rel string, size int) {
		t.Helper()
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, make([]byte, size), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("a.txt", 1000)
	write("sub/b.txt", 500)
	write(".git/objects/big", 4096)

	got, err := dirSize(root)
	if err != nil {
		t.Fatal(err)
	}
	if got != 1500 {
		t.Errorf("dirSize = %d; want 1500 (.git must be excluded)", got)
	}

	linked := t.TempDir()
	if err := os.WriteFile(filepath.Join(linked, ".git"), []byte("gitdir: /elsewhere"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(linked, "c.txt"), make([]byte, 100), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err = dirSize(linked)
	if err != nil {
		t.Fatal(err)
	}
	if got != 100 {
		t.Errorf("dirSize with .git pointer file = %d; want 100", got)
	}

	if _, err := dirSize(filepath.Join(root, "missing")); err == nil {
		t.Error("dirSize on a missing dir should fail")
	}
}
