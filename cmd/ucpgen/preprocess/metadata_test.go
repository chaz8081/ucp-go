package preprocess

import "testing"

func TestNormalizeMetadata(t *testing.T) {
	set := &SchemaSet{Files: map[string]map[string]any{
		"ucp.json": {
			"$defs": map[string]any{
				"platform_schema":         map[string]any{},
				"business_schema":         map[string]any{},
				"response_cart_schema":    map[string]any{},
				"response_catalog_schema": map[string]any{},
				"entity":                  map[string]any{},
			},
		},
		"shopping/checkout.json": {
			"properties": map[string]any{
				"ucp": map[string]any{"$ref": "../ucp.json#/$defs/response_schema"},
			},
		},
		"shopping/checkout_create_request.json": {
			"properties": map[string]any{
				"ucp": map[string]any{"$ref": "../ucp.json#/$defs/response_schema"},
			},
		},
	}}
	NormalizeMetadata(set)

	anyOf := set.Files["ucp.json"]["anyOf"].([]any)
	if len(anyOf) != 4 {
		t.Fatalf("ucp.json anyOf = %d members, want 4 (platform, business, 2 responses; entity excluded)", len(anyOf))
	}
	got := set.Files["shopping/checkout.json"]["properties"].(map[string]any)["ucp"].(map[string]any)["$ref"]
	if got != "../ucp.json" {
		t.Errorf("ucp property ref not truncated to file part: %v", got)
	}
	// _request.json files are skipped (preprocess_schemas.py:568).
	got = set.Files["shopping/checkout_create_request.json"]["properties"].(map[string]any)["ucp"].(map[string]any)["$ref"]
	if got != "../ucp.json#/$defs/response_schema" {
		t.Errorf("_request.json file must be skipped, got %v", got)
	}
}

// TestNormalizeMetadataUsesAnyOf mirrors python-sdk 35af25c (#76), which
// fixed the union this SDK reported as unsatisfiable.
//
// The synthesized union is a type union for code generation, not an
// exclusivity constraint. Three of ucp.json's response profiles are
// identical apart from title and description, so `oneOf` — exactly one —
// could never be satisfied, and `ucp` is required on Cart, Checkout and
// Order.
func TestNormalizeMetadataUsesAnyOf(t *testing.T) {
	ucp := map[string]any{
		"title": "UCP Metadata",
		"$defs": map[string]any{
			"business_schema":         map[string]any{"type": "object"},
			"platform_schema":         map[string]any{"type": "object"},
			"response_cart_schema":    map[string]any{"type": "object"},
			"response_catalog_schema": map[string]any{"type": "object"},
			"entity":                  map[string]any{"type": "object"},
		},
	}
	NormalizeMetadata(&SchemaSet{Files: map[string]map[string]any{"ucp.json": ucp}})
	if _, wrong := ucp["oneOf"]; wrong {
		t.Error("the metadata union must be anyOf; oneOf asserts an exclusivity the members cannot satisfy")
	}
	members, ok := ucp["anyOf"].([]any)
	if !ok {
		t.Fatalf("no anyOf union was synthesized: %v", ucp)
	}
	if len(members) != 4 {
		t.Errorf("union has %d members, want 4 (the two profiles plus two response schemas)", len(members))
	}
}
