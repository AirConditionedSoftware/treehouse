package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// This file is the schema-versioning engine shared by the global config and
// the repo-local .thrc. Both files carry a "version" (absent means 1) and
// each has its own registry of forward-only steps; the two tracks are
// independent and may drift apart.
//
// Steps transform raw JSON rather than typed structs: the shape a step reads
// stopped existing as a Go type the moment the schema moved on. After the
// chain runs, the result is stamped with the current version and decoded
// into the current struct with unknown fields rejected, which is the real
// validation.

// migration is one forward step: it consumes a document at version from and
// produces one at from+1. apply receives the decoded document, with numbers
// as json.Number so untouched values survive the round trip byte-faithfully,
// and must not touch "version" — the engine stamps it.
type migration struct {
	from  int
	apply func(m map[string]any) (map[string]any, error)
}

// currentVersion derives a track's version from its registry: entry i
// migrates version i+1 to i+2, so an empty registry means version 1 and
// appending a step bumps the version automatically.
func currentVersion(steps []migration) int { return len(steps) + 1 }

// CurrentGlobalVersion is the schema version th writes into the global
// config file.
func CurrentGlobalVersion() int { return currentVersion(globalMigrations) }

// CurrentLocalVersion is the schema version th writes into a .thrc.
func CurrentLocalVersion() int { return currentVersion(localMigrations) }

// sniffVersion reads only the "version" field, leniently: unknown fields are
// ignored, because an unknown shape is exactly what a migration is for.
// Absent or 0 means version 1 — no file in the wild carries a version yet.
// Anything that is not a non-negative integer is a broken file, not an old
// one, and fails loudly.
func sniffVersion(raw []byte) (int, error) {
	var probe struct {
		Version any `json:"version"`
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&probe); err != nil {
		return 0, err
	}
	switch v := probe.Version.(type) {
	case nil:
		return 1, nil
	case json.Number:
		n, err := v.Int64()
		if err != nil {
			return 0, fmt.Errorf("schema version %s is not an integer", v)
		}
		if n < 0 {
			return 0, fmt.Errorf("schema version %d is negative", n)
		}
		if n == 0 {
			return 1, nil
		}
		return int(n), nil
	default:
		return 0, fmt.Errorf("schema version must be a number, got %T", probe.Version)
	}
}

// TooNewError reports a file whose schema version is ahead of this binary.
// Migrations are forward-only, so the only fix is a newer th.
type TooNewError struct {
	// Desc names the file, as in the package's other errors: "config
	// <path>" or "repo config <path>".
	Desc string
	// Found is the version in the file, Supported the highest this binary
	// knows how to read.
	Found, Supported int
}

func (e *TooNewError) Error() string {
	return fmt.Sprintf("%s: schema version %d was written by a newer th (this version supports up to %d); upgrade th",
		e.Desc, e.Found, e.Supported)
}

// migrateRaw brings raw up to the track's current version by running every
// step from the file's version onwards, then stamping "version". It reports
// the versions it went from and to; when they are equal it returns raw
// itself, so an up-to-date file is never re-encoded and never rewritten.
// desc names the file for errors ("config <path>", "repo config <path>").
func migrateRaw(desc string, raw []byte, steps []migration) (out []byte, from, to int, err error) {
	to = currentVersion(steps)
	from, err = sniffVersion(raw)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("%s: %w", desc, err)
	}
	if from > to {
		return nil, 0, 0, &TooNewError{Desc: desc, Found: from, Supported: to}
	}
	if from == to {
		return raw, from, to, nil
	}

	var m map[string]any
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&m); err != nil {
		return nil, 0, 0, fmt.Errorf("%s: %w", desc, err)
	}
	for _, s := range steps {
		if s.from < from {
			continue
		}
		m, err = s.apply(m)
		if err != nil {
			return nil, 0, 0, fmt.Errorf("%s: migrating schema v%d to v%d: %w", desc, s.from, s.from+1, err)
		}
	}
	// A literal "null" document, or a step that returned no map, decodes to
	// a nil map, which cannot be stamped.
	if m == nil {
		m = map[string]any{}
	}
	m["version"] = to

	out, err = json.MarshalIndent(m, "", "  ")
	if err != nil {
		return nil, 0, 0, fmt.Errorf("%s: %w", desc, err)
	}
	return append(out, '\n'), from, to, nil
}

// backupTimeFormat stamps backup names: sortable, filename-safe, second
// resolution.
const backupTimeFormat = "20060102-150405"

// backupPath names the backup taken before migrating path away from version
// from: "<path>.v<from>.<timestamp>.bak". The timestamp means a backup is
// never overwritten, however often a file is migrated.
func backupPath(path string, from int, now time.Time) string {
	return fmt.Sprintf("%s.v%d.%s.bak", path, from, now.Format(backupTimeFormat))
}

// writeBackup saves the pre-migration bytes beside path, preserving the
// file's mode, and returns the backup's path. A plain write is enough: the
// original is still intact until the migrated content is renamed over it,
// so a half-written backup can only be a stale extra file.
func writeBackup(path string, from int, original []byte) (backup string, err error) {
	backup = backupPath(path, from, time.Now())
	if err := os.WriteFile(backup, original, modeOf(path)); err != nil {
		return "", err
	}
	return backup, nil
}

// atomicWrite replaces path with data via a temp file in the same directory
// plus a rename, so a reader never sees a half-written config. Same idiom as
// the trust store's writer.
func atomicWrite(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+"-*")
	if err != nil {
		return err
	}
	// Removing the temp file is a no-op once the rename has consumed it.
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// modeOf reports path's permission bits. The file was just read, so the stat
// only fails in a race; 0600 is the conservative answer for a file th is
// about to rewrite.
func modeOf(path string) os.FileMode {
	fi, err := os.Stat(path)
	if err != nil {
		return 0o600
	}
	return fi.Mode().Perm()
}

// PendingMigration is a file that was migrated in memory but not written
// back — the .thrc path, where the decision to rewrite a repo-owned file
// belongs to the user, and the inspection API below, which never writes at
// all. Exported so internal/cmd can offer the backup, then persist.
type PendingMigration struct {
	// desc names the file for errors, as elsewhere in the package: "config
	// <path>" or "repo config <path>". Unexported and optional: a
	// zero-valued construction describes itself as a repo config, which is
	// what the .thrc path — the original and still the common one — wants.
	desc string
	// Path is the file the content came from.
	Path string
	// From is the version on disk, To the version th now uses in memory.
	From, To int
	// Original is the pre-migration content, what WriteBackup saves.
	Original []byte
	// Migrated is the post-migration content, what Persist writes.
	Migrated []byte
}

// describe names the file for errors, defaulting to the .thrc wording.
func (p *PendingMigration) describe() string {
	if p.desc != "" {
		return p.desc
	}
	return "repo config " + p.Path
}

// WriteBackup saves the pre-migration content next to the file and returns
// the backup's path. The file itself is left alone.
func (p *PendingMigration) WriteBackup() (string, error) {
	backup, err := writeBackup(p.Path, p.From, p.Original)
	if err != nil {
		return "", fmt.Errorf("%s: backing up before migration: %w", p.describe(), err)
	}
	return backup, nil
}

// Persist writes the migrated content over the file, atomically, keeping the
// file's mode.
func (p *PendingMigration) Persist() error {
	if err := atomicWrite(p.Path, p.Migrated, modeOf(p.Path)); err != nil {
		return fmt.Errorf("%s: writing migrated config: %w", p.describe(), err)
	}
	return nil
}

// migratePending is the one computation shared by the loaders and the
// read-only inspection API: it migrates raw in memory and reports both the
// migrated bytes (raw itself when the file was already current) and, when
// the version moved, the PendingMigration describing the rewrite the file
// has not had yet. Nothing is written; what the caller does with the pending
// migration is the caller's policy.
func migratePending(desc, path string, raw []byte, steps []migration) ([]byte, *PendingMigration, error) {
	migrated, from, to, err := migrateRaw(desc, raw, steps)
	if err != nil {
		return nil, nil, err
	}
	if from == to {
		return migrated, nil, nil
	}
	return migrated, &PendingMigration{desc: desc, Path: path, From: from, To: to, Original: raw, Migrated: migrated}, nil
}

// PendingLocalMigration reports what migrating <mainPath>/.thrc would do,
// without touching the file: nil pending means the file is already on the
// current schema, exists false means there is no .thrc there (not an error —
// a repo without one has nothing to migrate).
//
// This is the read-only counterpart to Resolve: Resolve loads the whole
// configuration and, in doing so, rewrites an outdated *global* file as a
// side effect. A caller that only wants to know what is outstanding — or
// that must decide per file whether to write — asks here instead.
//
// The migrated bytes are strictly decoded as a cheap proof the steps
// produced something this binary can still load. validateVSCode is
// deliberately not run: inspection is about the schema version, not about
// whether the settings are semantically valid, and the shipped posture is to
// migrate first and validate at load.
func PendingLocalMigration(mainPath string) (pending *PendingMigration, path string, exists bool, err error) {
	path = filepath.Join(mainPath, LocalFileName)
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, path, false, nil
	}
	if err != nil {
		return nil, path, false, fmt.Errorf("repo config %s: %w", path, err)
	}
	desc := "repo config " + path
	_, pending, err = migratePending(desc, path, raw, localMigrations)
	if err != nil {
		return nil, path, true, err
	}
	if pending != nil {
		dec := json.NewDecoder(bytes.NewReader(pending.Migrated))
		dec.DisallowUnknownFields()
		var local LocalConfig
		if err := dec.Decode(&local); err != nil {
			return nil, path, true, fmt.Errorf("%s: %w", desc, err)
		}
	}
	return pending, path, true, nil
}

// PendingGlobalMigration reports what migrating the global config file would
// do, without touching it: nil pending means it is already on the current
// schema, exists false means there is no file at the default location. A
// missing file at an explicit $TH_CONFIG is an error, as in Load — a typo'd
// path fails loudly instead of reading as "nothing to do".
//
// Being read-only is the whole point. Load migrates an outdated global file
// and rewrites it in place on every call, so a caller that wants to preview
// the rewrite, or to leave the file alone until the user says otherwise,
// must not go through Load. See PendingLocalMigration on the strict decode
// and on why validateVSCode is not run.
func PendingGlobalMigration() (pending *PendingMigration, path string, exists bool, err error) {
	path, explicit, err := Path()
	if err != nil {
		return nil, "", false, err
	}
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) && !explicit {
		return nil, path, false, nil
	}
	if err != nil {
		return nil, path, false, fmt.Errorf("config %s: %w", path, err)
	}
	desc := "config " + path
	_, pending, err = migratePending(desc, path, raw, globalMigrations)
	if err != nil {
		return nil, path, true, err
	}
	if pending != nil {
		dec := json.NewDecoder(bytes.NewReader(pending.Migrated))
		dec.DisallowUnknownFields()
		var cfg File
		if err := dec.Decode(&cfg); err != nil {
			return nil, path, true, fmt.Errorf("%s: %w", desc, err)
		}
	}
	return pending, path, true, nil
}
