package config

import "fmt"

// Schema v2 groups the VS Code settings that used to sit flat beside every
// other setting into one "vscode" object, in the global config's top level
// and in each repos entry alike. The local track's step (migrate_local_v2.go)
// shares the nesting helper below.

// vscodeV2Renames maps each flat v1 key to its name inside the v2 "vscode"
// object, in a fixed order so the step is deterministic. workspace_paths kept
// its name — it was VS Code's despite the unprefixed spelling.
var vscodeV2Renames = [][2]string{
	{"vscode_open", "open"},
	{"vscode_workspace_file", "workspace_file"},
	{"vscode_workspace_prefix", "workspace_prefix"},
	{"vscode_window_title", "window_title"},
	{"vscode_window_color", "window_color"},
	{"workspace_paths", "workspace_paths"},
}

// nestVSCodeV2 moves m's flat VS Code keys into m["vscode"], in place. Only
// keys that exist move, and their values move verbatim — a JSON null decodes
// as a no-op into every field of VSCode, so no file that loaded as v1 can
// fail as v2. The object is created only when at least one key moves, so a
// document with none stays byte-identical; a "vscode" object already there is
// merged into, the moved key winning. A "vscode" that is not an object is a
// broken file, not an old one, and fails loudly.
func nestVSCodeV2(m map[string]any) error {
	var moved map[string]any
	for _, r := range vscodeV2Renames {
		v, ok := m[r[0]]
		if !ok {
			continue
		}
		delete(m, r[0])
		if moved == nil {
			moved = map[string]any{}
		}
		moved[r[1]] = v
	}
	if moved == nil {
		return nil
	}
	existing, ok := m["vscode"]
	if !ok {
		m["vscode"] = moved
		return nil
	}
	obj, ok := existing.(map[string]any)
	if !ok {
		return fmt.Errorf("\"vscode\" is %T, want an object", existing)
	}
	for k, v := range moved {
		obj[k] = v
	}
	return nil
}

// migrateGlobalV2 nests the flat VS Code keys of the global config's top
// level and of every repos entry.
func migrateGlobalV2(m map[string]any) (map[string]any, error) {
	if err := nestVSCodeV2(m); err != nil {
		return nil, err
	}
	repos, _ := m["repos"].([]any)
	for i, entry := range repos {
		// A non-object entry (or a "repos" that is not an array) was
		// un-loadable as v1 too; leave it for the strict decode after the
		// chain to report as it always did.
		obj, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		if err := nestVSCodeV2(obj); err != nil {
			return nil, fmt.Errorf("repos[%d]: %w", i, err)
		}
	}
	return m, nil
}
