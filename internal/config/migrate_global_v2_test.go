package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// The v2 fixtures run through migrateRaw with the real registry rather than
// calling the step directly: that is the call Load makes, so they also prove
// the chain runs, stamps the version, and produces a document the current
// struct still accepts.

// migrateGlobalV2Fixture migrates a v1 document and returns the migrated
// bytes plus the document as a generic map, for asserting what did — and did
// not — end up in the file.
func migrateGlobalV2Fixture(t *testing.T, raw string) ([]byte, map[string]any) {
	t.Helper()
	out, from, to, err := migrateRaw("config /tmp/th.json", []byte(raw), globalMigrations)
	if err != nil {
		t.Fatalf("migrateRaw(%s): %v", raw, err)
	}
	if want := CurrentGlobalVersion(); from != 1 || to != want {
		t.Fatalf("migrateRaw reported v%d -> v%d; want v1 -> v%d", from, to, want)
	}
	var m map[string]any
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatalf("migrated output is not JSON: %v", err)
	}
	return out, m
}

// decodeGlobalStrict decodes migrated bytes the way Load does. A step whose
// output the current struct rejects has not finished the job.
func decodeGlobalStrict(t *testing.T, out []byte) File {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(out))
	dec.DisallowUnknownFields()
	var cfg File
	if err := dec.Decode(&cfg); err != nil {
		t.Fatalf("migrated config does not decode: %v\n%s", err, out)
	}
	return cfg
}

// assertNoFlatVSCodeKeys checks that none of the v1 spellings survive in m.
// where names the object for the failure message.
func assertNoFlatVSCodeKeys(t *testing.T, where string, m map[string]any) {
	t.Helper()
	for _, r := range vscodeV2Renames {
		if _, ok := m[r[0]]; ok {
			t.Errorf("%s still has the flat key %q", where, r[0])
		}
	}
}

// reposEntry returns m's repos entry i, failing if it is not an object.
func reposEntry(t *testing.T, m map[string]any, i int) map[string]any {
	t.Helper()
	repos, ok := m["repos"].([]any)
	if !ok || len(repos) <= i {
		t.Fatalf("migrated config has no repos[%d]: %v", i, m["repos"])
	}
	entry, ok := repos[i].(map[string]any)
	if !ok {
		t.Fatalf("repos[%d] = %v; want an object", i, repos[i])
	}
	return entry
}

func TestMigrateGlobalV2NestsVSCodeKeys(t *testing.T) {
	raw := `{
  "branch_prefix": "peter",
  "vscode_open": true,
  "vscode_workspace_file": true,
  "vscode_workspace_prefix": "th-",
  "vscode_window_title": "myapp ${activeEditorShort}",
  "vscode_window_color": "auto",
  "workspace_paths": [{"name": "docs", "path": "~/notes"}],
  "repos": [
    {"name": "myapp", "path": "/code/myapp", "vscode_workspace_file": true, "vscode_window_color": "#aabbcc"},
    {"path": "/code/plain", "default_base": "trunk"}
  ]
}`
	out, m := migrateGlobalV2Fixture(t, raw)
	got := decodeGlobalStrict(t, out)
	want := File{
		Version: CurrentGlobalVersion(),
		Settings: Settings{
			BranchPrefix: "peter",
			VSCode: &VSCode{
				Open:            boolPtr(true),
				WorkspaceFile:   boolPtr(true),
				WorkspacePrefix: "th-",
				WindowTitle:     "myapp ${activeEditorShort}",
				WindowColor:     "auto",
				WorkspacePaths:  []WorkspacePath{{Name: "docs", Path: "~/notes"}},
			},
		},
		Repos: []RepoConfig{
			{Name: "myapp", Path: "/code/myapp", Settings: Settings{VSCode: &VSCode{WorkspaceFile: boolPtr(true), WindowColor: "#aabbcc"}}},
			{Path: "/code/plain", Settings: Settings{DefaultBase: "trunk"}},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("migrated config = %+v; want %+v", got, want)
	}
	// An entry with no VS Code keys gains no object at all.
	if got.Repos[1].VSCode != nil {
		t.Errorf("repos[1] = %+v; want a nil vscode object", got.Repos[1].VSCode)
	}
	assertNoFlatVSCodeKeys(t, "top level", m)
	assertNoFlatVSCodeKeys(t, "repos[0]", reposEntry(t, m, 0))
}

func TestMigrateGlobalV2NoKeysCreatesNoObject(t *testing.T) {
	out, m := migrateGlobalV2Fixture(t, `{"branch_prefix": "peter", "repos": [{"path": "/x", "default_base": "trunk"}]}`)
	if _, ok := m["vscode"]; ok {
		t.Errorf("migrated config gained a vscode object with nothing to put in it:\n%s", out)
	}
	if _, ok := reposEntry(t, m, 0)["vscode"]; ok {
		t.Errorf("repos[0] gained a vscode object with nothing to put in it:\n%s", out)
	}
}

func TestMigrateGlobalV2MovesOnlyKeysThatExist(t *testing.T) {
	_, m := migrateGlobalV2Fixture(t, `{"branch_prefix": "peter", "vscode_window_color": "auto"}`)
	want := map[string]any{"window_color": "auto"}
	if got, _ := m["vscode"].(map[string]any); !reflect.DeepEqual(got, want) {
		t.Errorf("vscode = %v; want exactly %v", m["vscode"], want)
	}
}

func TestMigrateGlobalV2MergesIntoExistingObject(t *testing.T) {
	// A hand-written vscode object is kept; the moved key wins the overlap.
	_, m := migrateGlobalV2Fixture(t, `{"vscode": {"open": true, "window_color": "#aabbcc"}, "vscode_window_color": "auto"}`)
	want := map[string]any{"open": true, "window_color": "auto"}
	if got, _ := m["vscode"].(map[string]any); !reflect.DeepEqual(got, want) {
		t.Errorf("vscode = %v; want %v", m["vscode"], want)
	}
}

func TestMigrateGlobalV2RejectsNonObjectVSCode(t *testing.T) {
	tests := []struct {
		name, raw   string
		wantSubstrs []string
	}{
		{"top level", `{"vscode": "yes", "vscode_open": true}`, []string{"migrating schema v1 to v2", `"vscode" is string, want an object`}},
		{"repos entry", `{"repos": [{"path": "/x", "vscode": 42, "vscode_open": true}]}`, []string{"migrating schema v1 to v2", "repos[0]"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, _, err := migrateRaw("config /tmp/th.json", []byte(tt.raw), globalMigrations)
			if err == nil {
				t.Fatalf("migrateRaw(%s) should fail", tt.raw)
			}
			for _, s := range tt.wantSubstrs {
				if !strings.Contains(err.Error(), s) {
					t.Errorf("err = %v; want it to mention %q", err, s)
				}
			}
		})
	}
}

func TestMigrateGlobalV2MovesNullsVerbatim(t *testing.T) {
	out, m := migrateGlobalV2Fixture(t, `{"vscode_open": null, "vscode_window_title": null}`)
	want := map[string]any{"open": nil, "window_title": nil}
	if got, _ := m["vscode"].(map[string]any); !reflect.DeepEqual(got, want) {
		t.Errorf("vscode = %v; want the nulls moved verbatim %v", m["vscode"], want)
	}
	// A JSON null is a no-op for every field of VSCode, so a v1 file that
	// loaded before still loads after — the reason nulls can move blind.
	cfg := decodeGlobalStrict(t, out)
	if cfg.VSCode == nil {
		t.Fatal("migrated config has no vscode object")
	}
	if cfg.VSCode.Open != nil || cfg.VSCode.WindowTitle != "" {
		t.Errorf("vscode = %+v; want the nulls to decode as unset", cfg.VSCode)
	}
}

func TestMigrateGlobalV2SkipsNonObjectReposEntries(t *testing.T) {
	// Un-loadable as v1 too: the step leaves it for the strict decode after
	// the chain to reject with the error it always gave.
	out, _, _, err := migrateRaw("config /tmp/th.json", []byte(`{"repos": [42]}`), globalMigrations)
	if err != nil {
		t.Fatalf("migrateRaw: %v", err)
	}
	want := fmt.Sprintf("{\n  \"repos\": [\n    42\n  ],\n  \"version\": %d\n}\n", CurrentGlobalVersion())
	if string(out) != want {
		t.Errorf("migrated config =\n%q\nwant\n%q", out, want)
	}
}
