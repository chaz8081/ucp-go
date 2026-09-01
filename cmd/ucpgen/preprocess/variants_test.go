package preprocess

import (
	"encoding/json"
	"reflect"
	"sort"
	"strings"
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
	// python-sdk 3e1aace strips $id in PreprocessDocument, which runs before
	// variants are generated, so on the real corpus a variant is copied from
	// a document that has none and this branch never fires. It is kept
	// because upstream kept theirs — parity is defined by output, not by
	// which branches execute — and exercised here so it cannot rot silently
	// while unreachable.
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

// TestGenerateVariantsPropertylessObjectEmitsEmptyPropertiesAndRequired
// covers a real spec-review divergence: python's applyRequestRules
// equivalent defaults properties to {} via .get("properties", {}) and
// unconditionally assigns properties/required on every object-shaped
// variant (preprocess_schemas.py:468-470, 489-490), bailing only when
// properties is present but not a dict. An object schema with no
// "properties" key at all (e.g. attribution.json, fulfillment_destination
// .json, message.json in the real spec) must still get an explicit empty
// properties object and empty required array in its variant.
func TestGenerateVariantsPropertylessObjectEmitsEmptyPropertiesAndRequired(t *testing.T) {
	set := &SchemaSet{Files: map[string]map[string]any{
		"shopping/attribution.json": {
			"title": "Attribution",
			"type":  "object",
		},
	}}
	needs := map[string]map[string]bool{
		"shopping/attribution.json": {"create": true},
	}
	GenerateVariants(set, needs)

	v, ok := set.Files["shopping/attribution_create_request.json"]
	if !ok {
		t.Fatalf("create variant not added to set; files: %v", set.Paths())
	}
	if _, has := v["properties"]; !has {
		t.Fatal("variant missing properties key")
	}
	if _, has := v["required"]; !has {
		t.Fatal("variant missing required key")
	}
	out, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	s := string(out)
	if !strings.Contains(s, `"properties":{}`) {
		t.Errorf("expected empty properties object in JSON, got %s", s)
	}
	if !strings.Contains(s, `"required":[]`) {
		t.Errorf("expected empty required array in JSON, got %s", s)
	}
}

func TestApplyVariantIdentityEmptyOp(t *testing.T) {
	// "ucp_request": {"": "required"} is valid JSON and yields an empty op.
	// Python's "".capitalize() is "", producing a double-space title rather
	// than crashing; Go must not panic on op[:1].
	variant := map[string]any{"title": "A", "$id": "https://ucp.dev/schemas/a.json"}
	applyVariantIdentity(variant, "", "a")
	if got := variant["title"]; got != "A  Request" {
		t.Errorf("title = %q, want %q (python double space)", got, "A  Request")
	}
	if got := variant["$id"]; got != "https://ucp.dev/schemas/a__request.json" {
		t.Errorf("$id = %v, want a__request.json", got)
	}
}

func TestPropagateNeedsSkipsVariantTargets(t *testing.T) {
	// Python's schemas dict never holds generated variants (load-time filter),
	// so a variant can never become a propagation target. Our set.Files does
	// hold them after GenerateVariants, so the filter must be explicit.
	set := &SchemaSet{Files: map[string]map[string]any{
		"q.json": {
			"title": "Q", "type": "object",
			"properties": map[string]any{
				"c": map[string]any{"ucp_request": "required", "$ref": "c_create_request.json"},
			},
		},
		"c_create_request.json": {"title": "C Create Request", "type": "object"},
	}}
	needs := DiscoverVariantNeeds(set)
	PropagateNeeds(set, needs)
	if _, has := needs["c_create_request.json"]; has {
		t.Errorf("variant file must never receive needs: %v", needs)
	}
	GenerateVariants(set, needs)
	for _, p := range set.Paths() {
		if strings.Count(p, "_request") > 1 {
			t.Errorf("double-suffixed variant generated: %s", p)
		}
	}
}

func TestRewriteRefsToVariantsNormalizesDotSlash(t *testing.T) {
	// python builds the new ref via Path math (ref_path.parent / stem_op),
	// which drops a leading "./"; a raw TrimSuffix concat would keep it.
	needs := map[string]map[string]bool{"child.json": {"create": true}}
	data := map[string]any{"$ref": "./child.json"}
	rewriteRefsToVariants(data, "create", "parent.json", needs)
	if got := data["$ref"]; got != "child_create_request.json" {
		t.Errorf("$ref = %v, want normalized child_create_request.json", got)
	}
}

func TestGenerateVariantsSkipsUnknownPath(t *testing.T) {
	// python raises KeyError here; we skip, leaving fail-loud to the caller.
	set := &SchemaSet{Files: map[string]map[string]any{}}
	GenerateVariants(set, map[string]map[string]bool{"ghost.json": {"create": true}})
	if len(set.Files) != 0 {
		t.Errorf("phantom variant emitted for unknown path: %v", set.Paths())
	}
}

func TestVariantStageReentrant(t *testing.T) {
	// The one property goldens can never cover: they run the pipeline once.
	build := func() *SchemaSet {
		return &SchemaSet{Files: map[string]map[string]any{
			"parent.json": {
				"title": "Parent", "type": "object",
				"required": []any{"child"},
				"properties": map[string]any{
					"child": map[string]any{"ucp_request": "required", "$ref": "child.json"},
				},
			},
			"child.json": {
				"title": "Child", "type": "object",
				"properties": map[string]any{"sku": map[string]any{"type": "string"}},
			},
		}}
	}
	stage := func(set *SchemaSet) {
		needs := DiscoverVariantNeeds(set)
		PropagateNeeds(set, needs)
		GenerateVariants(set, needs)
	}
	set := build()
	stage(set)
	firstPaths := set.Paths()
	firstBody := map[string]string{}
	for _, p := range firstPaths {
		raw, err := json.Marshal(set.Files[p])
		if err != nil {
			t.Fatal(err)
		}
		firstBody[p] = string(raw)
	}

	stage(set) // second run over the already-populated set
	if !reflect.DeepEqual(set.Paths(), firstPaths) {
		t.Errorf("file set changed on re-run:\n%v\n%v", firstPaths, set.Paths())
	}
	for _, p := range set.Paths() {
		raw, err := json.Marshal(set.Files[p])
		if err != nil {
			t.Fatal(err)
		}
		if string(raw) != firstBody[p] {
			t.Errorf("%s changed on re-run:\n%s\n%s", p, firstBody[p], raw)
		}
		if strings.Count(p, "_request") > 1 {
			t.Errorf("double-suffixed variant on re-run: %s", p)
		}
	}
}

func TestExternalRefsIncludesFragmentRefs(t *testing.T) {
	// Upstream python-sdk (f8b714b) changed extract_external_refs to split
	// the fragment off and depend on the FILE part, so a ref like
	// "types/payment_instrument.json#/$defs/selected_payment_instrument"
	// now creates a dependency. Pure local refs ("#/$defs/x") still don't.
	schema := map[string]any{
		"properties": map[string]any{
			"instruments": map[string]any{
				"type":  "array",
				"items": map[string]any{"$ref": "types/payment_instrument.json#/$defs/selected"},
			},
			"local": map[string]any{"$ref": "#/$defs/thing"},
			"plain": map[string]any{"$ref": "types/buyer.json"},
		},
	}
	got := externalRefs("shopping/payment.json", schema)
	want := map[string]string{
		"instruments": "shopping/types/payment_instrument.json",
		"plain":       "shopping/types/buyer.json",
	}
	if len(got) != len(want) {
		t.Fatalf("refs = %v, want %v", got, want)
	}
	for _, pair := range got {
		if want[pair[0]] != pair[1] {
			t.Errorf("ref for %q = %q, want %q", pair[0], pair[1], want[pair[0]])
		}
	}
}

func TestRewriteRefsToVariantsPreservesFragment(t *testing.T) {
	// The paired upstream change: the file part is rewritten to the variant
	// and the fragment is re-appended unchanged.
	needs := map[string]map[string]bool{
		"shopping/types/payment_instrument.json": {"create": true},
	}
	data := map[string]any{
		"items": map[string]any{"$ref": "types/payment_instrument.json#/$defs/selected"},
		"local": map[string]any{"$ref": "#/$defs/untouched"},
	}
	rewriteRefsToVariants(data, "create", "shopping/payment.json", needs)
	got := data["items"].(map[string]any)["$ref"]
	if got != "types/payment_instrument_create_request.json#/$defs/selected" {
		t.Errorf("$ref = %v, want variant with fragment preserved", got)
	}
	if got := data["local"].(map[string]any)["$ref"]; got != "#/$defs/untouched" {
		t.Errorf("pure local ref must not be rewritten: %v", got)
	}
}

func TestExternalRefsScansTopLevelComposition(t *testing.T) {
	// python-sdk 51bf73c (PR #83, fixing python-sdk#34). Scanning only
	// `properties` left a schema whose alternatives live in a root
	// oneOf/anyOf/allOf looking dependency-free, so no variant need
	// propagated onto its members, no variants were generated for them, and
	// the refs that should have been rewritten had nothing to point at.
	//
	// The bug therefore erased its own evidence: the un-rewritten refs
	// looked correct precisely because the missing variants were missing.
	schema := map[string]any{
		"type": "object",
		"oneOf": []any{
			map[string]any{"$ref": "shipping_destination.json"},
			map[string]any{"$ref": "retail_location.json"},
		},
	}
	got := externalRefs("shopping/types/fulfillment_destination.json", schema)
	want := map[string]bool{
		"shopping/types/shipping_destination.json": true,
		"shopping/types/retail_location.json":      true,
	}
	if len(got) != len(want) {
		t.Fatalf("refs = %v, want the two oneOf members", got)
	}
	for _, pair := range got {
		if !want[pair[1]] {
			t.Errorf("unexpected ref %q", pair[1])
		}
		// The keyword stands in for the property name, matching python
		// passing the key itself. No such property exists, so inclusion
		// falls back to its default.
		if pair[0] != "oneOf" {
			t.Errorf("ref %q reported under name %q, want \"oneOf\"", pair[1], pair[0])
		}
	}
}

func TestExternalRefsScansItems(t *testing.T) {
	// The same change also scans a root-level `items`, which is how
	// totals.json reaches total.json.
	schema := map[string]any{
		"type":  "array",
		"items": map[string]any{"$ref": "total.json"},
	}
	got := externalRefs("shopping/types/totals.json", schema)
	if len(got) != 1 || got[0][1] != "shopping/types/total.json" || got[0][0] != "items" {
		t.Fatalf("refs = %v, want one items -> shopping/types/total.json", got)
	}
}
