package emit

import (
	"fmt"
	"path"
	"sort"
	"strings"
)

// PackageForSchema maps a schema's slash-relative path to the Go package
// name and full import path of the package its types belong to. The
// directory basename is the package name, which is what Go tooling expects
// — a package under shopping/types is named "types" and needs no import
// alias. Root-level schemas belong to package "ucp" at the module root.
func PackageForSchema(rel, modulePath string) (pkgName, importPath string) {
	dir := path.Dir(rel)
	if dir == "." || dir == "" {
		return "ucp", modulePath
	}
	return path.Base(dir), modulePath + "/" + dir
}

// SchemaStem returns the file stem used to qualify $def-derived type names:
// "shopping/types/line_item.json" -> "line_item".
func SchemaStem(rel string) string {
	return strings.TrimSuffix(path.Base(rel), ".json")
}

// TypeRef identifies one emitted Go type.
type TypeRef struct {
	Name       string // Go type name, e.g. "LineItem" or "CapabilityBase"
	Package    string // package name, e.g. "types"
	ImportPath string // full import path
}

// TypeIndex maps schema locations to emitted Go types. A location is a
// schema file plus a $def name; an empty def name means the file-level type.
type TypeIndex struct {
	byLocation map[string]TypeRef
}

func indexKey(rel, def string) string { return rel + "#" + def }

// hasFileLevelType reports whether a schema emits a type for the document
// root. A schema whose content lives entirely in $defs does not: its types
// are the $defs themselves (14 such files in the real spec).
func hasFileLevelType(schema map[string]any) bool {
	// Anything the document root itself declares — properties, a type, or a
	// union — makes the root a schema in its own right. ucp.json is the
	// motivating case: its $defs hold the profile schemas while its root is
	// the metadata union over them, so it yields both a file-level type and
	// per-$def types.
	for _, k := range []string{"properties", "type", "oneOf", "anyOf", "$ref"} {
		if _, ok := schema[k]; ok {
			return true
		}
	}
	// A document that only groups definitions has no type of its own.
	if _, ok := schema["$defs"]; ok {
		return false
	}
	return true
}

// schemaKeywords are the keywords whose presence marks a node as an actual
// schema. A $def carrying none of them is a namespace: a grouping object
// whose values are themselves schemas, used by the spec as an extension
// mount point (identity_linking and dev_ucp_shopping_fulfillment are the
// only two in the corpus, each holding business_schema/platform_schema).
// Nothing references a namespace — a ref would have to point inside a
// $def, which addresses no emitted type — so, like the python generator,
// we emit nothing for them.
var schemaKeywords = map[string]bool{
	"type": true, "properties": true, "$ref": true,
	"allOf": true, "anyOf": true, "oneOf": true, "not": true, "if": true,
	"items": true, "enum": true, "const": true,
	"additionalProperties": true, "required": true,
	"pattern": true, "format": true,
}

// isNamespaceDef reports whether a $def is a grouping object rather than a
// schema.
func isNamespaceDef(def map[string]any) bool {
	if len(def) == 0 {
		return false
	}
	for k := range def {
		if schemaKeywords[k] {
			return false
		}
	}
	// Every value must itself look like a schema object.
	for _, v := range def {
		if _, ok := v.(map[string]any); !ok {
			return false
		}
	}
	return true
}

// BuildTypeIndex registers every type the emitter will produce, before any
// rendering, so cross-file references resolve in a single pass.
//
// $def types are qualified with the schema's file stem. Unqualified $def
// names collide 18 times across the real spec — six files define "base",
// eight define "checkout" — because python gets one module per schema file
// while Go shares a package across a directory. The qualified form has no
// collisions anywhere in the corpus.
func BuildTypeIndex(files map[string]map[string]any, modulePath string) (*TypeIndex, error) {
	idx := &TypeIndex{byLocation: map[string]TypeRef{}}
	seen := map[string]string{} // package.Name -> origin, for collision detection

	rels := make([]string, 0, len(files))
	for rel := range files {
		rels = append(rels, rel)
	}
	sort.Strings(rels) // deterministic collision reporting

	register := func(rel, def, name string) error {
		pkg, imp := PackageForSchema(rel, modulePath)
		key := pkg + "." + name
		origin := rel
		if def != "" {
			origin += "#/$defs/" + def
		}
		if prev, dup := seen[key]; dup {
			return fmt.Errorf("type name collision: %s emitted from both %s and %s", key, prev, origin)
		}
		seen[key] = origin
		idx.byLocation[indexKey(rel, def)] = TypeRef{Name: name, Package: pkg, ImportPath: imp}
		return nil
	}

	for _, rel := range rels {
		schema := files[rel]
		if hasFileLevelType(schema) {
			title, _ := schema["title"].(string)
			if title == "" {
				return nil, fmt.Errorf("%s: schema has no title", rel)
			}
			if err := register(rel, "", GoName(title)); err != nil {
				return nil, err
			}
		}
		defs, _ := schema["$defs"].(map[string]any)
		names := make([]string, 0, len(defs))
		for name := range defs {
			names = append(names, name)
		}
		sort.Strings(names)
		stem := GoName(SchemaStem(rel))
		for _, def := range names {
			if d, ok := defs[def].(map[string]any); ok && isNamespaceDef(d) {
				continue
			}
			if err := register(rel, def, stem+GoName(def)); err != nil {
				return nil, err
			}
		}
	}
	return idx, nil
}

// Lookup returns the type emitted for a schema location.
func (t *TypeIndex) Lookup(rel, def string) (TypeRef, bool) {
	ref, ok := t.byLocation[indexKey(rel, def)]
	return ref, ok
}
