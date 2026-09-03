package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/AirConditionedSoftware/treehouse/internal/config"
	"github.com/spf13/cobra"
)

const (
	// The schema files always land in ~/.th, never beside $TH_CONFIG — same
	// rationale as trust.json: machine state, not configuration. VS Code
	// also needs one stable absolute path, since it does not expand ~ in a
	// schema url.
	schemaHomeDirName    = ".th"
	thrcSchemaFileName   = "thrc.schema.json"
	globalSchemaFileName = "config.schema.json"

	// VS Code's own setting names, and the association that makes an
	// extensionless .thrc parse as JSON.
	associationsKey  = "files.associations"
	schemasKey       = "json.schemas"
	thrcAssociation  = ".thrc"
	thrcFileMatch    = "**/.thrc"
	globalFileMatch  = "**/.th/config.json"
	associationValue = "json"

	// settingsBackupTimeFormat stamps the settings backup, mirroring the
	// config migration backups: sortable, filename-safe, second resolution.
	settingsBackupTimeFormat = "20060102-150405"
)

var (
	schemaInstallDryRun       bool
	schemaInstallSettingsPath string
)

var schemaInstallCmd = &cobra.Command{
	Use:   "install",
	Short: "Write both schemas to ~/.th and point VS Code's user settings at them",
	Long: `Write thrc.schema.json and config.schema.json to ~/.th, then patch VS
Code's user settings.json to use them: a files.associations entry so an
extensionless .thrc parses as JSON, and two json.schemas entries pointing
at the two local files by absolute path (VS Code does not expand ~). When
$TH_CONFIG is set, its path joins **/.th/config.json in the global
schema's fileMatch.

Installing is idempotent — a re-run refreshes the schema files and updates
the same settings entries in place, and reports that it changed nothing
when it did not. Re-run it after upgrading th: the schemas describe the
config schema this binary understands.

A settings.json that is not strict JSON — comments or trailing commas,
which VS Code accepts — is never rewritten. th prints the snippet to paste
and exits non-zero rather than reformatting the file behind your back; the
schema files are written first, so the snippet works immediately.

--settings-path names the settings.json to patch, which is how to reach
Insiders, VSCodium, or Cursor. --dry-run reports what would happen and
writes nothing. All output goes to stderr; stdout stays empty.`,
	Args: cobra.NoArgs,
	RunE: runSchemaInstall,
}

func runSchemaInstall(cmd *cobra.Command, args []string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	dir := filepath.Join(home, schemaHomeDirName)
	thrcPath := filepath.Join(dir, thrcSchemaFileName)
	globalPath := filepath.Join(dir, globalSchemaFileName)

	// Step 1: the schema files. They go down first so that the snippet the
	// JSONC path prints below is already true when it is printed.
	schemas := []struct {
		path string
		data []byte
	}{
		{thrcPath, config.ThrcSchema()},
		{globalPath, config.GlobalSchema()},
	}
	for _, s := range schemas {
		state := fileState(s.path, s.data)
		if schemaInstallDryRun {
			if state == stateUnchanged {
				fmt.Fprintf(os.Stderr, "%s: already current, would be left alone (dry run)\n", displayPath(s.path))
			} else {
				fmt.Fprintf(os.Stderr, "%s: would be %s (dry run)\n", displayPath(s.path), state)
			}
			continue
		}
		if state == stateUnchanged {
			fmt.Fprintf(os.Stderr, "%s: unchanged\n", displayPath(s.path))
			continue
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(s.path, s.data, 0o644); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "%s: %s\n", displayPath(s.path), state)
	}

	// Step 2: the settings file to patch.
	settingsPath := schemaInstallSettingsPath
	if settingsPath == "" {
		if settingsPath, err = vscodeUserSettingsPath(home); err != nil {
			return err
		}
	} else if settingsPath, err = absSettingsPath(settingsPath); err != nil {
		return err
	}

	// Step 3: the patch.
	entries := []map[string]any{
		{"fileMatch": []string{thrcFileMatch}, "url": thrcPath},
		{"fileMatch": globalFileMatches(), "url": globalPath},
	}

	raw, err := os.ReadFile(settingsPath)
	missing := errors.Is(err, os.ErrNotExist)
	if err != nil && !missing {
		return err
	}

	out, actions, err := patchSettings(raw, entries)
	if errors.Is(err, errNotStrictJSON) {
		return refuseJSONC(settingsPath, entries)
	}
	if err != nil {
		return fmt.Errorf("%s: %w", displayPath(settingsPath), err)
	}
	unchanged := !missing && bytes.Equal(out, raw)

	if schemaInstallDryRun {
		switch {
		case missing:
			fmt.Fprintf(os.Stderr, "%s: would be created (dry run)\n", displayPath(settingsPath))
		case unchanged:
			fmt.Fprintf(os.Stderr, "%s: would be left alone, it already has both schemas (dry run)\n", displayPath(settingsPath))
		default:
			fmt.Fprintf(os.Stderr, "%s: would be patched (dry run)\n", displayPath(settingsPath))
			fmt.Fprintf(os.Stderr, "  backup would be %s.<timestamp>.bak beside it\n", filepath.Base(settingsPath))
		}
		for _, a := range actions {
			fmt.Fprintf(os.Stderr, "  would %s\n", a)
		}
		return nil
	}

	if unchanged {
		fmt.Fprintf(os.Stderr, "%s: unchanged, it already points at both schemas\n", displayPath(settingsPath))
		return nil
	}

	if missing {
		if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
			return err
		}
	}
	backup := ""
	if !missing {
		// The file is still untouched, so a failed backup costs nothing but
		// the command.
		if backup, err = backupSettings(settingsPath, raw); err != nil {
			return err
		}
	}
	if err := atomicWriteFile(settingsPath, out, settingsMode(settingsPath)); err != nil {
		return err
	}

	verb := "patched"
	if missing {
		verb = "created"
	}
	msg := fmt.Sprintf("%s: %s", displayPath(settingsPath), verb)
	if backup != "" {
		msg += fmt.Sprintf(" (backup: %s)", displayPath(backup))
	}
	fmt.Fprintln(os.Stderr, msg)
	for _, a := range actions {
		fmt.Fprintf(os.Stderr, "  %s\n", a)
	}
	fmt.Fprintln(os.Stderr, "Reload VS Code to pick up the schemas.")
	return nil
}

// File states, shared by the dry run and the real write so both describe the
// same thing.
const (
	stateCreated   = "created"
	stateUpdated   = "updated"
	stateUnchanged = "unchanged"
)

// fileState reports what writing data to path would do. An unchanged file is
// left alone rather than rewritten, which is what makes a re-run quiet.
func fileState(path string, data []byte) string {
	existing, err := os.ReadFile(path)
	if err != nil {
		return stateCreated
	}
	if bytes.Equal(existing, data) {
		return stateUnchanged
	}
	return stateUpdated
}

// vscodeUserSettingsPath returns VS Code's own user settings.json for this
// platform. Insiders, VSCodium and Cursor keep theirs elsewhere; they are
// reached with --settings-path.
func vscodeUserSettingsPath(home string) (string, error) {
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "Code", "User", "settings.json"), nil
	case "windows":
		appData := os.Getenv("APPDATA")
		if appData == "" {
			return "", errors.New("%APPDATA% is not set, so VS Code's user settings cannot be located; pass --settings-path")
		}
		return filepath.Join(appData, "Code", "User", "settings.json"), nil
	default:
		return filepath.Join(home, ".config", "Code", "User", "settings.json"), nil
	}
}

// absSettingsPath expands and absolutizes an explicit --settings-path, so the
// messages and the temp file beside it all name one path.
func absSettingsPath(p string) (string, error) {
	expanded, err := config.ExpandTilde(p)
	if err != nil {
		return "", err
	}
	return filepath.Abs(expanded)
}

// globalFileMatches lists the globs the global schema applies to. A config
// relocated with $TH_CONFIG is covered by its own expanded path, since it no
// longer matches the ~/.th glob.
func globalFileMatches() []string {
	matches := []string{globalFileMatch}
	p := os.Getenv(config.EnvVar)
	if p == "" {
		return matches
	}
	if abs, err := absSettingsPath(p); err == nil {
		p = abs
	}
	return append(matches, p)
}

// errNotStrictJSON marks a settings file th must not rewrite: JSONC, which
// VS Code accepts and encoding/json does not, or something that is not JSON
// at all. Keeping a JSONC parser out of go.mod is the point.
var errNotStrictJSON = errors.New("not strict JSON")

// patchSettings upserts our keys into raw and returns the file's new content
// plus a line per change. Foreign keys and foreign json.schemas entries are
// carried through untouched: the file belongs to the user, and only the
// entries th installs are th's to rewrite.
func patchSettings(raw []byte, entries []map[string]any) ([]byte, []string, error) {
	m, err := decodeStrictObject(raw)
	if err != nil {
		return nil, nil, err
	}
	var actions []string

	assoc, ok := objectAt(m, associationsKey)
	if !ok {
		return nil, nil, fmt.Errorf("%q is not a JSON object", associationsKey)
	}
	if assoc[thrcAssociation] != associationValue {
		actions = append(actions, fmt.Sprintf("associate %s files with %s", thrcAssociation, associationValue))
	}
	assoc[thrcAssociation] = associationValue
	m[associationsKey] = assoc

	var list []any
	switch v := m[schemasKey].(type) {
	case nil:
	case []any:
		list = v
	default:
		return nil, nil, fmt.Errorf("%q is not a JSON array", schemasKey)
	}
	for _, entry := range entries {
		url, _ := entry["url"].(string)
		switch i := findSchemaEntry(list, url); {
		case i < 0:
			list = append(list, entry)
			actions = append(actions, fmt.Sprintf("add the %s entry for %s", schemasKey, url))
		case !jsonEqual(list[i], entry):
			// A stale entry — an older th's globs, or a path from another
			// machine that settings sync carried over — is replaced whole.
			actions = append(actions, fmt.Sprintf("replace the %s entry for %s", schemasKey, url))
			list[i] = entry
		}
	}
	m[schemasKey] = list

	out, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return nil, nil, err
	}
	return append(out, '\n'), actions, nil
}

// decodeStrictObject decodes a settings file, treating anything encoding/json
// will not accept — and any trailing content after the object — as JSONC.
// Numbers survive as json.Number so foreign values round-trip byte-faithfully,
// as in the config migration engine. An absent or blank file reads as an empty
// object: there is nothing to preserve and everything to create.
func decodeStrictObject(raw []byte) (map[string]any, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return map[string]any{}, nil
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var doc any
	if err := dec.Decode(&doc); err != nil {
		return nil, errNotStrictJSON
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, errNotStrictJSON
	}
	switch v := doc.(type) {
	case nil:
		return map[string]any{}, nil
	case map[string]any:
		return v, nil
	default:
		return nil, errors.New("holds a JSON value that is not an object")
	}
}

// objectAt returns the object under key, an empty one when the key is absent,
// and false when something else is sitting there.
func objectAt(m map[string]any, key string) (map[string]any, bool) {
	switch v := m[key].(type) {
	case nil:
		return map[string]any{}, true
	case map[string]any:
		return v, true
	default:
		return nil, false
	}
}

// findSchemaEntry returns the index of the json.schemas entry that names the
// same schema file as url, or -1. The match is by file name under a .th
// directory as well as by exact path: settings sync carries an absolute url
// to a machine whose home is somewhere else, and that entry is ours to fix
// rather than to duplicate.
func findSchemaEntry(list []any, url string) int {
	suffix := "/" + schemaHomeDirName + "/" + filepath.Base(url)
	for i, e := range list {
		obj, ok := e.(map[string]any)
		if !ok {
			continue
		}
		u, ok := obj["url"].(string)
		if !ok {
			continue
		}
		if u == url || strings.HasSuffix(filepath.ToSlash(u), suffix) {
			return i
		}
	}
	return -1
}

// jsonEqual compares two decoded values by their encoding, which is stable:
// map keys marshal in sorted order.
func jsonEqual(a, b any) bool {
	ab, err := json.Marshal(a)
	if err != nil {
		return false
	}
	bb, err := json.Marshal(b)
	if err != nil {
		return false
	}
	return bytes.Equal(ab, bb)
}

// refuseJSONC reports a settings file th will not rewrite, with the exact
// snippet to paste. Absolute paths, not the ~ abbreviation: VS Code does not
// expand ~ in a schema url. A hard error, not a warning — the wiring is the
// whole job, and exiting 0 would fake success.
func refuseJSONC(path string, entries []map[string]any) error {
	snippet, err := json.MarshalIndent(map[string]any{
		associationsKey: map[string]any{thrcAssociation: associationValue},
		schemasKey:      entries,
	}, "", "  ")
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "%s is not strict JSON (comments or trailing commas), so th will not rewrite it.\n", displayPath(path))
	fmt.Fprintln(os.Stderr, "The schemas are installed; merge these keys into it by hand:")
	fmt.Fprintln(os.Stderr, string(snippet))
	return fmt.Errorf("%s was left untouched; paste the keys above into it", displayPath(path))
}

// backupSettings copies the settings file beside itself before the rewrite,
// named like the config migration backups so a timestamp never overwrites an
// earlier copy.
func backupSettings(path string, raw []byte) (string, error) {
	backup := fmt.Sprintf("%s.%s.bak", path, time.Now().Format(settingsBackupTimeFormat))
	if err := os.WriteFile(backup, raw, settingsMode(path)); err != nil {
		return "", err
	}
	return backup, nil
}

// settingsMode reports the file's permission bits, defaulting to 0644 for a
// file th is about to create.
func settingsMode(path string) os.FileMode {
	fi, err := os.Stat(path)
	if err != nil {
		return 0o644
	}
	return fi.Mode().Perm()
}

// atomicWriteFile replaces path via a temp file in the same directory plus a
// rename, so an editor watching the file never reads a half-written one.
func atomicWriteFile(path string, data []byte, mode os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+"-*")
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

func init() {
	schemaInstallCmd.Flags().BoolVar(&schemaInstallDryRun, "dry-run", false, "report what would be written and changed, without writing anything")
	schemaInstallCmd.Flags().StringVar(&schemaInstallSettingsPath, "settings-path", "", "settings.json to patch (default: VS Code's user settings for this platform)")
	schemaCmd.AddCommand(schemaInstallCmd)
}
