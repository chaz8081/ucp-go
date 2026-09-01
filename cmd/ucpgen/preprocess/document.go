package preprocess

import (
	"fmt"
	"sort"
	"strings"
)

// DistributeToBranches pushes a node's base properties, required list, and
// type down into each anyOf/oneOf branch so every union branch is a
// self-contained schema (preprocess_schemas.py:181-223). Branch properties
// override base properties. The merged required list is the set union;
// Python builds it via list(set(...)) which is hash-order nondeterministic —
// Go sorts it, and the golden comparator sorts required arrays on both
// sides before comparing.
func DistributeToBranches(node map[string]any) {
	// properties present but not an object is a malformed shape, and it
	// is NOT guaranteed to be caught upstream: MergeAllOf only validates
	// properties on nodes that carry an allOf key, so a node with
	// anyOf/oneOf but no allOf and malformed properties reaches this
	// function completely unvalidated. This function has no error
	// return by design, so it silently no-ops on that shape rather than
	// propagating a failure — python's equivalent (dict.update()/.items()
	// against a non-dict) raises AttributeError in the same case. This
	// combination (malformed properties + anyOf/oneOf, no allOf) does
	// not occur anywhere in the real UCP spec.
	baseProps, ok := node["properties"].(map[string]any)
	if !ok {
		return
	}
	baseReq, _ := node["required"].([]any)
	baseType := node["type"]

	for _, polyKey := range []string{"anyOf", "oneOf"} {
		branches, ok := node[polyKey].([]any)
		if !ok {
			continue
		}
		updated := make([]any, 0, len(branches))
		for _, rb := range branches {
			branch, ok := rb.(map[string]any)
			if !ok {
				updated = append(updated, rb)
				continue
			}
			nb := CopyTree(branch).(map[string]any)
			combined := CopyTree(baseProps).(map[string]any)
			if bp, ok := nb["properties"].(map[string]any); ok {
				for k, v := range bp {
					combined[k] = v
				}
			}
			nb["properties"] = combined

			seen := map[string]bool{}
			var req []string
			for _, lists := range [][]any{baseReq, asAnySlice(nb["required"])} {
				for _, r := range lists {
					// Non-string entries are silently dropped by the
					// r.(string) failing the ok check below — a
					// divergence from python, whose set()-based union
					// preserves any hashable value regardless of type.
					// See allof.go's addRequired for the related (but
					// distinct) case of "required" itself being a bool
					// rather than an array, from the spec's embedded
					// OpenRPC descriptors.
					if s, ok := r.(string); ok && !seen[s] {
						seen[s] = true
						req = append(req, s)
					}
				}
			}
			sort.Strings(req)
			reqAny := make([]any, len(req))
			for i, s := range req {
				reqAny[i] = s
			}
			nb["required"] = reqAny

			if _, has := nb["type"]; !has && baseType != nil && baseType != "" {
				// Deep-copy: baseType may be an array-valued "type"
				// ([]any), and assigning the same slice to every branch
				// would let one branch's later mutation bleed into its
				// siblings and into the node's own base type.
				// baseType == "" is excluded deliberately: python
				// inherits base type by truthiness (`if base_type:`),
				// and an empty string is falsy there just like an unset
				// key — inheriting it here would diverge from python.
				nb["type"] = CopyTree(baseType)
			}
			updated = append(updated, nb)
		}
		node[polyKey] = updated
	}
}

func asAnySlice(v any) []any {
	s, _ := v.([]any)
	return s
}

// flattenEntityRef replaces allOf items referencing ucp.json#/$defs/entity
// with an inlined deep copy of the entity definition, title/description
// stripped (preprocess_schemas.py:226-251).
func flattenEntityRef(node map[string]any, entityDef map[string]any) error {
	items, ok := node["allOf"].([]any)
	if !ok {
		return nil
	}
	out := make([]any, 0, len(items))
	for _, it := range items {
		m, ok := it.(map[string]any)
		ref, _ := m["$ref"].(string)
		if ok && strings.HasSuffix(ref, "ucp.json#/$defs/entity") {
			if len(entityDef) == 0 {
				return fmt.Errorf("node requires ucp.json#/$defs/entity but no entity definition was provided")
			}
			e := CopyTree(entityDef).(map[string]any)
			delete(e, "title")
			delete(e, "description")
			out = append(out, e)
			continue
		}
		out = append(out, it)
	}
	node["allOf"] = out
	return nil
}

// PreprocessDocument normalizes one schema file in place. Entity flattening
// runs as a whole-document pre-pass over every node BEFORE the merge walk,
// rather than interleaved per-node as python does
// (preprocess_schemas.py:263-265). The mechanism this protects: of every
// external allOf $ref in the real spec (25 of them), only a
// ucp.json#/$defs/entity ref is ever converted into real inline content —
// flattenEntityRef substitutes the ref for a deep copy of the entity def's
// own properties/required/etc. Every other external ref is dropped
// identically into a slim, unresolved allOf regardless of visit order, so
// order never matters for those. It does matter for entity refs: 3 of the
// spec's $defs entries (capability, payment_handler, and service .json's
// "base" defs) carry an entity ref AND are themselves $ref'd by a sibling
// def's allOf — of 6 $defs entries total that carry some external allOf
// ref and are locally $ref'd this way, these 3 are the entity-ref half;
// the other 3 carry non-entity external refs, which are the
// order-insensitive case above. For the entity-ref 3, when MergeAllOf
// follows the local $ref and recursively merges the copied target, the
// target's entity ref must already be real content, or the entity's
// properties/required never make it into the referencing node at all.
// A whole-document pre-pass guarantees every
// entity ref is inlined before any merge starts, so this dependency can
// never become visit-order-sensitive (verified 78/78 against the python
// output on the real spec).
// entityDef may be nil, or empty, for schemas that never reference the
// entity base — python's `if entity_def:` treats an empty dict the same
// as None, so len(entityDef) == 0 no-ops the pre-pass either way.
func PreprocessDocument(schema map[string]any, entityDef map[string]any) error {
	// python-sdk 3e1aace drops $id here, with the reason given at the call
	// site upstream: "Remove $id so datamodel-code-generator resolves
	// relative $refs strictly via the local filesystem rather than
	// attempting remote HTTP fetching." The spec's schemas identify
	// themselves by https://ucp.dev URLs, and a generator that honours the
	// $id resolves their sibling refs against that base — i.e. over the
	// network — instead of against the directory it was handed.
	//
	// Ordering matters and mirrors python: this runs before variants are
	// generated, so a variant is copied from a document that no longer has
	// an $id. applyVariantIdentity's $id rewrite is therefore unreachable on
	// this corpus. It is kept because upstream kept theirs, and parity is
	// defined by output, not by which branches happen to execute.
	delete(schema, "$id")

	nodes := IterNodes(schema)
	if len(entityDef) > 0 {
		for _, n := range nodes {
			if node, ok := n.(map[string]any); ok {
				if err := flattenEntityRef(node, entityDef); err != nil {
					return fmt.Errorf("entity pre-pass: %w", err)
				}
			}
		}
	}
	for i := len(nodes) - 1; i >= 0; i-- {
		node, ok := nodes[i].(map[string]any)
		if !ok {
			continue
		}
		if err := MergeAllOf(node, schema); err != nil {
			return fmt.Errorf("document walk: %w", err)
		}
		DistributeToBranches(node)
	}
	return nil
}
