package emit

import "testing"

func TestFieldsForCarriesGoType(t *testing.T) {
	schema := map[string]any{
		"type":     "object",
		"required": []any{"type"},
		"properties": map[string]any{
			"type":         map[string]any{"type": "string"},
			"display_text": map[string]any{"type": "string"},
			"tags":         map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		},
	}
	e := newFileEmitter(idxFixture(t), "shopping/types/total.json", "types")
	fields, err := fieldsFor(e, "Total", schema)
	if err != nil {
		t.Fatalf("fieldsFor: %v", err)
	}
	got := map[string]structField{}
	for _, f := range fields {
		got[f.jsonName] = f
	}
	if got["type"].goType != "string" {
		t.Errorf("required scalar: goType = %q, want %q", got["type"].goType, "string")
	}
	if got["display_text"].goType != "*string" {
		t.Errorf("optional scalar: goType = %q, want %q", got["display_text"].goType, "*string")
	}
	// The proxy `required` would call this a pointer. It is not: renderStruct
	// leaves nilable types unpointered so callers write v.Tags[0].
	if got["tags"].goType != "[]string" {
		t.Errorf("optional slice: goType = %q, want %q", got["tags"].goType, "[]string")
	}
}
