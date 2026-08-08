package preprocess

import "fmt"

// MergeAllOf collapses node's allOf branches into node itself. Local
// $ref branches are resolved against root first (see the aliasing note
// below). Properties use a last-write-wins policy: among the allOf
// branches a later branch's property replaces an earlier branch's
// property of the same name, and the branches' merged properties then
// override the node's own properties of the same name — matching
// python-sdk's dict.update-based merge. Scalar keywords (anything
// outside properties/required) keep the node's own value whenever the
// node already sets one; only unset keys are filled in from the
// branches. Among the branches themselves, scalar keywords are
// first-branch-wins — the inverse of properties' last-branch-wins.
// required is a node-first, order-preserving union: the
// node's own required entries are seeded first, then each branch's
// entries are appended in branch order, skipping duplicates.
func MergeAllOf(node, root map[string]any) error {
	rawAllOf, has := node["allOf"]
	if !has {
		return nil
	}
	rawBranches, ok := rawAllOf.([]any)
	if !ok {
		return fmt.Errorf("allOf is not an array: %T", rawAllOf)
	}

	mergedProps := map[string]any{}
	var mergedReq []any
	seenReq := map[string]bool{}
	merged := map[string]any{}

	addRequired := func(list any) error {
		items, ok := list.([]any)
		if !ok {
			// Absent, or not a list: nothing to add. The spec overloads
			// "required" — shopping/fulfillment.json's embedded OpenRPC
			// parameter descriptors under /embedded/methods/*/params use
			// "required": true (a boolean, OpenRPC semantics, not the
			// JSON Schema array keyword). Erroring here would break
			// generation once the Phase 2 document walk visits those
			// nodes, so non-array required stays a silent no-op; only
			// individual array entries are checked below.
			return nil
		}
		for _, r := range items {
			s, ok := r.(string)
			if !ok {
				return fmt.Errorf("required entry is not a string: %v (%T)", r, r)
			}
			if s == "" || seenReq[s] {
				continue
			}
			seenReq[s] = true
			mergedReq = append(mergedReq, s)
		}
		return nil
	}

	// Seed with the node's own required list first so its entries sort
	// ahead of anything contributed by the branches below.
	if err := addRequired(node["required"]); err != nil {
		return err
	}

	for _, rb := range rawBranches {
		branch, ok := rb.(map[string]any)
		if !ok {
			return fmt.Errorf("allOf branch is not an object: %v", rb)
		}
		if ref, ok := branch["$ref"].(string); ok {
			resolved, err := ResolveLocalRef(ref, root)
			if err != nil {
				return err
			}
			// resolved aliases the live node inside root (see
			// ResolveLocalRef's doc comment); the recursive MergeAllOf
			// call below mutates it in place, the same way the
			// python-sdk preprocessor flattens $defs entries in place.
			// Python instead deep-copies at the ref site, but since
			// both approaches produce the same flattened JSON this
			// only matters if a later pass mutates a property map that
			// is reachable from more than one referring node.
			branch = resolved
		}
		// Branches may themselves contain allOf (e.g. entity bases): recurse.
		if err := MergeAllOf(branch, root); err != nil {
			return err
		}
		for k, v := range branch {
			switch k {
			case "properties":
				propsMap, ok := v.(map[string]any)
				if !ok {
					return fmt.Errorf("allOf branch properties is not an object: %T", v)
				}
				for pk, pv := range propsMap {
					// Last branch wins: unconditional overwrite.
					mergedProps[pk] = pv
				}
			case "required":
				if err := addRequired(v); err != nil {
					return err
				}
			case "$ref", "title", "description":
				// refs handled above; branch titles/descriptions never
				// override the node's own documentation
			default:
				if _, exists := merged[k]; !exists {
					merged[k] = v
				}
			}
		}
	}

	delete(node, "allOf")
	// Node's own keys win over everything merged from branches.
	for k, v := range merged {
		if _, exists := node[k]; !exists {
			node[k] = v
		}
	}
	rawNodeProps, hasNodeProps := node["properties"]
	if hasNodeProps {
		nodeProps, ok := rawNodeProps.(map[string]any)
		if !ok {
			return fmt.Errorf("node properties is not an object: %T", rawNodeProps)
		}
		// Merged branch properties override the node's own, matching
		// python-sdk's node.setdefault("properties", {}).update(...).
		for pk, pv := range mergedProps {
			nodeProps[pk] = pv
		}
	} else if len(mergedProps) > 0 {
		node["properties"] = mergedProps
	}
	if len(mergedReq) > 0 {
		node["required"] = mergedReq
	}
	return nil
}
