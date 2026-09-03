package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeSteps are the two steps the engine tests chain: v1 -> v2 renames "old"
// to "new", v2 -> v3 wraps "new"'s value in a list. Real steps never live in
// tests — these exist to exercise the machinery while both registries are
// still empty.
var fakeSteps = []migration{
	{from: 1, apply: func(m map[string]any) (map[string]any, error) {
		if v, ok := m["old"]; ok {
			delete(m, "old")
			m["new"] = v
		}
		return m, nil
	}},
	{from: 2, apply: func(m map[string]any) (map[string]any, error) {
		if v, ok := m["new"]; ok {
			m["new"] = []any{v}
		}
		return m, nil
	}},
}

func TestSniffVersion(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    int
		wantErr bool
	}{
		{"absent means 1", `{"name": "x"}`, 1, false},
		{"explicit version", `{"version": 3}`, 3, false},
		{"zero means 1", `{"version": 0}`, 1, false},
		{"negative rejected", `{"version": -1}`, 0, true},
		{"non-integer rejected", `{"version": 1.5}`, 0, true},
		{"string rejected", `{"version": "2"}`, 0, true},
		{"unknown fields ignored", `{"version": 2, "from_the_future": {"a": 1}}`, 2, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := sniffVersion([]byte(tt.raw))
			if (err != nil) != tt.wantErr {
				t.Fatalf("sniffVersion(%s) err = %v; wantErr = %v", tt.raw, err, tt.wantErr)
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("sniffVersion(%s) = %d; want %d", tt.raw, got, tt.want)
			}
		})
	}
}

func TestMigrateRawChainsSteps(t *testing.T) {
	out, from, to, err := migrateRaw("config /tmp/th.json", []byte(`{"old": "x"}`), fakeSteps)
	if err != nil {
		t.Fatalf("migrateRaw: %v", err)
	}
	if from != 1 || to != 3 {
		t.Errorf("migrateRaw reported v%d -> v%d; want v1 -> v3", from, to)
	}
	// Both steps applied, "version" stamped, two-space indent, trailing newline.
	want := "{\n  \"new\": [\n    \"x\"\n  ],\n  \"version\": 3\n}\n"
	if string(out) != want {
		t.Errorf("migrateRaw output =\n%q\nwant\n%q", out, want)
	}
}

func TestMigrateRawSameVersionPassesThrough(t *testing.T) {
	// Deliberately odd formatting: an up-to-date file must come back byte
	// for byte, never re-encoded, so nothing on disk is disturbed.
	raw := []byte("{\"version\":   1,\n\t\"branch_prefix\": \"peter\"}")
	out, from, to, err := migrateRaw("config /tmp/th.json", raw, nil)
	if err != nil {
		t.Fatalf("migrateRaw: %v", err)
	}
	if from != 1 || to != 1 {
		t.Errorf("migrateRaw reported v%d -> v%d; want v1 -> v1", from, to)
	}
	if string(out) != string(raw) {
		t.Errorf("migrateRaw rewrote an up-to-date document:\n%q\nwant\n%q", out, raw)
	}
}

func TestMigrateRawTooNew(t *testing.T) {
	// An empty registry: version 1, which is what ships today.
	_, _, _, err := migrateRaw("config /tmp/th.json", []byte(`{"version": 99}`), nil)
	var tooNew *TooNewError
	if !errors.As(err, &tooNew) {
		t.Fatalf("migrateRaw with a future version: err = %v; want *TooNewError", err)
	}
	if tooNew.Found != 99 || tooNew.Supported != 1 {
		t.Errorf("TooNewError = %+v; want Found 99, Supported 1", tooNew)
	}
	want := "config /tmp/th.json: schema version 99 was written by a newer th (this version supports up to 1); upgrade th"
	if err.Error() != want {
		t.Errorf("err = %q; want %q", err, want)
	}
	if !strings.Contains(err.Error(), "upgrade th") {
		t.Errorf("err = %q; want it to say how to fix it", err)
	}
}

func TestMigrateRawWrapsStepErrors(t *testing.T) {
	steps := []migration{
		{from: 1, apply: func(m map[string]any) (map[string]any, error) { return m, nil }},
		{from: 2, apply: func(map[string]any) (map[string]any, error) { return nil, errors.New("boom") }},
	}
	_, _, _, err := migrateRaw("config /tmp/th.json", []byte(`{}`), steps)
	want := "config /tmp/th.json: migrating schema v2 to v3: boom"
	if err == nil || err.Error() != want {
		t.Errorf("migrateRaw with a failing step: err = %v; want %q", err, want)
	}
}

// TestRegistriesAreContiguous guards the invariant the engine relies on:
// entry i migrates version i+1, with no gaps and nothing out of order, so
// len(registry)+1 really is the current version.
func TestRegistriesAreContiguous(t *testing.T) {
	tests := []struct {
		name    string
		steps   []migration
		version int
	}{
		{"global", globalMigrations, CurrentGlobalVersion()},
		{"local", localMigrations, CurrentLocalVersion()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for i, s := range tt.steps {
				if s.from != i+1 {
					t.Errorf("%s step %d migrates from v%d; want v%d — steps must stay in order with no gaps", tt.name, i, s.from, i+1)
				}
				if s.apply == nil {
					t.Errorf("%s step %d has no apply func", tt.name, i)
				}
			}
			if want := len(tt.steps) + 1; tt.version != want {
				t.Errorf("current %s version = %d; want %d", tt.name, tt.version, want)
			}
		})
	}
}

func TestBackupPathIsTimestamped(t *testing.T) {
	path := "/home/p/.th/config.json"
	at := time.Date(2026, 8, 25, 11, 8, 0, 0, time.UTC)
	got := backupPath(path, 1, at)
	want := path + ".v1.20260825-110800.bak"
	if got != want {
		t.Errorf("backupPath(%q, 1, %v) = %q; want %q", path, at, got, want)
	}
	// Same file, same version, later run: a backup is never overwritten.
	if later := backupPath(path, 1, at.Add(time.Second)); later == got {
		t.Errorf("two backups of v1 collide on %q; want distinct names", got)
	}
}

func TestPendingMigrationWriteBackupAndPersist(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, LocalFileName)
	original := []byte("{\"old_name\": \"solo\"}\n")
	if err := os.WriteFile(path, original, 0o640); err != nil {
		t.Fatal(err)
	}
	p := &PendingMigration{
		Path:     path,
		From:     1,
		To:       2,
		Original: original,
		Migrated: []byte("{\n  \"name\": \"solo\",\n  \"version\": 2\n}\n"),
	}

	backup, err := p.WriteBackup()
	if err != nil {
		t.Fatalf("WriteBackup: %v", err)
	}
	if !strings.HasPrefix(backup, path+".v1.") || !strings.HasSuffix(backup, ".bak") {
		t.Errorf("backup = %q; want %q with a timestamp and .bak", backup, path+".v1.<ts>.bak")
	}
	got, err := os.ReadFile(backup)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(original) {
		t.Errorf("backup content = %q; want the pre-migration bytes %q", got, original)
	}
	if fi, err := os.Stat(backup); err != nil {
		t.Fatal(err)
	} else if fi.Mode().Perm() != 0o640 {
		t.Errorf("backup mode = %v; want the source's 0640", fi.Mode().Perm())
	}
	// The backup alone must not disturb the file.
	if got, err := os.ReadFile(path); err != nil {
		t.Fatal(err)
	} else if string(got) != string(original) {
		t.Errorf("WriteBackup changed %s to %q", LocalFileName, got)
	}

	if err := p.Persist(); err != nil {
		t.Fatalf("Persist: %v", err)
	}
	if got, err := os.ReadFile(path); err != nil {
		t.Fatal(err)
	} else if string(got) != string(p.Migrated) {
		t.Errorf("after Persist %s = %q; want %q", LocalFileName, got, p.Migrated)
	}
	if fi, err := os.Stat(path); err != nil {
		t.Fatal(err)
	} else if fi.Mode().Perm() != 0o640 {
		t.Errorf("after Persist mode = %v; want the original 0640", fi.Mode().Perm())
	}
	// The temp file the atomic write used is gone: the .thrc and its backup.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Errorf("directory holds %d entries; want the file and its backup only", len(entries))
	}
}

// TestPendingMigrationDescribesItsFile pins the wording each caller gets:
// the global loader's pending names the file "config <path>", while a
// zero-valued construction — every .thrc one, in this package and in
// internal/cmd — still says "repo config <path>".
func TestPendingMigrationDescribesItsFile(t *testing.T) {
	// A path inside a directory that does not exist: both writes are certain
	// to fail, which is the only way to see the wording.
	missing := filepath.Join(t.TempDir(), "nope", "config.json")
	tests := []struct {
		name string
		desc string
		want string
	}{
		{"global desc", "config " + missing, "config " + missing},
		{"zero desc falls back to the .thrc wording", "", "repo config " + missing},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &PendingMigration{
				desc:     tt.desc,
				Path:     missing,
				From:     1,
				To:       2,
				Original: []byte("{}\n"),
				Migrated: []byte("{\n  \"version\": 2\n}\n"),
			}
			if _, err := p.WriteBackup(); err == nil {
				t.Error("WriteBackup into a nonexistent directory succeeded")
			} else if want := tt.want + ": backing up before migration"; !strings.HasPrefix(err.Error(), want) {
				t.Errorf("WriteBackup err = %q; want it to start with %q", err, want)
			}
			if err := p.Persist(); err == nil {
				t.Error("Persist into a nonexistent directory succeeded")
			} else if want := tt.want + ": writing migrated config"; !strings.HasPrefix(err.Error(), want) {
				t.Errorf("Persist err = %q; want it to start with %q", err, want)
			}
		})
	}
}

// unknownFieldStep is a fake v1 -> v2 step that invents a key no current
// struct has: its output is well-formed JSON the strict decoder still
// rejects, which is exactly what the inspection API decodes to catch.
func unknownFieldStep(name string) migration {
	return migration{from: 1, apply: func(m map[string]any) (map[string]any, error) {
		m[name] = true
		return m, nil
	}}
}

func TestPendingLocalMigration(t *testing.T) {
	t.Run("out of date", func(t *testing.T) {
		useLocalMigrations(t, []migration{renameStep("old_prefix", "branch_prefix")})
		main := t.TempDir()
		path := filepath.Join(main, LocalFileName)
		original := []byte(`{"old_prefix": "team"}`)
		if err := os.WriteFile(path, original, 0o644); err != nil {
			t.Fatal(err)
		}

		pending, got, exists, err := PendingLocalMigration(main)
		if err != nil {
			t.Fatalf("PendingLocalMigration(%q): %v", main, err)
		}
		if got != path || !exists {
			t.Errorf("PendingLocalMigration() = path %q, exists %v; want %q, true", got, exists, path)
		}
		if pending == nil {
			t.Fatal("PendingLocalMigration() reported nothing pending for a v1 .thrc")
		}
		if pending.Path != path || pending.From != 1 || pending.To != 2 {
			t.Errorf("pending = {Path: %q, From: %d, To: %d}; want {%q, 1, 2}", pending.Path, pending.From, pending.To, path)
		}
		if string(pending.Original) != string(original) {
			t.Errorf("pending.Original = %q; want %q", pending.Original, original)
		}
		wantMigrated := "{\n  \"branch_prefix\": \"team\",\n  \"version\": 2\n}\n"
		if string(pending.Migrated) != wantMigrated {
			t.Errorf("pending.Migrated =\n%q\nwant\n%q", pending.Migrated, wantMigrated)
		}
		// The whole point of the API: inspecting writes nothing — no
		// rewrite, no backup.
		assertUntouched(t, main, path, original)
	})

	t.Run("already current", func(t *testing.T) {
		useLocalMigrations(t, []migration{renameStep("old_prefix", "branch_prefix")})
		main := t.TempDir()
		path := filepath.Join(main, LocalFileName)
		content := []byte("{\"version\":   2,\n\t\"branch_prefix\": \"team\"}")
		if err := os.WriteFile(path, content, 0o644); err != nil {
			t.Fatal(err)
		}

		pending, got, exists, err := PendingLocalMigration(main)
		if err != nil {
			t.Fatalf("PendingLocalMigration(%q): %v", main, err)
		}
		if pending != nil {
			t.Errorf("pending = %+v; want nil for a current .thrc", pending)
		}
		if got != path || !exists {
			t.Errorf("PendingLocalMigration() = path %q, exists %v; want %q, true", got, exists, path)
		}
		assertUntouched(t, main, path, content)
	})

	t.Run("missing", func(t *testing.T) {
		main := t.TempDir()
		pending, got, exists, err := PendingLocalMigration(main)
		if err != nil {
			t.Fatalf("PendingLocalMigration(%q) of a repo without a %s: %v", main, LocalFileName, err)
		}
		if pending != nil || exists {
			t.Errorf("PendingLocalMigration() = pending %+v, exists %v; want nil, false", pending, exists)
		}
		if want := filepath.Join(main, LocalFileName); got != want {
			t.Errorf("path = %q; want %q reported anyway, for the message", got, want)
		}
	})

	t.Run("too new", func(t *testing.T) {
		main := t.TempDir()
		path := filepath.Join(main, LocalFileName)
		original := []byte(`{"version": 99}`)
		if err := os.WriteFile(path, original, 0o644); err != nil {
			t.Fatal(err)
		}

		pending, _, _, err := PendingLocalMigration(main)
		var tooNew *TooNewError
		if !errors.As(err, &tooNew) {
			t.Fatalf("PendingLocalMigration() of a future .thrc: err = %v; want *TooNewError", err)
		}
		if pending != nil {
			t.Errorf("pending = %+v; want nil for a file this th cannot read", pending)
		}
		if !strings.Contains(err.Error(), "repo config") || !strings.Contains(err.Error(), "upgrade th") {
			t.Errorf("err = %v; want a repo config error saying to upgrade th", err)
		}
		assertUntouched(t, main, path, original)
	})

	t.Run("migrated bytes must still decode", func(t *testing.T) {
		useLocalMigrations(t, []migration{unknownFieldStep("not_a_setting")})
		main := t.TempDir()
		path := filepath.Join(main, LocalFileName)
		original := []byte(`{"name": "solo"}`)
		if err := os.WriteFile(path, original, 0o644); err != nil {
			t.Fatal(err)
		}

		pending, _, _, err := PendingLocalMigration(main)
		if err == nil {
			t.Fatal("PendingLocalMigration() accepted migrated bytes the loader would reject")
		}
		if pending != nil {
			t.Errorf("pending = %+v; want nil alongside the error", pending)
		}
		want := "repo config " + path + ": json: unknown field \"not_a_setting\""
		if err.Error() != want {
			t.Errorf("err = %q; want %q", err, want)
		}
		assertUntouched(t, main, path, original)
	})
}

func TestPendingGlobalMigration(t *testing.T) {
	t.Run("out of date, file untouched", func(t *testing.T) {
		// The exact opposite of TestLoadMigratesAndBacksUp: same file, same
		// step, and nothing on disk moves.
		useGlobalMigrations(t, []migration{renameStep("old_base", "default_base")})
		dir := t.TempDir()
		p := filepath.Join(dir, "th.json")
		original := []byte(`{"old_base": "develop"}`)
		if err := os.WriteFile(p, original, 0o600); err != nil {
			t.Fatal(err)
		}
		t.Setenv(EnvVar, p)

		pending, got, exists, err := PendingGlobalMigration()
		if err != nil {
			t.Fatalf("PendingGlobalMigration(): %v", err)
		}
		if got != p || !exists {
			t.Errorf("PendingGlobalMigration() = path %q, exists %v; want %q, true", got, exists, p)
		}
		if pending == nil {
			t.Fatal("PendingGlobalMigration() reported nothing pending for a v1 config")
		}
		if pending.Path != p || pending.From != 1 || pending.To != 2 {
			t.Errorf("pending = {Path: %q, From: %d, To: %d}; want {%q, 1, 2}", pending.Path, pending.From, pending.To, p)
		}
		if string(pending.Original) != string(original) {
			t.Errorf("pending.Original = %q; want %q", pending.Original, original)
		}
		wantMigrated := "{\n  \"default_base\": \"develop\",\n  \"version\": 2\n}\n"
		if string(pending.Migrated) != wantMigrated {
			t.Errorf("pending.Migrated =\n%q\nwant\n%q", pending.Migrated, wantMigrated)
		}
		assertUntouched(t, dir, p, original)
	})

	t.Run("missing at an explicit TH_CONFIG", func(t *testing.T) {
		p := filepath.Join(t.TempDir(), "th.json")
		t.Setenv(EnvVar, p)

		pending, got, exists, err := PendingGlobalMigration()
		if err == nil {
			t.Fatal("PendingGlobalMigration() ignored a missing explicit $TH_CONFIG; want the same loud failure as Load")
		}
		if !strings.Contains(err.Error(), p) {
			t.Errorf("err = %v; want it to name %q", err, p)
		}
		if pending != nil || exists {
			t.Errorf("PendingGlobalMigration() = pending %+v, exists %v; want nil, false", pending, exists)
		}
		if got != p {
			t.Errorf("path = %q; want %q", got, p)
		}
	})

	t.Run("missing at the default location", func(t *testing.T) {
		t.Setenv(EnvVar, "")
		home := t.TempDir()
		t.Setenv("HOME", home)

		pending, got, exists, err := PendingGlobalMigration()
		if err != nil {
			t.Fatalf("PendingGlobalMigration() without a config file: %v", err)
		}
		if pending != nil || exists {
			t.Errorf("PendingGlobalMigration() = pending %+v, exists %v; want nil, false", pending, exists)
		}
		if want := filepath.Join(home, homeConfigDirName, globalFileName); got != want {
			t.Errorf("path = %q; want %q reported anyway, for the message", got, want)
		}
	})
}
