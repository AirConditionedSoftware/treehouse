package config

// migrateLocalV2 nests a .thrc's flat VS Code keys into one "vscode" object,
// the same move schema v2 makes in the global config. A .thrc is settings for
// the repo it lives in, so its top level is the only place they can appear —
// "repos" is a field the strict decoder rejects. The nesting helper is shared
// with the global step (migrate_global_v2.go).
func migrateLocalV2(m map[string]any) (map[string]any, error) {
	if err := nestVSCodeV2(m); err != nil {
		return nil, err
	}
	return m, nil
}
