package cmd

import (
	"slices"
	"sort"
	"testing"

	"github.com/AirConditionedSoftware/treehouse/internal/config"
)

// effectiveLeaves flattens the --json settings object to the dotted names
// the table and the sources object use, so a nested "vscode" key reads as
// "vscode.open" and friends.
func effectiveLeaves(m map[string]any, prefix string) []string {
	var names []string
	for k, v := range m {
		name := k
		if prefix != "" {
			name = prefix + "." + k
		}
		if nested, ok := v.(map[string]any); ok {
			names = append(names, effectiveLeaves(nested, name)...)
			continue
		}
		names = append(names, name)
	}
	return names
}

// TestEffectiveViewsAgree is the drift guard for th config --effective: the
// table, the JSON settings object and the JSON sources object all have to
// report exactly the settings in effectiveFieldNames. Adding a setting
// without touching all three fails here.
func TestEffectiveViewsAgree(t *testing.T) {
	var (
		res  config.Resolved
		prov config.Provenance
	)
	want := slices.Clone(effectiveFieldNames)
	sort.Strings(want)

	check := func(what string, got []string) {
		t.Helper()
		sort.Strings(got)
		if !slices.Equal(got, want) {
			t.Errorf("%s = %v; want %v", what, got, want)
		}
	}

	check("settings leaf keys", effectiveLeaves(effectiveSettings(res), ""))

	sources := effectiveSources(prov)
	names := make([]string, 0, len(sources))
	for name := range sources {
		names = append(names, name)
	}
	check("sources keys", names)

	display := effectiveDisplay(res)
	names = names[:0]
	for name := range display {
		names = append(names, name)
	}
	check("table values", names)

	// The table renders the field list in order, with the repo name row —
	// which is repo identity, not a setting — in front of it.
	rows := effectiveRows(res, prov, "myapp")
	if len(rows) != len(effectiveFieldNames)+1 || rows[0].name != "name" || rows[0].value != "myapp" {
		t.Fatalf("effectiveRows = %v; want a leading name row plus one row per setting", rows)
	}
	for i, name := range effectiveFieldNames {
		if rows[i+1].name != name {
			t.Errorf("row %d = %q; want %q (table order must follow effectiveFieldNames)", i+1, rows[i+1].name, name)
		}
	}
	if got := effectiveRows(res, prov, ""); len(got) != len(effectiveFieldNames) {
		t.Errorf("effectiveRows outside a repo returned %d rows; want %d with no name row", len(got), len(effectiveFieldNames))
	}
}
