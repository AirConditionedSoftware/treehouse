package config

// localMigrations are the forward steps for the repo-local .thrc, in order:
// entry i migrates version i+1 to i+2, so the current version is
// len(localMigrations)+1. This track is independent of globalMigrations and
// the two are expected to drift apart.
//
// To add a step: create migrate_local_vN.go containing
//
//	func migrateLocalVN(m map[string]any) (map[string]any, error)
//
// append {from: N - 1, apply: migrateLocalVN} to this slice, and add a
// fixture test for it. Never edit or reorder a shipped step — migrations are
// forward-only history, and someone's file is still on the old version.
var localMigrations = []migration{
	{from: 1, apply: migrateLocalV2},
}
