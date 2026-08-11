package emit

import (
	"fmt"
	"path"
	"strings"
)

const defsFragmentPrefix = "/$defs/"

// ucpRootSchema is the document that owns the shared entity definition.
const ucpRootSchema = "ucp.json"

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
			return TypeRef{}, fmt.Errorf("%s: unsupported ref fragment in %q (only %s… is supported)", from, ref, defsFragmentPrefix)
		}
		// A deeper pointer (e.g. #/$defs/x/properties/y) addresses an
		// anonymous subschema, which has no registered type.
		if name, tail, hasTail := strings.Cut(rest, "/"); hasTail {
			return TypeRef{}, fmt.Errorf("%s: ref %q points inside a $def (%s → %s); no type is emitted for that location", from, ref, name, tail)
		}
		def = rest
	}

	got, ok := idx.Lookup(target, def)

	// A purely local ref that does not resolve in its own document is an
	// entity-inlining artifact: flattening ucp.json#/$defs/entity into a
	// schema copies the entity's body, including refs it wrote relative to
	// ucp.json, which then dangle in their new home. Across the whole
	// corpus this is exactly `#/$defs/version` in capability.json,
	// payment_handler.json and service.json, all resolvable in ucp.json.
	// The python generator resolves them the same way. The fallback is
	// deliberately narrow: local refs only, and only after the in-document
	// lookup has already failed.
	if !ok && filePart == "" && def != "" && target != ucpRootSchema {
		if fromUCP, okUCP := idx.Lookup(ucpRootSchema, def); okUCP {
			return fromUCP, nil
		}
	}

	if !ok {
		where := target
		if def != "" {
			where += "#/$defs/" + def
		}
		return TypeRef{}, fmt.Errorf("%s: ref %q resolves to %s, which emits no type", from, ref, where)
	}
	return got, nil
}
