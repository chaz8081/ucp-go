package preprocess

// CopyTree deep-copies a JSON tree (maps, slices, scalars). The python-sdk
// preprocessor deep-copies at every ref resolution and variant creation
// (preprocess_schemas.py:98, :497); this is the Go equivalent.
func CopyTree(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			out[k] = CopyTree(val)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, val := range t {
			out[i] = CopyTree(val)
		}
		return out
	default:
		return v
	}
}
