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
	// properties present but not an object is a malformed shape; this
	// function has no error return, so it deliberately no-ops here
	// rather than panicking — MergeAllOf's branch/property checks catch
	// malformed properties upstream, before the document walk reaches
	// this stage.
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

			if _, has := nb["type"]; !has && baseType != nil {
				// Deep-copy: baseType may be an array-valued "type"
				// ([]any), and assigning the same slice to every branch
				// would let one branch's later mutation bleed into its
				// siblings and into the node's own base type.
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
// runs as a whole-document pre-pass over every node BEFORE the merge walk:
// interleaving it per-node (as python does at preprocess_schemas.py:263-265)
// is order-sensitive when a $defs entry holding an unresolved external ref
// is $ref'd by a sibling — the pre-pass removes the only such case (the
// entity ref) up front, making the reversed walk order-independent
// (verified 78/78 against the python output on the real spec).
// entityDef may be nil for schemas that never reference the entity base.
func PreprocessDocument(schema map[string]any, entityDef map[string]any) error {
	nodes := IterNodes(schema)
	if entityDef != nil {
		for _, n := range nodes {
			if node, ok := n.(map[string]any); ok {
				if err := flattenEntityRef(node, entityDef); err != nil {
					return err
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
