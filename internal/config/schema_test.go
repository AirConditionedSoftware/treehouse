package config

import (
	"encoding/json"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// schemaTrack pairs an embedded schema with the struct it describes and the
// schema version that track's migration registry is on.
type schemaTrack struct {
	name    string
	raw     []byte
	typ     reflect.Type
	version int
}

func schemaTracks() []schemaTrack {
	return []schemaTrack{
		{name: "thrc.schema.json", raw: ThrcSchema(), typ: reflect.TypeOf(LocalConfig{}), version: CurrentLocalVersion()},
		{name: "config.schema.json", raw: GlobalSchema(), typ: reflect.TypeOf(File{}), version: CurrentGlobalVersion()},
	}
}

func decodeSchema(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("schema is not valid JSON: %v", err)
	}
	return doc
}

// TestSchemasMatchStructs is the drift guard on the shipped schemas: every
// JSON name the structs decode has a schema property and every schema
// property has a field, recursively through the nested objects and the arrays
// of objects. Adding a Settings field without describing it in both schemas
// fails here.
func TestSchemasMatchStructs(t *testing.T) {
	for _, tr := range schemaTracks() {
		t.Run(tr.name, func(t *testing.T) {
			doc := decodeSchema(t, tr.raw)
			assertPropertiesMatch(t, doc, tr.name, doc, tr.typ)
		})
	}
}

func assertPropertiesMatch(t *testing.T, doc map[string]any, where string, node map[string]any, typ reflect.Type) {
	t.Helper()
	props, ok := node["properties"].(map[string]any)
	if !ok {
		t.Fatalf("%s: the schema node describing %s has no \"properties\" object", where, typ)
	}
	fields := jsonFields(t, where, typ)
	for _, name := range slices.Sorted(maps.Keys(fields)) {
		if _, ok := props[name]; !ok {
			t.Errorf("%s: %s field %q has no schema property", where, typ, name)
		}
	}
	for _, name := range slices.Sorted(maps.Keys(props)) {
		if _, ok := fields[name]; !ok {
			t.Errorf("%s: schema property %q has no %s field", where, name, typ)
		}
	}
	for _, name := range slices.Sorted(maps.Keys(fields)) {
		elem, viaItems, nested := nestedStruct(fields[name])
		if !nested {
			continue
		}
		child, ok := props[name].(map[string]any)
		if !ok {
			continue
		}
		child = resolveRef(t, doc, where+"."+name, child)
		if viaItems {
			items, ok := child["items"].(map[string]any)
			if !ok {
				t.Errorf("%s.%s: the schema for a []%s has no \"items\" object", where, name, elem.Name())
				continue
			}
			child = resolveRef(t, doc, where+"."+name+"[]", items)
		}
		assertPropertiesMatch(t, doc, where+"."+name, child, elem)
	}
}

// jsonFields maps the JSON names typ decodes to their Go types, flattening
// embedded structs the way encoding/json promotes their fields.
func jsonFields(t *testing.T, where string, typ reflect.Type) map[string]reflect.Type {
	t.Helper()
	fields := map[string]reflect.Type{}
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		if f.Anonymous && f.Tag.Get("json") == "" && f.Type.Kind() == reflect.Struct {
			maps.Copy(fields, jsonFields(t, where, f.Type))
			continue
		}
		name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		if name == "" || name == "-" {
			t.Fatalf("%s: %s.%s has no json name", where, typ, f.Name)
		}
		fields[name] = f.Type
	}
	return fields
}

// nestedStruct reports whether a field is described by a nested schema
// object: a struct, possibly behind pointers, or a slice of them — in which
// case the object is the array's "items".
func nestedStruct(ft reflect.Type) (elem reflect.Type, viaItems, ok bool) {
	for ft.Kind() == reflect.Pointer {
		ft = ft.Elem()
	}
	if ft.Kind() == reflect.Slice {
		elem = ft.Elem()
		for elem.Kind() == reflect.Pointer {
			elem = elem.Elem()
		}
		return elem, true, elem.Kind() == reflect.Struct
	}
	return ft, false, ft.Kind() == reflect.Struct
}

func resolveRef(t *testing.T, doc map[string]any, where string, node map[string]any) map[string]any {
	t.Helper()
	ref, ok := node["$ref"].(string)
	if !ok {
		return node
	}
	name, found := strings.CutPrefix(ref, "#/definitions/")
	if !found {
		t.Fatalf("%s: unsupported $ref %q; the schemas keep everything under \"definitions\"", where, ref)
	}
	defs, _ := doc["definitions"].(map[string]any)
	target, ok := defs[name].(map[string]any)
	if !ok {
		t.Fatalf("%s: $ref %q resolves to nothing", where, ref)
	}
	return target
}

// TestSchemaVersionMatchesCurrent pins each schema's version const to the
// version its migration registry derives, so appending a migration step goes
// stale here rather than in someone's editor.
func TestSchemaVersionMatchesCurrent(t *testing.T) {
	for _, tr := range schemaTracks() {
		t.Run(tr.name, func(t *testing.T) {
			doc := decodeSchema(t, tr.raw)
			props, ok := doc["properties"].(map[string]any)
			if !ok {
				t.Fatal("schema has no \"properties\" object")
			}
			version, ok := props["version"].(map[string]any)
			if !ok {
				t.Fatal("schema has no \"version\" property")
			}
			got, ok := version["const"].(float64)
			if !ok {
				t.Fatalf("the version property's \"const\" is %v; want a number", version["const"])
			}
			if int(got) != tr.version {
				t.Errorf("version const = %d; want %d", int(got), tr.version)
			}
		})
	}
}

// TestSchemasAreClosedDraft07 checks the two properties the schemas must have
// to describe what th's decoders actually accept: draft-07, which is what VS
// Code's json.schemas association reads, and every object closed to unknown
// properties, as DisallowUnknownFields makes them.
func TestSchemasAreClosedDraft07(t *testing.T) {
	const draft07 = "http://json-schema.org/draft-07/schema#"
	for _, tr := range schemaTracks() {
		t.Run(tr.name, func(t *testing.T) {
			doc := decodeSchema(t, tr.raw)
			if got := doc["$schema"]; got != draft07 {
				t.Errorf("$schema = %v; want %q", got, draft07)
			}
			assertClosedObjects(t, tr.name, doc)
		})
	}
}

// assertClosedObjects walks the whole document — definitions the property
// walk never reaches included — requiring additionalProperties false wherever
// a node lists properties.
func assertClosedObjects(t *testing.T, where string, node any) {
	t.Helper()
	switch n := node.(type) {
	case map[string]any:
		if _, ok := n["properties"]; ok {
			if open, ok := n["additionalProperties"].(bool); !ok || open {
				t.Errorf("%s: object schema is missing \"additionalProperties\": false", where)
			}
		}
		for _, k := range slices.Sorted(maps.Keys(n)) {
			assertClosedObjects(t, where+"."+k, n[k])
		}
	case []any:
		for i, e := range n {
			assertClosedObjects(t, fmt.Sprintf("%s[%d]", where, i), e)
		}
	}
}
