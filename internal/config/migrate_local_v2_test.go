package config

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"
)

func TestMigrateLocalV2NestsVSCodeKeys(t *testing.T) {
	raw := `{
  "name": "solo",
  "branch_prefix": "team",
  "vscode_workspace_file": true,
  "vscode_window_color": "auto",
  "workspace_paths": [{"path": "/shared/lib"}]
}`
	out, from, to, err := migrateRaw("repo config /tmp/"+LocalFileName, []byte(raw), localMigrations)
	if err != nil {
		t.Fatalf("migrateRaw: %v", err)
	}
	if want := CurrentLocalVersion(); from != 1 || to != want {
		t.Fatalf("migrateRaw reported v%d -> v%d; want v1 -> v%d", from, to, want)
	}

	dec := json.NewDecoder(bytes.NewReader(out))
	dec.DisallowUnknownFields()
	var got LocalConfig
	if err := dec.Decode(&got); err != nil {
		t.Fatalf("migrated %s does not decode: %v\n%s", LocalFileName, err, out)
	}
	want := LocalConfig{
		Version: CurrentLocalVersion(),
		Name:    "solo",
		Settings: Settings{
			BranchPrefix: "team",
			VSCode: &VSCode{
				WorkspaceFile:  boolPtr(true),
				WindowColor:    "auto",
				WorkspacePaths: []WorkspacePath{{Path: "/shared/lib"}},
			},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("migrated %s = %+v; want %+v", LocalFileName, got, want)
	}

	var m map[string]any
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatalf("migrated output is not JSON: %v", err)
	}
	assertNoFlatVSCodeKeys(t, LocalFileName, m)
}

func TestMigrateLocalV2NoKeysCreatesNoObject(t *testing.T) {
	out, _, _, err := migrateRaw("repo config /tmp/"+LocalFileName, []byte(`{"branch_prefix": "team"}`), localMigrations)
	if err != nil {
		t.Fatalf("migrateRaw: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatalf("migrated output is not JSON: %v", err)
	}
	if _, ok := m["vscode"]; ok {
		t.Errorf("migrated %s gained a vscode object with nothing to put in it:\n%s", LocalFileName, out)
	}
}
