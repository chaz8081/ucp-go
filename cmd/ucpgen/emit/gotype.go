package emit

import (
	"fmt"
	"sort"
)

// nestedType is an inline object schema promoted to a named Go type.
type nestedType struct {
	name   string
	schema map[string]any
}

// fileEmitter carries the per-file state rendering accumulates: the imports
// the file needs and the inline object types it must also emit.
type fileEmitter struct {
	idx      *TypeIndex
	rel      string            // schema being rendered
	pkg      string            // package it belongs to
	prefix   string            // type-name prefix for nested types
	imports  map[string]string // import path -> package name
	nested   []nestedType
	nestedAt map[string]bool // dedupe by generated name

	// stdlib imports the generated Validate machinery needs.
	usesErrors, usesRegexp, usesSync, usesUtf8 bool

	// unenforced records validation-only keywords seen per property, so the
	// manifest can carry a machine-readable coverage gap alongside the doc
	// comments in the generated source.
	unenforced map[string][]string

	// degradedRefs records $refs typed as raw JSON to keep the package
	// graph acyclic (see goTypeExpr), so the affected fields can say so.
	degradedRefs map[string]string

	// breaks[dstPackage] marks an edge out of this package that must not be
	// a real import; see CycleBreaks.
	breaks map[string]bool
}

func newFileEmitter(idx *TypeIndex, rel, pkg string) *fileEmitter {
	return newFileEmitterWithBreaks(idx, rel, pkg, nil)
}

func newFileEmitterWithBreaks(idx *TypeIndex, rel, pkg string, breaks map[string]bool) *fileEmitter {
	e := &fileEmitter{
		idx: idx, rel: rel, pkg: pkg,
		imports:      map[string]string{},
		nestedAt:     map[string]bool{},
		unenforced:   map[string][]string{},
		degradedRefs: map[string]string{},
		breaks:       breaks,
	}
	// Nested type names hang off the enclosing type, so a file's inline
	// objects are namespaced by whatever type encloses them. Callers
	// rendering a $def override this per type.
	if ref, ok := idx.Lookup(rel, ""); ok {
		e.prefix = ref.Name
	}
	return e
}

// qualify renders a resolved type reference as it must appear in this file,
// recording an import when the target lives in another package.
func (e *fileEmitter) qualify(ref TypeRef) string {
	if ref.Package == e.pkg {
		return ref.Name
	}
	e.imports[ref.ImportPath] = ref.Package
	return ref.Package + "." + ref.Name
}

// sortedImports returns the recorded import paths in a stable order.
func (e *fileEmitter) sortedImports() []string {
	out := make([]string, 0, len(e.imports))
	for p := range e.imports {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

// goTypeExpr renders the Go type for one schema node. fieldName seeds the
// name of any inline object promoted to a named type; it is combined with
// the emitter's current prefix so nested names stay unique within a file.
func (e *fileEmitter) goTypeExpr(node map[string]any, fieldName string) (string, error) {
	if ref, ok := node["$ref"].(string); ok {
		target, err := ResolveRef(e.idx, e.rel, ref)
		if err != nil {
			return "", err
		}
		// Typing this reference would create an import Go forbids, because
		// the two packages reference each other. CycleBreaks decides which
		// edge to cut; the bytes still round-trip losslessly, and the
		// affected field says so in a comment.
		if e.breaks[target.Package] {
			e.degradedRefs[ref] = target.Package + "." + target.Name
			e.imports["encoding/json"] = "json"
			return "json.RawMessage", nil
		}
		return e.qualify(target), nil
	}

	// A node carrying a union but no properties of its own is modeled as raw
	// JSON: the alternatives are genuinely different shapes (the spec's only
	// case is `extends`, a string or an array of strings), and raw bytes
	// round-trip losslessly while claiming no typing we haven't done.
	// Typed variants are phase 4. A node with BOTH a union and properties is
	// handled by the caller, which renders the properties as a struct.
	if hasUnion(node) {
		if _, hasProps := node["properties"].(map[string]any); !hasProps {
			e.imports["encoding/json"] = "json"
			return "json.RawMessage", nil
		}
	}

	// A type-affecting keyword must halt generation wherever it appears —
	// inside items and additionalProperties too, not only on a property.
	if kw := checkTypeAffectingKeywords(node); kw != "" {
		return "", fmt.Errorf("%q: keyword %q changes the schema's shape and is not modeled yet (phase 4)", fieldName, kw)
	}

	switch t := node["type"].(type) {
	case string:
		return e.goTypeForNamed(t, node, fieldName)
	case []any:
		// The spec's only multi-type shape is ["x", "null"], i.e. an optional
		// x. Anything richer is a real union and would need a named type.
		var nonNull []string
		for _, v := range t {
			if s, _ := v.(string); s != "" && s != "null" {
				nonNull = append(nonNull, s)
			}
		}
		if len(nonNull) != 1 {
			return "", fmt.Errorf("%s: field %q has type %v; only [\"x\",\"null\"] multi-type is supported (phase 4)", e.rel, fieldName, t)
		}
		inner, err := e.goTypeForNamed(nonNull[0], node, fieldName)
		if err != nil {
			return "", err
		}
		return "*" + inner, nil
	case nil:
		// JSON Schema implies the type from the keywords present. The
		// merged ucp.json narrowings re-state capabilities/services with
		// only additionalProperties, so short-circuiting to `any` here
		// would type the metadata envelope's three registry fields as
		// `any` when map[string][]T is fully derivable.
		if _, ok := node["properties"]; ok {
			return e.goTypeForNamed("object", node, fieldName)
		}
		if _, ok := node["additionalProperties"].(map[string]any); ok {
			return e.goTypeForNamed("object", node, fieldName)
		}
		if _, ok := node["items"]; ok {
			return e.goTypeForNamed("array", node, fieldName)
		}
		if _, ok := node["const"].(string); ok {
			return "string", nil
		}
		if enum, ok := node["enum"].([]any); ok && len(enum) > 0 {
			if _, isString := enum[0].(string); isString {
				return "string", nil
			}
		}
		return "any", nil
	default:
		return "", fmt.Errorf("field %q has non-string type %T", fieldName, node["type"])
	}
}

func (e *fileEmitter) goTypeForNamed(t string, node map[string]any, fieldName string) (string, error) {
	switch t {
	case "string":
		return "string", nil
	case "integer":
		return "int64", nil
	case "number":
		return "float64", nil
	case "boolean":
		return "bool", nil
	case "array":
		items, ok := node["items"].(map[string]any)
		if !ok {
			return "[]any", nil
		}
		inner, err := e.goTypeExpr(items, fieldName+"Item")
		if err != nil {
			return "", err
		}
		return "[]" + inner, nil
	case "object":
		// additionalProperties as a schema means a map keyed by string.
		//
		// propertyNames is deliberately not consulted here: it constrains the
		// KEYS, which are always Go strings, so it contributes no type. Were
		// it treated as one, ucp.json's key constraints would pull
		// shopping/types into the root package's imports and create the
		// corpus's only import cycle. It remains a phase 4 validation concern.
		if ap, ok := node["additionalProperties"].(map[string]any); ok {
			inner, err := e.goTypeExpr(ap, fieldName+"Value")
			if err != nil {
				return "", err
			}
			return "map[string]" + inner, nil
		}
		if _, ok := node["properties"].(map[string]any); ok {
			name := e.prefix + GoName(fieldName)
			if !e.nestedAt[name] {
				e.nestedAt[name] = true
				e.nested = append(e.nested, nestedType{name: name, schema: node})
			}
			return name, nil
		}
		// An object with neither properties nor a value schema carries only
		// unknown keys.
		return "map[string]any", nil
	default:
		return "", fmt.Errorf("field %q has unsupported type %q", fieldName, t)
	}
}

// hasUnion reports whether a node declares oneOf or anyOf members.
func hasUnion(node map[string]any) bool {
	for _, key := range []string{"oneOf", "anyOf"} {
		if members, ok := node[key].([]any); ok && len(members) > 0 {
			return true
		}
	}
	return false
}

// unionMembers returns the node's union members and which keyword declared
// them, preferring oneOf when both are present.
func unionMembers(node map[string]any) (string, []any) {
	for _, key := range []string{"oneOf", "anyOf"} {
		if members, ok := node[key].([]any); ok && len(members) > 0 {
			return key, members
		}
	}
	return "", nil
}
