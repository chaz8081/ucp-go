package emit

import "testing"

func TestGoTypeExpr(t *testing.T) {
	idx := idxFixture(t)
	cases := []struct {
		name string
		node map[string]any
		want string
	}{
		{"string", map[string]any{"type": "string"}, "string"},
		{"integer", map[string]any{"type": "integer"}, "int64"},
		{"ref same package", map[string]any{"$ref": "../shopping/checkout.json"}, "Checkout"},
		{"array of ref", map[string]any{
			"type": "array", "items": map[string]any{"$ref": "types/line_item.json"},
		}, "[]types.LineItem"},
		{"map of scalar", map[string]any{
			"type": "object", "additionalProperties": map[string]any{"type": "string"},
		}, "map[string]string"},
		{"map of array of ref", map[string]any{
			"type": "object",
			"additionalProperties": map[string]any{
				"type": "array", "items": map[string]any{"$ref": "../capability.json#/$defs/base"},
			},
		}, "map[string][]ucp.CapabilityBase"},
		{"nullable string", map[string]any{"type": []any{"string", "null"}}, "*string"},
		{"untyped", map[string]any{}, "any"},
		{"bare object", map[string]any{"type": "object"}, "map[string]any"},
		{"inline union no properties", map[string]any{
			"oneOf": []any{
				map[string]any{"type": "string"},
				map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			},
		}, "json.RawMessage"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := newFileEmitter(idx, "shopping/checkout.json", "shopping")
			got, err := e.goTypeExpr(c.node, "Field")
			if err != nil {
				t.Fatalf("goTypeExpr: %v", err)
			}
			if got != c.want {
				t.Errorf("goTypeExpr = %q, want %q", got, c.want)
			}
		})
	}
}

func TestGoTypeExprRecordsImports(t *testing.T) {
	idx := idxFixture(t)
	e := newFileEmitter(idx, "shopping/checkout.json", "shopping")
	if _, err := e.goTypeExpr(map[string]any{"$ref": "types/line_item.json"}, "F"); err != nil {
		t.Fatal(err)
	}
	if _, ok := e.imports["github.com/chaz8081/ucp-go/shopping/types"]; !ok {
		t.Errorf("cross-package ref must record an import; got %v", e.imports)
	}
	e2 := newFileEmitter(idx, "shopping/checkout.json", "shopping")
	if _, err := e2.goTypeExpr(map[string]any{"$ref": "../shopping/checkout.json"}, "F"); err != nil {
		t.Fatal(err)
	}
	if len(e2.imports) != 0 {
		t.Errorf("same-package ref must not record an import; got %v", e2.imports)
	}
}

func TestGoTypeExprInlineObjectMakesNestedType(t *testing.T) {
	idx := idxFixture(t)
	e := newFileEmitter(idx, "shopping/checkout.json", "shopping")
	got, err := e.goTypeExpr(map[string]any{
		"type":       "object",
		"properties": map[string]any{"carrier": map[string]any{"type": "string"}},
	}, "Fulfillment")
	if err != nil {
		t.Fatal(err)
	}
	if got != "CheckoutFulfillment" {
		t.Errorf("inline object type = %q, want CheckoutFulfillment", got)
	}
	if len(e.nested) != 1 || e.nested[0].name != "CheckoutFulfillment" {
		t.Errorf("nested type not queued for emission: %+v", e.nested)
	}
}

func TestGoTypeExprRejectsMultiTypeUnion(t *testing.T) {
	idx := idxFixture(t)
	e := newFileEmitter(idx, "shopping/checkout.json", "shopping")
	if _, err := e.goTypeExpr(map[string]any{"type": []any{"string", "integer"}}, "F"); err == nil {
		t.Error(`type ["string","integer"] must error; only ["x","null"] is supported`)
	}
}
