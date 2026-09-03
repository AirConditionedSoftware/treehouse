package config

import _ "embed"

// The two schemas are hand-written rather than generated: their whole value is
// the description on every property, which an editor shows on hover and
// reflection cannot produce. schema_test.go keeps their properties in step
// with the structs below, field for field.

//go:embed thrc.schema.json
var thrcSchema []byte

//go:embed config.schema.json
var globalSchema []byte

// ThrcSchema returns the JSON Schema describing a repository's .thrc, as it
// is written on disk. The bytes back the embedded file; callers must not
// modify them.
func ThrcSchema() []byte { return thrcSchema }

// GlobalSchema returns the JSON Schema describing the global config file,
// under the same no-modification rule as ThrcSchema.
func GlobalSchema() []byte { return globalSchema }
