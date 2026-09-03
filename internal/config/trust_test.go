package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

// trustHome isolates the trust store in a temp HOME and returns its path.
func trustHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	return filepath.Join(home, ".th", TrustFileName)
}

func TestApproveCommandsRoundTrip(t *testing.T) {
	path := trustHome(t)
	main := t.TempDir()

	tests := []struct {
		name string
		hook string
		cmds []string
	}{
		{"one command", "post_create", []string{"npm ci"}},
		{"several commands", "post_create", []string{"direnv allow", "npm ci"}},
		{"empty list", "post_create", []string{}},
		{"pre_create", "pre_create", []string{"check-vpn"}},
		{"pre_remove", "pre_remove", []string{"docker compose down"}},
		{"post_remove", "post_remove", []string{"docker volume rm x"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := ApproveCommands(main, tt.hook, tt.cmds); err != nil {
				t.Fatalf("ApproveCommands: %v", err)
			}
			got, ok := ApprovedCommands(main, tt.hook)
			if !ok {
				t.Fatal("ApprovedCommands() found no record after approving")
			}
			if !reflect.DeepEqual(got, tt.cmds) {
				t.Errorf("ApprovedCommands() = %#v; want %#v", got, tt.cmds)
			}
		})
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("trust file: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("trust file mode = %v; want 0600", perm)
	}

	var tf trustFile
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &tf); err != nil {
		t.Fatalf("trust file is not valid JSON: %v", err)
	}
	rec, ok := tf.Repos[normalizePath(main)]
	if !ok {
		t.Fatalf("trust file keyed by %v; want the normalized main path %q", tf.Repos, normalizePath(main))
	}
	for _, hook := range []string{"pre_create", "post_create", "pre_remove", "post_remove"} {
		ha, ok := rec.Hooks[hook]
		if !ok {
			t.Fatalf("no hooks entry for %s in %v", hook, rec.Hooks)
		}
		if _, err := time.Parse(time.RFC3339, ha.ApprovedAt); err != nil {
			t.Errorf("%s approved_at = %q; want RFC3339: %v", hook, ha.ApprovedAt, err)
		}
	}
}

func TestApprovedCommandsNoRecord(t *testing.T) {
	trustHome(t)
	other := t.TempDir()
	main := t.TempDir()

	if got, ok := ApprovedCommands(main, "post_create"); ok || got != nil {
		t.Errorf("ApprovedCommands() with no trust file = %#v, %v; want nil, false", got, ok)
	}
	if err := ApproveCommands(other, "post_create", []string{"npm ci"}); err != nil {
		t.Fatal(err)
	}
	if got, ok := ApprovedCommands(main, "post_create"); ok || got != nil {
		t.Errorf("ApprovedCommands() for an unapproved repo = %#v, %v; want nil, false", got, ok)
	}
	// An approval for one hook is no record for another.
	if err := ApproveCommands(main, "pre_remove", []string{"make down"}); err != nil {
		t.Fatal(err)
	}
	if got, ok := ApprovedCommands(main, "post_remove"); ok || got != nil {
		t.Errorf("ApprovedCommands(post_remove) with only pre_remove approved = %#v, %v; want nil, false", got, ok)
	}
	if got, ok := ApprovedCommands(main, "post_create"); ok || got != nil {
		t.Errorf("ApprovedCommands(post_create) with only pre_remove approved = %#v, %v; want nil, false", got, ok)
	}
}

func TestApproveCommandsOverwritesPerHook(t *testing.T) {
	trustHome(t)
	main := t.TempDir()
	other := t.TempDir()

	if err := ApproveCommands(other, "post_create", []string{"make setup"}); err != nil {
		t.Fatal(err)
	}
	if err := ApproveCommands(main, "post_create", []string{"npm install"}); err != nil {
		t.Fatal(err)
	}
	if err := ApproveCommands(main, "pre_remove", []string{"docker compose down"}); err != nil {
		t.Fatal(err)
	}
	if err := ApproveCommands(main, "post_create", []string{"npm ci"}); err != nil {
		t.Fatal(err)
	}

	got, ok := ApprovedCommands(main, "post_create")
	if !ok || !reflect.DeepEqual(got, []string{"npm ci"}) {
		t.Errorf("ApprovedCommands() after re-approving = %#v, %v; want [npm ci], true", got, ok)
	}
	// Re-approving one hook must not clobber another's approval …
	if got, ok := ApprovedCommands(main, "pre_remove"); !ok || !reflect.DeepEqual(got, []string{"docker compose down"}) {
		t.Errorf("ApprovedCommands(pre_remove) after re-approving post_create = %#v, %v; want [docker compose down], true", got, ok)
	}
	// … nor another repo's record.
	if got, ok := ApprovedCommands(other, "post_create"); !ok || !reflect.DeepEqual(got, []string{"make setup"}) {
		t.Errorf("ApprovedCommands() for the other repo = %#v, %v; want [make setup], true", got, ok)
	}
}

func TestApprovedCommandsLegacyRecord(t *testing.T) {
	path := trustHome(t)
	main := t.TempDir()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	legacy := `{"repos":{"` + normalizePath(main) + `":{"post_create":["npm ci"],"approved_at":"2026-01-01T00:00:00Z"}}}`
	if err := os.WriteFile(path, []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}

	// A record from before per-hook approvals still answers for post_create
	// and only for post_create.
	if got, ok := ApprovedCommands(main, "post_create"); !ok || !reflect.DeepEqual(got, []string{"npm ci"}) {
		t.Errorf("ApprovedCommands() on a legacy record = %#v, %v; want [npm ci], true", got, ok)
	}
	if got, ok := ApprovedCommands(main, "pre_remove"); ok || got != nil {
		t.Errorf("ApprovedCommands(pre_remove) on a legacy record = %#v, %v; want nil, false", got, ok)
	}

	// Any write migrates the legacy fields into the per-hook layout without
	// losing the post_create approval.
	if err := ApproveCommands(main, "pre_remove", []string{"make down"}); err != nil {
		t.Fatal(err)
	}
	if got, ok := ApprovedCommands(main, "post_create"); !ok || !reflect.DeepEqual(got, []string{"npm ci"}) {
		t.Errorf("ApprovedCommands(post_create) after migration = %#v, %v; want [npm ci], true", got, ok)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var tf trustFile
	if err := json.Unmarshal(data, &tf); err != nil {
		t.Fatal(err)
	}
	rec := tf.Repos[normalizePath(main)]
	if rec.PostCreate != nil || rec.ApprovedAt != "" {
		t.Errorf("legacy fields survived the migration: %+v", rec)
	}
	if ha := rec.Hooks["post_create"]; !reflect.DeepEqual(ha.Commands, []string{"npm ci"}) || ha.ApprovedAt != "2026-01-01T00:00:00Z" {
		t.Errorf("migrated post_create entry = %+v; want the legacy commands and timestamp", ha)
	}
}

func TestApprovedCommandsCorruptFile(t *testing.T) {
	path := trustHome(t)
	main := t.TempDir()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"repos": {`), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, ok := ApprovedCommands(main, "post_create"); ok || got != nil {
		t.Errorf("ApprovedCommands() with a corrupt trust file = %#v, %v; want nil, false", got, ok)
	}
	// A corrupt store must not wedge approvals: writing repairs it.
	if err := ApproveCommands(main, "post_create", []string{"npm ci"}); err != nil {
		t.Fatalf("ApproveCommands over a corrupt trust file: %v", err)
	}
	if got, ok := ApprovedCommands(main, "post_create"); !ok || !reflect.DeepEqual(got, []string{"npm ci"}) {
		t.Errorf("ApprovedCommands() after repair = %#v, %v; want [npm ci], true", got, ok)
	}
}

func TestTrustFileIgnoresWTConfig(t *testing.T) {
	path := trustHome(t)
	t.Setenv(EnvVar, filepath.Join(t.TempDir(), "elsewhere.json"))
	main := t.TempDir()

	if err := ApproveCommands(main, "post_create", []string{"npm ci"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("trust file should live at ~/.th/%s regardless of $%s: %v", TrustFileName, EnvVar, err)
	}
}
