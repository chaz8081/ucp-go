package preprocess

import "sort"

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
