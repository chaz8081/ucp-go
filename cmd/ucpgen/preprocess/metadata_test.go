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

	oneOf := set.Files["ucp.json"]["oneOf"].([]any)
	if len(oneOf) != 4 {
		t.Fatalf("ucp.json oneOf = %d members, want 4 (platform, business, 2 responses; entity excluded)", len(oneOf))
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
