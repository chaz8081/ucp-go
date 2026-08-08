package preprocess

import "fmt"

// MergeAllOf collapses node's allOf branches into node itself.
// Local $ref branches are resolved against root first. Properties and
// required lists are unioned; for scalar keywords the node's own value
// wins over branch values, matching the python-sdk preprocessor.
func MergeAllOf(node, root map[string]any) error {
	rawBranches, ok := node["allOf"].([]any)
	if !ok {
		return nil
	}
	mergedProps := map[string]any{}
	var mergedReq []any
	seenReq := map[string]bool{}
	merged := map[string]any{}

	addRequired := func(list any) {
		items, _ := list.([]any)
		for _, r := range items {
			s, _ := r.(string)
			if s != "" && !seenReq[s] {
				seenReq[s] = true
				mergedReq = append(mergedReq, s)
			}
		}
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
			branch = resolved
		}
		// Branches may themselves contain allOf (e.g. entity bases): recurse.
		if err := MergeAllOf(branch, root); err != nil {
			return err
		}
		for k, v := range branch {
			switch k {
			case "properties":
				for pk, pv := range v.(map[string]any) {
					if _, exists := mergedProps[pk]; !exists {
						mergedProps[pk] = pv
					}
				}
			case "required":
				addRequired(v)
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
	if nodeProps, ok := node["properties"].(map[string]any); ok {
		for pk, pv := range mergedProps {
			if _, exists := nodeProps[pk]; !exists {
				nodeProps[pk] = pv
			}
		}
	} else if len(mergedProps) > 0 {
		node["properties"] = mergedProps
	}
	addRequired(node["required"])
	if len(mergedReq) > 0 {
		node["required"] = mergedReq
	}
	return nil
}
