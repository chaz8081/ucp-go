package preprocess

import (
	"strings"
	"testing"
)

func TestCanonicalJSON(t *testing.T) {
	a := map[string]any{
		"required": []any{"b", "a"},
		"oneOf": []any{
			map[string]any{"$ref": "#/$defs/z"},
			map[string]any{"$ref": "#/$defs/a"},
		},
		"properties": map[string]any{"x": map[string]any{"type": "string"}},
	}
	b := map[string]any{
		"properties": map[string]any{"x": map[string]any{"type": "string"}},
		"oneOf": []any{
			map[string]any{"$ref": "#/$defs/a"},
			map[string]any{"$ref": "#/$defs/z"},
		},
		"required": []any{"a", "b"},
	}
	ca, err := CanonicalJSON(a)
	if err != nil {
		t.Fatal(err)
	}
	cb, err := CanonicalJSON(b)
	if err != nil {
		t.Fatal(err)
	}
	if string(ca) != string(cb) {
		t.Errorf("canonical forms differ:\n%s\n%s", ca, cb)
	}

	// Ordinary arrays keep their order — only the two documented
	// order-insensitive classes are normalized.
	c1, _ := CanonicalJSON(map[string]any{"enum": []any{"b", "a"}})
	c2, _ := CanonicalJSON(map[string]any{"enum": []any{"a", "b"}})
	if string(c1) == string(c2) {
		t.Error("enum array order must be preserved — only required and oneOf-of-refs are sorted")
	}

	// A oneOf whose members are not all single-$ref objects keeps its order:
	// there the ordering may carry meaning (discriminated branches).
	m1, _ := CanonicalJSON(map[string]any{"oneOf": []any{
		map[string]any{"type": "string"}, map[string]any{"type": "integer"},
	}})
	m2, _ := CanonicalJSON(map[string]any{"oneOf": []any{
		map[string]any{"type": "integer"}, map[string]any{"type": "string"},
	}})
	if string(m1) == string(m2) {
		t.Error("non-ref oneOf members must keep their order")
	}
}

func TestCanonicalJSONDoesNotMutateInput(t *testing.T) {
	in := map[string]any{"required": []any{"b", "a"}}
	if _, err := CanonicalJSON(in); err != nil {
		t.Fatal(err)
	}
	got := in["required"].([]any)
	if got[0] != "b" || got[1] != "a" {
		t.Errorf("input mutated by canonicalization: %v", got)
	}
}

func TestCanonicalJSONNestedNormalization(t *testing.T) {
	// Normalization must reach arbitrarily deep — variants nest required
	// lists inside $defs and inside union branches.
	deep := map[string]any{
		"$defs": map[string]any{
			"inner": map[string]any{"required": []any{"z", "a"}},
		},
	}
	raw, err := CanonicalJSON(deep)
	if err != nil {
		t.Fatal(err)
	}
	want := `"required": [
        "a",
        "z"
      ]`
	if got := string(raw); !contains(got, want) {
		t.Errorf("nested required not sorted:\n%s", got)
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (func() bool {
		for i := 0; i+len(needle) <= len(haystack); i++ {
			if haystack[i:i+len(needle)] == needle {
				return true
			}
		}
		return false
	})()
}

// TestCanonicalJSONSortsAnyOfRefs covers the metadata union after it moved
// from oneOf to anyOf (python-sdk 35af25c). Python builds the member list in
// dict insertion order and Go sorts $defs names, so without normalising
// anyOf the two would differ by ordering alone and the parity test would
// report a mismatch that is not one.
func TestCanonicalJSONSortsAnyOfRefs(t *testing.T) {
	doc := map[string]any{
		"anyOf": []any{
			map[string]any{"$ref": "#/$defs/platform_schema"},
			map[string]any{"$ref": "#/$defs/business_schema"},
		},
	}
	got, err := CanonicalJSON(doc)
	if err != nil {
		t.Fatalf("CanonicalJSON: %v", err)
	}
	want := `{
  "anyOf": [
    {
      "$ref": "#/$defs/business_schema"
    },
    {
      "$ref": "#/$defs/platform_schema"
    }
  ]
}`
	if strings.TrimSpace(string(got)) != want {
		t.Errorf("anyOf members not sorted:\ngot:\n%s\nwant:\n%s", got, want)
	}
}
