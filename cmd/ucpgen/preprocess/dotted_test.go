package preprocess

import "testing"

func TestFlattenDottedDefs(t *testing.T) {
	schema := map[string]any{
		"$defs": map[string]any{
			"dev.ucp.shopping.checkout": map[string]any{"type": "object"},
			"dev.ucp.mount.total":       map[string]any{"type": "object"},
			"total":                     map[string]any{"type": "integer"},
			"plain":                     map[string]any{"type": "string"},
		},
		"properties": map[string]any{
			"c": map[string]any{"$ref": "#/$defs/dev.ucp.shopping.checkout"},
			"t": map[string]any{"$ref": "#/$defs/dev.ucp.mount.total/properties/x"},
		},
	}
	rename := FlattenDottedDefs(schema)
	defs := schema["$defs"].(map[string]any)
	if _, ok := defs["checkout"]; !ok {
		t.Error("bare-tail rename missing: want $defs.checkout")
	}
	// tail "total" collides with existing def -> dot->underscore fallback
	if _, ok := defs["dev_ucp_mount_total"]; !ok {
		t.Errorf("collision fallback missing, defs: %v", keys(defs))
	}
	if _, ok := defs["plain"]; !ok {
		t.Error("undotted def must survive untouched")
	}
	props := schema["properties"].(map[string]any)
	if got := props["c"].(map[string]any)["$ref"]; got != "#/$defs/checkout" {
		t.Errorf("local ref not rewritten: %v", got)
	}
	if got := props["t"].(map[string]any)["$ref"]; got != "#/$defs/dev_ucp_mount_total/properties/x" {
		t.Errorf("local ref with tail not rewritten: %v", got)
	}
	if rename["dev.ucp.shopping.checkout"] != "checkout" {
		t.Errorf("rename map wrong: %v", rename)
	}
}

func TestRewriteExternalDefsRefs(t *testing.T) {
	set := &SchemaSet{Files: map[string]map[string]any{
		"shopping/checkout.json": {
			"properties": map[string]any{
				"x": map[string]any{"$ref": "types/total.json#/$defs/dev.ucp.mount.total"},
			},
		},
		"shopping/types/total.json": {},
	}}
	renames := map[string]map[string]string{
		"shopping/types/total.json": {"dev.ucp.mount.total": "total"},
	}
	RewriteExternalDefsRefs(set, renames)
	got := set.Files["shopping/checkout.json"]["properties"].(map[string]any)["x"].(map[string]any)["$ref"]
	if got != "types/total.json#/$defs/total" {
		t.Errorf("external ref not rewritten: %v", got)
	}
}
