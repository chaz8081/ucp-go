package preprocess

import (
	"encoding/json"
	"sort"
)

// CanonicalJSON serializes a schema tree with sorted object keys (Go's
// encoding/json sorts map keys natively) after normalizing the two places
// where the Go and python preprocessors legitimately disagree on ARRAY
// order:
//
//   - "required" arrays are sorted. Python builds them with list(set(...))
//     during union distribution, which is hash-seed dependent, and its
//     variant filter follows JSON insertion order, which Go loses at
//     unmarshal. Neither ordering is reproducible from the other.
//   - "oneOf" arrays are sorted by $ref, but only when every member is a
//     lone {"$ref": ...} object. Python emits the ucp.json metadata union
//     in $defs insertion order; Go sorts the member names. A oneOf with
//     richer members keeps its order, since there the sequence can carry
//     meaning.
//
// Both normalized classes are order-insensitive under JSON Schema
// semantics, so this is an equivalence-preserving comparison form and not
// a weakening of the golden check. Every other array — enum, allOf,
// examples, prefixItems — keeps its order exactly.
//
// The input tree is never mutated: normalization runs on a deep copy.
func CanonicalJSON(v any) ([]byte, error) {
	return json.MarshalIndent(normalizeOrder(CopyTree(v)), "", "  ")
}

func normalizeOrder(v any) any {
	switch t := v.(type) {
	case map[string]any:
		for k, val := range t {
			t[k] = normalizeOrder(val)
		}
		if req, ok := t["required"].([]any); ok {
			sort.SliceStable(req, func(i, j int) bool {
				a, _ := req[i].(string)
				b, _ := req[j].(string)
				return a < b
			})
		}
		if oneOf, ok := t["oneOf"].([]any); ok && allSingleRefs(oneOf) {
			sort.SliceStable(oneOf, func(i, j int) bool {
				return refOf(oneOf[i]) < refOf(oneOf[j])
			})
		}
		return t
	case []any:
		for i, val := range t {
			t[i] = normalizeOrder(val)
		}
		return t
	default:
		return v
	}
}

func allSingleRefs(items []any) bool {
	for _, it := range items {
		m, ok := it.(map[string]any)
		if !ok || len(m) != 1 {
			return false
		}
		if _, ok := m["$ref"].(string); !ok {
			return false
		}
	}
	return len(items) > 0
}

func refOf(v any) string {
	m, ok := v.(map[string]any)
	if !ok {
		return ""
	}
	s, _ := m["$ref"].(string)
	return s
}
