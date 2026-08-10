package preprocess

import "sort"

// IterNodes returns every map and slice node in the tree in stack-based
// DFS discovery order, matching the python-sdk's iter_nodes
// (preprocess_schemas.py:38-60). Callers needing approximate bottom-up
// order iterate the result in reverse, as the document preprocessor does.
// Sibling order is nondeterministic (map iteration); every transform
// applied via this walker must be commutative across siblings — serialized
// determinism is guaranteed separately by CanonicalJSON.
func IterNodes(root any) []any {
	var out []any
	stack := []any{root}
	for len(stack) > 0 {
		curr := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		out = append(out, curr)
		switch t := curr.(type) {
		case map[string]any:
			for _, child := range t {
				switch child.(type) {
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
