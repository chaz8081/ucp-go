package preprocess

import (
	"reflect"
	"sort"
	"testing"
)

func TestRequiredOps(t *testing.T) {
	schema := map[string]any{
		"properties": map[string]any{
			"a": map[string]any{"ucp_request": "required"},
			"b": map[string]any{"ucp_request": map[string]any{"complete": "required", "update": "omit"}},
			"c": map[string]any{"type": "string"},
		},
	}
	got := RequiredOps(schema)
	sort.Strings(got)
	// string marker -> create+update (:386-388); dict marker -> its keys (:389-390)
	want := []string{"complete", "create", "update"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("RequiredOps = %v, want %v", got, want)
	}
	if got := RequiredOps(map[string]any{}); len(got) != 0 {
		t.Errorf("no markers -> no ops, got %v", got)
	}
}

func TestEvalPropInclusion(t *testing.T) {
	baseReq := []any{"id"}
	cases := []struct {
		name     string
		propName string
		data     map[string]any
		op       string
		include  bool
		required bool
	}{
		{"no marker, not in base required", "x", map[string]any{}, "create", true, false},
		{"nil data, in base required", "id", nil, "create", true, true},
		{"omit string", "x", map[string]any{"ucp_request": "omit"}, "create", false, false},
		{"required string", "x", map[string]any{"ucp_request": "required"}, "create", true, true},
		{"optional string overrides base", "id", map[string]any{"ucp_request": "optional"}, "create", true, false},
		{"dict op required", "x", map[string]any{"ucp_request": map[string]any{"create": "required"}}, "create", true, true},
		{"dict op optional", "id", map[string]any{"ucp_request": map[string]any{"create": "optional"}}, "create", true, false},
		{"dict op omit", "x", map[string]any{"ucp_request": map[string]any{"create": "omit"}}, "create", false, false},
		{"dict missing op key -> omit", "x", map[string]any{"ucp_request": map[string]any{"update": "required"}}, "create", false, false},
		{"dict unknown value includes", "x", map[string]any{"ucp_request": map[string]any{"create": "banana"}}, "create", true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			inc, req := EvalPropInclusion(tc.propName, tc.data, tc.op, baseReq)
			if inc != tc.include || req != tc.required {
				t.Errorf("got (include=%v, required=%v), want (%v, %v)", inc, req, tc.include, tc.required)
			}
		})
	}
}

func TestGenerateVariantsEndToEnd(t *testing.T) {
	set := &SchemaSet{Files: map[string]map[string]any{
		"shopping/checkout.json": {
			"title":    "Checkout",
			"$id":      "https://ucp.dev/schemas/shopping/checkout.json",
			"type":     "object",
			"required": []any{"id", "line_items"},
			"properties": map[string]any{
				"id":         map[string]any{"type": "string", "ucp_request": "omit"},
				"line_items": map[string]any{"ucp_request": "required", "items": map[string]any{"$ref": "types/line_item.json"}},
				"note":       map[string]any{"type": "string", "ucp_request": map[string]any{"create": "optional"}},
			},
		},
		"shopping/types/line_item.json": {
			"title": "Line Item",
			"type":  "object",
			"properties": map[string]any{
				"sku": map[string]any{"type": "string"},
			},
		},
	}}

	needs := DiscoverVariantNeeds(set)
	PropagateNeeds(set, needs)
	if _, ok := needs["shopping/types/line_item.json"]; !ok {
		t.Fatalf("transitive propagation missed line_item: %v", needs)
	}

	GenerateVariants(set, needs)

	v, ok := set.Files["shopping/checkout_create_request.json"]
	if !ok {
		t.Fatalf("create variant not added to set; files: %v", set.Paths())
	}
	if v["title"] != "Checkout Create Request" {
		t.Errorf("variant title = %v", v["title"])
	}
	if v["$id"] != "https://ucp.dev/schemas/shopping/checkout_create_request.json" {
		t.Errorf("variant $id = %v", v["$id"])
	}
	props := v["properties"].(map[string]any)
	if _, has := props["id"]; has {
		t.Error("omitted property survived into variant")
	}
	if _, has := props["note"]; !has {
		t.Error("optional-for-create property missing")
	}
	if _, has := props["line_items"].(map[string]any)["ucp_request"]; has {
		t.Error("ucp_request marker not stripped")
	}
	ref := props["line_items"].(map[string]any)["items"].(map[string]any)["$ref"]
	if ref != "types/line_item_create_request.json" {
		t.Errorf("child ref not rewritten to variant: %v", ref)
	}
	req := v["required"].([]any)
	if len(req) != 1 || req[0] != "line_items" {
		t.Errorf("variant required = %v, want [line_items]", req)
	}
	if _, has := set.Files["shopping/checkout.json"]["properties"].(map[string]any)["id"]; !has {
		t.Error("variant generation mutated the source schema")
	}
	if _, ok := set.Files["shopping/types/line_item_create_request.json"]; !ok {
		t.Error("transitive child variant not generated")
	}
}

func TestPropagateNeedsUcpRules(t *testing.T) {
	set := &SchemaSet{Files: map[string]map[string]any{
		"ucp.json": {
			"title": "UCP",
			"$defs": map[string]any{"entity": map[string]any{}},
		},
		"shopping/order.json": {
			"title": "Order", "type": "object",
			"properties": map[string]any{
				"buyer": map[string]any{"type": "string", "ucp_request": "required"},
				"ucp":   map[string]any{"$ref": "../ucp.json"},
			},
		},
		"shopping/types/error_response.json": {
			"title": "Error Response", "type": "object",
			"properties": map[string]any{
				"ucp": map[string]any{"$ref": "../../ucp.json"},
			},
		},
	}}
	needs := DiscoverVariantNeeds(set)
	if _, has := needs["ucp.json"]; has {
		t.Fatal("ucp.json must not self-discover needs")
	}
	PropagateNeeds(set, needs)
	// order.json's ops propagate onto ucp.json through the file-only ref...
	ops, ok := needs["ucp.json"]
	if !ok || !ops["create"] || !ops["update"] {
		t.Fatalf("ucp.json should receive create+update from order.json, got %v", needs)
	}
	// ...but error_response (no ops of its own) must not propagate anything.
	if _, has := needs["shopping/types/error_response.json"]; has {
		t.Error("error_response must not gain needs")
	}
	GenerateVariants(set, needs)
	uv, ok := set.Files["ucp_create_request.json"]
	if !ok {
		t.Fatalf("ucp_create_request.json not generated; files: %v", set.Paths())
	}
	if uv["title"] != "UCP Create Request" {
		t.Errorf("ucp variant title = %v", uv["title"])
	}
	// No properties at root: no request rules applied, $defs carried as-is.
	if _, has := uv["$defs"]; !has {
		t.Error("ucp variant lost $defs")
	}
}
