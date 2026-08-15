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
	e := newFileEmitter(idxFixture(t), "shopping/types/line_item.json", "types")
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
	// The proxy `required` would call this a pointer. It is not: fieldsFor
	// leaves nilable types unpointered so callers write v.Tags[0].
	if got["tags"].goType != "[]string" {
		t.Errorf("optional slice: goType = %q, want %q", got["tags"].goType, "[]string")
	}
}

// fieldsFor sets the nested-type prefix itself. A caller that had to set
// it first would get silently mis-named types on forgetting to, so the
// invariant is checked here rather than trusted at every call site.
func TestFieldsForNamespacesNestedTypesWithoutCallerSetup(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"adjustment": map[string]any{
				"type":       "object",
				"properties": map[string]any{"reason": map[string]any{"type": "string"}},
			},
		},
	}
	e := newFileEmitter(idxFixture(t), "shopping/types/line_item.json", "types")
	before := e.prefix
	fields, err := fieldsFor(e, "Total", schema)
	if err != nil {
		t.Fatalf("fieldsFor: %v", err)
	}
	if got, want := fields[0].goType, "*TotalAdjustment"; got != want {
		t.Errorf("inline object goType = %q, want %q", got, want)
	}
	if _, registered := e.nestedSchemas["TotalAdjustment"]; !registered {
		t.Errorf("nested type was not registered under the enclosing type's name: %v", e.nestedSchemas)
	}
	// The prefix is restored, so renderStruct's own later work — and any
	// sibling call — sees the emitter exactly as it left it.
	if e.prefix != before {
		t.Errorf("prefix = %q after fieldsFor, want it restored to %q", e.prefix, before)
	}
}

func totalFields() []structField {
	return []structField{
		{jsonName: "amount", goName: "Amount", goType: "SignedAmount", required: true},
		{jsonName: "display_text", goName: "DisplayText", goType: "*string"},
		{jsonName: "type", goName: "Type", goType: "string", required: true},
	}
}

func TestPredicateConstOnRequiredField(t *testing.T) {
	node := map[string]any{
		"properties": map[string]any{"type": map[string]any{"const": "subtotal"}},
		"required":   []any{"type"},
	}
	got, err := predicate(newFileEmitter(idxFixture(t), "shopping/types/total.json", "types"), "Total", "v", node, totalFields())
	if err != nil {
		t.Fatalf("predicate: %v", err)
	}
	want := `v.Type == "subtotal"`
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestPredicateEnumOnRequiredField(t *testing.T) {
	node := map[string]any{
		"properties": map[string]any{"type": map[string]any{"enum": []any{"discount", "items_discount"}}},
		"required":   []any{"type"},
	}
	got, err := predicate(newFileEmitter(idxFixture(t), "shopping/types/total.json", "types"), "Total", "v", node, totalFields())
	if err != nil {
		t.Fatalf("predicate: %v", err)
	}
	want := `(v.Type == "discount" || v.Type == "items_discount")`
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// A negated enum is the one shape the zero value falsely satisfies: "" is
// not in the excluded list, so a hand-built value would match. It must be
// gated on the presence record.
func TestPredicateNegatedEnumGatesOnPresence(t *testing.T) {
	node := map[string]any{
		"properties": map[string]any{
			"type": map[string]any{"not": map[string]any{"enum": []any{"subtotal", "total"}}},
		},
		"required": []any{"type"},
	}
	got, err := predicate(newFileEmitter(idxFixture(t), "shopping/types/total.json", "types"), "Total", "v", node, totalFields())
	if err != nil {
		t.Fatalf("predicate: %v", err)
	}
	want := `v.present != nil && v.present["type"] && v.Type != "subtotal" && v.Type != "total"`
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestPredicateRequiredOnlyUsesPointerNilCheck(t *testing.T) {
	node := map[string]any{"required": []any{"display_text"}}
	got, err := predicate(newFileEmitter(idxFixture(t), "shopping/types/total.json", "types"), "Total", "v", node, totalFields())
	if err != nil {
		t.Fatalf("predicate: %v", err)
	}
	want := `v.DisplayText != nil`
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestPredicateRejectsOptionalPropertyTestWithoutRequired(t *testing.T) {
	// `properties` without `required` also matches an absent property, so
	// compiling it as present-and-matching would under-apply the rule the
	// condition guards.
	node := map[string]any{
		"properties": map[string]any{"display_text": map[string]any{"const": "x"}},
	}
	_, err := predicate(newFileEmitter(idxFixture(t), "shopping/types/line_item.json", "types"), "Total", "v", node, totalFields())
	if err == nil {
		t.Fatal("expected an error for a properties test with no required, got nil")
	}
}

func TestConditionalLiteralRejectsFractionalIntegerBound(t *testing.T) {
	f := structField{jsonName: "count", goName: "Count", goType: "int64", required: true}
	if _, err := conditionalLiteral("Thing", f, 1.5); err == nil {
		t.Fatal("expected an error for a fractional literal against int64, got nil")
	}
	if _, err := conditionalLiteral("Thing", f, float64(3)); err != nil {
		t.Errorf("integral literal against int64 should be accepted: %v", err)
	}
}

func TestPredicateRejectsUnsupportedForms(t *testing.T) {
	cases := map[string]map[string]any{
		"multi-property discriminator": {
			"properties": map[string]any{
				"type":   map[string]any{"const": "a"},
				"amount": map[string]any{"const": float64(1)},
			},
			"required": []any{"type", "amount"},
		},
		"unsupported keyword": {
			"properties": map[string]any{"type": map[string]any{"minLength": float64(3)}},
			"required":   []any{"type"},
		},
		"unknown property": {
			"properties": map[string]any{"nope": map[string]any{"const": "x"}},
			"required":   []any{"nope"},
		},
		"nested subschema keyword": {
			"allOf": []any{map[string]any{"const": "x"}},
		},
	}
	for name, node := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := predicate(newFileEmitter(idxFixture(t), "shopping/types/total.json", "types"), "Total", "v", node, totalFields()); err == nil {
				t.Fatal("expected an error, got nil")
			}
		})
	}
}
