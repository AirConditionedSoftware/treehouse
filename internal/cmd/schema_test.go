package cmd_test

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AirConditionedSoftware/treehouse/internal/config"
)

// thRaw runs th with an isolated HOME — which is what keeps th schema install
// away from the real ~/.th — and returns stdout byte for byte, since the
// schema command's whole contract is what it writes there. $TH_CONFIG is
// always cleared first, so a test that cares passes its own in extraEnv.
func thRaw(t *testing.T, home, dir string, extraEnv []string, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	cmd := exec.Command(thBin, args...)
	cmd.Dir = dir
	cmd.Env = append(append(gitEnv(home), "TH_CONFIG="), extraEnv...)
	var so, se strings.Builder
	cmd.Stdout = &so
	cmd.Stderr = &se
	err = cmd.Run()
	return so.String(), se.String(), err
}

func TestSchemaPrint(t *testing.T) {
	home := t.TempDir()

	for _, tc := range []struct {
		name    string
		args    []string
		want    []byte
		track   string
		version int
	}{
		{"thrc", []string{"schema"}, config.ThrcSchema(), config.LocalFileName, config.CurrentLocalVersion()},
		{"global", []string{"schema", "--global"}, config.GlobalSchema(), "config.json", config.CurrentGlobalVersion()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stdout, stderr, err := thRaw(t, home, home, nil, tc.args...)
			if err != nil {
				t.Fatalf("th %v: %v\n%s", tc.args, err, stderr)
			}

			want := string(tc.want)
			if !strings.HasSuffix(want, "\n") {
				want += "\n"
			}
			if stdout != want {
				t.Errorf("stdout is not the embedded schema verbatim (%d bytes, want %d)", len(stdout), len(want))
			}
			var doc map[string]any
			if err := json.Unmarshal([]byte(stdout), &doc); err != nil {
				t.Fatalf("stdout does not parse as JSON: %v", err)
			}
			if doc["properties"] == nil {
				t.Errorf("schema has no properties: %v", doc)
			}

			// The track and its version are the human half, and they must
			// stay off the machine channel.
			if stderr == "" {
				t.Error("stderr is empty; want the track and its schema version")
			}
			for _, w := range []string{tc.track, fmt.Sprintf("v%d", tc.version)} {
				if !strings.Contains(stderr, w) {
					t.Errorf("stderr = %q; want it to mention %q", stderr, w)
				}
			}
		})
	}
}

// settingsDoc decodes a settings.json the install command wrote.
func settingsDoc(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("%s does not parse as JSON: %v\n%s", path, err, data)
	}
	return doc
}

// schemaEntries returns the json.schemas array of a settings document.
func schemaEntries(t *testing.T, doc map[string]any) []map[string]any {
	t.Helper()
	list, ok := doc["json.schemas"].([]any)
	if !ok {
		t.Fatalf("json.schemas = %v; want an array", doc["json.schemas"])
	}
	var out []map[string]any
	for _, e := range list {
		m, ok := e.(map[string]any)
		if !ok {
			t.Fatalf("json.schemas entry = %v; want an object", e)
		}
		out = append(out, m)
	}
	return out
}

// entryFor returns the single json.schemas entry whose url is url.
func entryFor(t *testing.T, entries []map[string]any, url string) map[string]any {
	t.Helper()
	var found map[string]any
	for _, e := range entries {
		if e["url"] == url {
			if found != nil {
				t.Fatalf("json.schemas has more than one entry for %s", url)
			}
			found = e
		}
	}
	if found == nil {
		t.Fatalf("json.schemas has no entry for %s: %v", url, entries)
	}
	return found
}

// fileMatches returns an entry's fileMatch globs.
func fileMatches(t *testing.T, entry map[string]any) []string {
	t.Helper()
	list, ok := entry["fileMatch"].([]any)
	if !ok {
		t.Fatalf("fileMatch = %v; want an array", entry["fileMatch"])
	}
	var out []string
	for _, v := range list {
		s, ok := v.(string)
		if !ok {
			t.Fatalf("fileMatch entry = %v; want a string", v)
		}
		out = append(out, s)
	}
	return out
}

func schemaPaths(home string) (thrc, global string) {
	return filepath.Join(home, ".th", "thrc.schema.json"),
		filepath.Join(home, ".th", "config.schema.json")
}

func TestSchemaInstall(t *testing.T) {
	home := t.TempDir()
	// The parent directories are missing on purpose: a machine without VS
	// Code's settings directory still installs.
	settings := filepath.Join(t.TempDir(), "Code", "User", "settings.json")

	stdout, stderr, err := thRaw(t, home, home, nil, "schema", "install", "--settings-path", settings)
	if err != nil {
		t.Fatalf("th schema install: %v\n%s", err, stderr)
	}
	if stdout != "" {
		t.Errorf("stdout = %q; want nothing (install has no machine contract)", stdout)
	}

	thrcSchema, globalSchema := schemaPaths(home)
	for _, f := range []struct {
		path string
		want []byte
	}{{thrcSchema, config.ThrcSchema()}, {globalSchema, config.GlobalSchema()}} {
		got, err := os.ReadFile(f.path)
		if err != nil {
			t.Fatalf("%s: %v", f.path, err)
		}
		if string(got) != string(f.want) {
			t.Errorf("%s is not the embedded schema", f.path)
		}
	}

	doc := settingsDoc(t, settings)
	assoc, ok := doc["files.associations"].(map[string]any)
	if !ok {
		t.Fatalf("files.associations = %v; want an object", doc["files.associations"])
	}
	if assoc[".thrc"] != "json" {
		t.Errorf("files.associations[\".thrc\"] = %v; want \"json\"", assoc[".thrc"])
	}
	entries := schemaEntries(t, doc)
	if len(entries) != 2 {
		t.Fatalf("json.schemas has %d entries; want 2", len(entries))
	}
	if got := fileMatches(t, entryFor(t, entries, thrcSchema)); len(got) != 1 || got[0] != "**/.thrc" {
		t.Errorf("thrc fileMatch = %v; want [**/.thrc]", got)
	}
	if got := fileMatches(t, entryFor(t, entries, globalSchema)); len(got) != 1 || got[0] != "**/.th/config.json" {
		t.Errorf("global fileMatch = %v; want [**/.th/config.json]", got)
	}

	// Re-running is the documented way to refresh after upgrading th, so it
	// has to be quiet and byte-stable.
	first, err := os.ReadFile(settings)
	if err != nil {
		t.Fatal(err)
	}
	if _, stderr, err = thRaw(t, home, home, nil, "schema", "install", "--settings-path", settings); err != nil {
		t.Fatalf("second th schema install: %v\n%s", err, stderr)
	}
	if !strings.Contains(stderr, "unchanged") {
		t.Errorf("stderr = %q; want a re-run to report that nothing changed", stderr)
	}
	second, err := os.ReadFile(settings)
	if err != nil {
		t.Fatal(err)
	}
	if string(second) != string(first) {
		t.Errorf("settings.json changed on a second install:\n%s\n%s", first, second)
	}
	files, err := os.ReadDir(filepath.Dir(settings))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 {
		t.Errorf("settings directory holds %d files; want only settings.json (a no-op leaves no backup)", len(files))
	}
}

func TestSchemaInstallExistingSettings(t *testing.T) {
	home := t.TempDir()
	dir := t.TempDir()
	settings := filepath.Join(dir, "settings.json")
	// A foreign key, a foreign association, a foreign schema entry, and a
	// stale entry of ours carried over from another machine by settings sync.
	original := `{
  "editor.fontSize": 12,
  "files.associations": {
    "*.foo": "json"
  },
  "json.schemas": [
    {
      "fileMatch": ["**/foo.json"],
      "url": "https://example.com/foo.json"
    },
    {
      "fileMatch": ["**/.thrc"],
      "url": "/Users/someone-else/.th/thrc.schema.json"
    }
  ]
}
`
	if err := os.WriteFile(settings, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, stderr, err := thRaw(t, home, home, nil, "schema", "install", "--settings-path", settings); err != nil {
		t.Fatalf("th schema install: %v\n%s", err, stderr)
	}

	doc := settingsDoc(t, settings)
	if doc["editor.fontSize"] != float64(12) {
		t.Errorf("editor.fontSize = %v; want the foreign key carried through", doc["editor.fontSize"])
	}
	assoc, ok := doc["files.associations"].(map[string]any)
	if !ok {
		t.Fatalf("files.associations = %v; want an object", doc["files.associations"])
	}
	if assoc["*.foo"] != "json" || assoc[".thrc"] != "json" {
		t.Errorf("files.associations = %v; want the foreign association kept and .thrc added", assoc)
	}

	entries := schemaEntries(t, doc)
	if len(entries) != 3 {
		t.Fatalf("json.schemas has %d entries; want 3 (foreign kept, stale replaced, global added)", len(entries))
	}
	if entries[0]["url"] != "https://example.com/foo.json" {
		t.Errorf("json.schemas[0] = %v; want the foreign entry untouched and first", entries[0])
	}
	thrcSchema, globalSchema := schemaPaths(home)
	// entryFor fails if the stale entry was duplicated instead of replaced.
	entryFor(t, entries, thrcSchema)
	entryFor(t, entries, globalSchema)
	for _, e := range entries {
		if e["url"] == "/Users/someone-else/.th/thrc.schema.json" {
			t.Errorf("the stale entry survived: %v", e)
		}
	}

	files, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	backup := ""
	for _, f := range files {
		if strings.HasPrefix(f.Name(), "settings.json.") && strings.HasSuffix(f.Name(), ".bak") {
			backup = filepath.Join(dir, f.Name())
		}
	}
	if backup == "" {
		t.Fatalf("no timestamped backup beside the rewritten settings.json: %v", files)
	}
	saved, err := os.ReadFile(backup)
	if err != nil {
		t.Fatal(err)
	}
	if string(saved) != original {
		t.Errorf("backup = %q; want the pre-patch content", saved)
	}
}

// A settings.json with comments is JSONC, which VS Code accepts and
// encoding/json does not. th must refuse it rather than reformat it.
func TestSchemaInstallJSONC(t *testing.T) {
	home := t.TempDir()
	dir := t.TempDir()
	settings := filepath.Join(dir, "settings.json")
	original := "{\n  // my settings\n  \"editor.fontSize\": 12\n}\n"
	if err := os.WriteFile(settings, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	_, stderr, err := thRaw(t, home, home, nil, "schema", "install", "--settings-path", settings)
	if err == nil {
		t.Fatalf("th schema install on a JSONC settings file exited 0\n%s", stderr)
	}

	got, readErr := os.ReadFile(settings)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(got) != original {
		t.Errorf("settings.json = %q; want it untouched", got)
	}

	// The snippet must be pasteable as-is: absolute paths, since VS Code
	// does not expand ~ in a schema url.
	thrcSchema, globalSchema := schemaPaths(home)
	for _, want := range []string{"files.associations", "json.schemas", "**/.thrc", thrcSchema, globalSchema} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr = %q; want the snippet to contain %q", stderr, want)
		}
	}

	// The schema files land before the refusal, so the pasted snippet works
	// immediately.
	for _, path := range []string{thrcSchema, globalSchema} {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("%s: %v; want it written before the refusal", path, err)
		}
	}
}

func TestSchemaInstallDryRun(t *testing.T) {
	home := t.TempDir()
	dir := t.TempDir()
	settings := filepath.Join(dir, "settings.json")

	stdout, stderr, err := thRaw(t, home, home, nil, "schema", "install", "--dry-run", "--settings-path", settings)
	if err != nil {
		t.Fatalf("th schema install --dry-run: %v\n%s", err, stderr)
	}
	if stdout != "" {
		t.Errorf("stdout = %q; want nothing", stdout)
	}

	if _, err := os.Stat(filepath.Join(home, ".th")); !os.IsNotExist(err) {
		t.Errorf("~/.th exists after a dry run (err = %v)", err)
	}
	if _, err := os.Stat(settings); !os.IsNotExist(err) {
		t.Errorf("settings.json exists after a dry run (err = %v)", err)
	}

	thrcSchema, globalSchema := schemaPaths(home)
	for _, want := range []string{"dry run", thrcSchema, globalSchema, settings, "would"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr = %q; want it to mention %q", stderr, want)
		}
	}
}

// A config relocated with $TH_CONFIG no longer matches the ~/.th glob, so its
// path joins the global schema's fileMatch.
func TestSchemaInstallTHConfigFileMatch(t *testing.T) {
	home := t.TempDir()
	dir := t.TempDir()
	settings := filepath.Join(dir, "settings.json")
	cfg := filepath.Join(dir, "th.json")

	if _, stderr, err := thRaw(t, home, home, []string{"TH_CONFIG=" + cfg},
		"schema", "install", "--settings-path", settings); err != nil {
		t.Fatalf("th schema install: %v\n%s", err, stderr)
	}

	_, globalSchema := schemaPaths(home)
	entry := entryFor(t, schemaEntries(t, settingsDoc(t, settings)), globalSchema)
	got := fileMatches(t, entry)
	want := []string{"**/.th/config.json", cfg}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("global fileMatch = %v; want %v", got, want)
	}
}
