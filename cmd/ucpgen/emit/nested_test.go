package emit

import (
	"strings"
	"testing"
)

// TestEmitValidateRecursesIntoGeneratedTypes pins the difference between
// checking a value and checking the document it stands for. A JSON Schema
// validator applies a subschema wherever it is referenced, so a Checkout
// whose line item breaks a constraint is invalid — a Validate that stops at
// the top level would call it fine.
func TestEmitValidateRecursesIntoGeneratedTypes(t *testing.T) {
	schema := map[string]any{
		"title": "Holder", "type": "object",
		"required": []any{"one"},
		"properties": map[string]any{
			"one":    map[string]any{"$ref": "line_item.json"},
			"many":   map[string]any{"type": "array", "items": map[string]any{"$ref": "line_item.json"}},
			"by_key": map[string]any{"type": "object", "additionalProperties": map[string]any{"$ref": "line_item.json"}},
			"maybe":  map[string]any{"$ref": "line_item.json"},
			"plain":  map[string]any{"type": "string"},
		},
	}
	corpus := map[string]map[string]any{
		"test/holder.json": schema,
		"test/line_item.json": {
			"title": "Line Item", "type": "object",
			"properties": map[string]any{"sku": map[string]any{"type": "string", "maxLength": float64(4)}},
		},
	}
	src, err := emitFromCorpus(t, "test/holder.json", corpus)
	if err != nil {
		t.Fatalf("EmitFile: %v", err)
	}
	for _, want := range []string{
		"if err := v.One.Validate(); err != nil {",
		"for i := range v.Many {",
		"if err := v.Many[i].Validate(); err != nil {",
		"for _, m := range v.ByKey {",
		"if v.Maybe != nil {",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("missing %q in:\n%s", want, src)
		}
	}
	// A scalar has no Validate to call.
	if strings.Contains(src, "v.Plain.Validate()") {
		t.Errorf("a string field must not be recursed into:\n%s", src)
	}
}

// A reference degraded to raw JSON to break an import cycle has no Validate
// method, so calling one would not compile.
func TestEmitValidateSkipsDegradedRefs(t *testing.T) {
	if hasValidateMethod("json.RawMessage") {
		t.Error("json.RawMessage has no Validate method")
	}
	for _, typ := range []string{"string", "*string", "[]string", "int64", "float64", "bool", "any", "[]any", "map[string]any"} {
		if hasValidateMethod(typ) {
			t.Errorf("%s has no Validate method", typ)
		}
	}
	for _, typ := range []string{"LineItem", "*LineItem", "[]LineItem", "map[string]LineItem", "types.LineItem", "[]types.LineItem"} {
		if !hasValidateMethod(typ) {
			t.Errorf("%s is a generated type and has Validate", typ)
		}
	}
}
