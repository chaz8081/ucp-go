package emit

import (
	"fmt"
	"path"
	"strings"
)

const defsFragmentPrefix = "/$defs/"

// ResolveRef maps a $ref appearing in schema `from` to the Go type it
// denotes. Three forms occur in the normalized spec: a bare cross-file path
// ("types/line_item.json"), a cross-file path with a $defs fragment
// ("../capability.json#/$defs/base"), and a purely local fragment
// ("#/$defs/entity").
//
// An unresolvable ref is an error rather than a fallback to `any`: a model
// that silently accepts anything is worse than a generator that stops.
func ResolveRef(idx *TypeIndex, from, ref string) (TypeRef, error) {
	filePart, fragment, _ := strings.Cut(ref, "#")

	target := from
	if filePart != "" {
		target = path.Join(path.Dir(from), filePart)
	}

	def := ""
	if fragment != "" {
		rest, ok := strings.CutPrefix(fragment, defsFragmentPrefix)
		if !ok {
			return TypeRef{}, fmt.Errorf("unsupported ref fragment in %q (only %s… is supported)", ref, defsFragmentPrefix)
		}
		// A deeper pointer (e.g. #/$defs/x/properties/y) addresses an
		// anonymous subschema, which has no registered type.
		if name, tail, hasTail := strings.Cut(rest, "/"); hasTail {
			return TypeRef{}, fmt.Errorf("ref %q points inside a $def (%s → %s); no type is emitted for that location", ref, name, tail)
		}
		def = rest
	}

	got, ok := idx.Lookup(target, def)
	if !ok {
		where := target
		if def != "" {
			where += "#/$defs/" + def
		}
		return TypeRef{}, fmt.Errorf("ref %q resolves to %s, which emits no type", ref, where)
	}
	return got, nil
}
