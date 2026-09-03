package config

// globalMigrations are the forward steps for the global config file, in
// order: entry i migrates version i+1 to i+2, so the current version is
// len(globalMigrations)+1.
//
// To add a step: create migrate_global_vN.go containing
//
//	func migrateGlobalVN(m map[string]any) (map[string]any, error)
//
// append {from: N - 1, apply: migrateGlobalVN} to this slice, and add a
// fixture test for it. Never edit or reorder a shipped step — migrations are
// forward-only history, and someone's file is still on the old version.
var globalMigrations = []migration{
	{from: 1, apply: migrateGlobalV2},
}
