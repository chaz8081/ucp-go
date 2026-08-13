package preprocess

import (
	"errors"
	"fmt"
)

// MergeAllOf collapses node's allOf branches into node itself. Local
// $ref branches are resolved against root and deep-copied (matching
// python-sdk's copy.deepcopy at the ref site, preprocess_schemas.py:98)
// before merging, so mutating the merged result never bleeds back into
// root's $defs. A ref that resolves to an empty object is treated the
// same as unresolved — python's `if resolved:` check is falsy on `{}`
// (preprocess_schemas.py:97). A ref that resolves to a non-object value
// (ErrRefNotObject) is dropped entirely: python deep-copies it, finds
// it isn't a dict, and returns without contributing it anywhere
// (preprocess_schemas.py:104-105). Every other unresolved ref — external
// file refs and broken local pointers alike (ErrRefNotFound) — is not
// an error: python-sdk defers these to a later cross-file pass
// (preprocess_schemas.py:100-102), so they accumulate here and are
// re-emitted as a slim node["allOf"] containing only the unresolved
// branches (preprocess_schemas.py:151-153). A resolved branch's own
// leftover slim allOf (its own unresolved refs, produced by the
// recursive call below) is scoped to that branch and reserved — it must
// never carry onto the parent node, matching python's reserved-key set
// which excludes allOf from the copy-down (preprocess_schemas.py:123-130).
// Properties use a last-write-wins policy: among the allOf branches a
// later branch's property replaces an earlier branch's property of the
// same name, and the branches' merged properties then override the
// node's own properties of the same name — matching python-sdk's
// dict.update-based merge. anyOf/oneOf found on a branch are extracted
// off it and appended onto node's own anyOf/oneOf list rather than
// merged as scalars (preprocess_schemas.py:107-112). Scalar keywords
// (anything outside properties/required/allOf/$ref/anyOf/oneOf) keep
// the node's own value whenever the node already sets one; only unset
// keys are filled in from the branches — this includes title/description,
// which carry from a branch onto the node when the node doesn't already
// have them (python-sdk's reserved-key set excludes only
// properties/required/allOf/$ref/anyOf/oneOf, preprocess_schemas.py:123-130).
// Among the branches themselves, scalar keywords are first-branch-wins
// — the inverse of properties' last-branch-wins. required preserves the
// node's own list verbatim (python never rewrites it, only appends new
// entries, preprocess_schemas.py:141-145): each branch's required
// entries are appended, in branch order, skipping any string already
// present in the node's own list or already appended from an earlier
// branch. When no branch contributes a new entry, node["required"] is
// left completely untouched.
// A branch carrying if/then/else is a rule rather than a set of fields, so
// it is left in the residual allOf instead of being folded in: merging
// several would collapse mutually exclusive rules into one and keep only
// the last (python-sdk 816bbab, preprocess_schemas.py:107-110).
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
	var branchReq []any
	seenReq := map[string]bool{}
	merged := map[string]any{}
	var remainingRefs []any
	polyBranches := map[string][]any{}

	// Seed the de-dupe set from the node's own required list without
	// touching or validating it: python never rewrites the node's own
	// list, it only appends branch-contributed entries that aren't
	// already present (preprocess_schemas.py:141-145).
	if nodeReq, ok := node["required"].([]any); ok {
		for _, r := range nodeReq {
			if s, ok := r.(string); ok {
				seenReq[s] = true
			}
		}
	}

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
			branchReq = append(branchReq, s)
		}
		return nil
	}

	for _, rb := range rawBranches {
		branch, ok := rb.(map[string]any)
		if !ok {
			return fmt.Errorf("allOf branch is not an object: %v", rb)
		}
		// A conditional branch is preserved whole rather than merged.
		// Folding several of them into one node collapses mutually
		// exclusive rules into a single if/then and keeps only the last:
		// total.json declares that discount amounts are negative and that
		// subtotal, fulfillment, tax and fee amounts are not, and merging
		// silently drops one of the two
		// (python-sdk 816bbab, preprocess_schemas.py:107-110).
		if HasConditional(branch) {
			remainingRefs = append(remainingRefs, branch)
			continue
		}
		if ref, ok := branch["$ref"].(string); ok {
			resolved, err := ResolveLocalRef(ref, root)
			switch {
			case errors.Is(err, ErrRefNotObject):
				// Resolved, but not an object: python deep-copies it,
				// finds it isn't a dict, and drops it outright rather
				// than treating it as unresolved
				// (preprocess_schemas.py:104-105).
				continue
			case err != nil:
				// Any other unresolved ref (external file, broken local
				// pointer) is not an error: the python-sdk preprocessor
				// defers it to a later cross-file pass and re-emits it
				// as a slim allOf (preprocess_schemas.py:100-102,
				// 151-153).
				remainingRefs = append(remainingRefs, branch)
				continue
			case len(resolved) == 0:
				// python's `if resolved:` check treats an empty object
				// as falsy — an empty $defs entry is unresolved, not
				// merged (preprocess_schemas.py:97).
				remainingRefs = append(remainingRefs, branch)
				continue
			}
			// Deep-copy the resolved node (python-sdk deep-copies at
			// the ref site, preprocess_schemas.py:98) so the recursive
			// MergeAllOf call below and the merge that follows never
			// mutate root's $defs tree in place.
			branch = CopyTree(resolved).(map[string]any)
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
			case "anyOf", "oneOf":
				items, ok := v.([]any)
				if !ok {
					return fmt.Errorf("allOf branch %s is not an array: %T", k, v)
				}
				polyBranches[k] = append(polyBranches[k], items...)
			case "$ref":
				// handled above
			case "allOf":
				// A resolved branch may itself carry a leftover slim
				// allOf — its own unresolved refs, set by the recursive
				// MergeAllOf call above. That's scoped to the branch,
				// not the parent node: python's reserved-key set
				// already excludes allOf from the copy-down
				// (preprocess_schemas.py:123-130), so it must not leak
				// onto node.
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
	if len(branchReq) > 0 {
		// Node's own required list, if any, is carried forward exactly
		// as written — no dedupe, no stripping of empty strings — and
		// only new branch-contributed entries are appended
		// (preprocess_schemas.py:141-145).
		nodeReq, _ := node["required"].([]any)
		out := make([]any, 0, len(nodeReq)+len(branchReq))
		out = append(out, nodeReq...)
		out = append(out, branchReq...)
		node["required"] = out
	}
	for k, branches := range polyBranches {
		if existing, ok := node[k].([]any); ok {
			node[k] = append(existing, branches...)
		} else {
			node[k] = branches
		}
	}
	if len(remainingRefs) > 0 {
		node["allOf"] = remainingRefs
	}
	return nil
}

// HasConditional reports whether a node carries conditional keywords, which
// make it a rule rather than a set of fields to fold in.
func HasConditional(node map[string]any) bool {
	for _, k := range []string{"if", "then", "else"} {
		if _, ok := node[k]; ok {
			return true
		}
	}
	return false
}
