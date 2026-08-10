package preprocess

import "sort"

// IterNodes returns every map and slice node in the tree in stack-based
// DFS discovery order, matching the python-sdk's iter_nodes
// (preprocess_schemas.py:38-60). Traversal order is deterministic: a
// map node's children are pushed in sorted key order (list nodes are
// already ordered, so no sorting is needed there), and callers needing
// approximate bottom-up order iterate the result in reverse, as the
// document preprocessor does. Unlike python's iter_nodes, this walker
// keeps no visited set — every tree it walks has already passed through
// CopyTree (see MergeAllOf's ref resolution), so it is a genuine tree,
// not a graph, and cannot contain the shared-reference cycles a visited
// set would guard against.
func IterNodes(root any) []any {
	var out []any
	stack := []any{root}
	for len(stack) > 0 {
		curr := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		out = append(out, curr)
		switch t := curr.(type) {
		case map[string]any:
			keys := make([]string, 0, len(t))
			for k := range t {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				switch child := t[k]; child.(type) {
				case map[string]any, []any:
					stack = append(stack, child)
				}
			}
		case []any:
			for _, child := range t {
				switch child.(type) {
				case map[string]any, []any:
					stack = append(stack, child)
				}
			}
		}
	}
	return out
}

// Paths returns the schema file paths in sorted order for deterministic
// iteration (design §4: byte-identical output requires stable order).
func (s *SchemaSet) Paths() []string {
	out := make([]string, 0, len(s.Files))
	for k := range s.Files {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
