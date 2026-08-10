package preprocess

import "testing"

// TestPipelineSyntheticDottedRefs exercises the dotted-def rename +
// cross-file rewrite transform pair end to end. The real UCP spec has zero
// dotted $defs refs across all 78 files, so the goldens (Task 9) will never
// cover this path — this synthetic 3-file fixture
// (testdata/synth/{a.json,ucp.json,sub/b.json}) is the only coverage it
// gets. It walks the pipeline in the order the real driver (Task 9) will
// use — ucp.json excluded from the per-file FlattenDottedDefs/
// PreprocessDocument loop, same as there:
//
//	NormalizeMetadata(set)
//	for each non-ucp.json file: renames[file] = FlattenDottedDefs(file); PreprocessDocument(file, nil)
//	RewriteExternalDefsRefs(set, renames)
func TestPipelineSyntheticDottedRefs(t *testing.T) {
	set, err := LoadSchemas("testdata/synth")
	if err != nil {
		t.Fatalf("LoadSchemas: %v", err)
	}
	for _, want := range []string{"a.json", "ucp.json", "sub/b.json"} {
		if _, ok := set.Files[want]; !ok {
			t.Fatalf("fixture missing %s; got %v", want, set.Paths())
		}
	}

	NormalizeMetadata(set)

	renames := map[string]map[string]string{}
	for _, rel := range set.Paths() {
		if rel == "ucp.json" {
			continue
		}
		schema := set.Files[rel]
		// Mirror the real driver: only store non-empty rename maps.
		if rm := FlattenDottedDefs(schema); len(rm) > 0 {
			renames[rel] = rm
		}
		// nil entityDef: entity flattening is out of scope for this
		// transform pair, so ucp.json#/$defs/entity refs are left as
		// unresolved external refs throughout — see below.
		if err := PreprocessDocument(schema, nil); err != nil {
			t.Fatalf("PreprocessDocument(%s): %v", rel, err)
		}
	}

	RewriteExternalDefsRefs(set, renames)

	a := set.Files["a.json"]
	aDefs := a["$defs"].(map[string]any)

	// 1. Dotted def renamed with collision fallback: "dev.ucp.ext.other"'s
	// tail "other" collides with the sibling def literally named "other",
	// so it must fall back to the dot->underscore name.
	if _, has := aDefs["dev.ucp.ext.other"]; has {
		t.Error("dev.ucp.ext.other should have been renamed away")
	}
	if _, ok := aDefs["dev_ucp_ext_other"]; !ok {
		t.Errorf("collision fallback name dev_ucp_ext_other missing; defs: %v", keys(aDefs))
	}
	// "dev.ucp.ext.thing"'s tail "thing" doesn't collide, so it gets the
	// bare tail name.
	if _, has := aDefs["dev.ucp.ext.thing"]; has {
		t.Error("dev.ucp.ext.thing should have been renamed away")
	}
	if _, ok := aDefs["thing"]; !ok {
		t.Errorf("bare-tail rename name thing missing; defs: %v", keys(aDefs))
	}

	// 2. Local ref + tailed ref rewritten. "deep.properties.x" is a plain
	// property $ref (not an allOf branch), so PreprocessDocument's merge
	// walk never touches it — it survives untouched from the
	// FlattenDottedDefs rewrite, making it a clean probe for the rename.
	deep := aDefs["deep"].(map[string]any)
	xRef := deep["properties"].(map[string]any)["x"].(map[string]any)["$ref"]
	if xRef != "#/$defs/thing/properties/t" {
		t.Errorf("local tailed ref not rewritten: got %v, want #/$defs/thing/properties/t", xRef)
	}

	// 3. Cross-file ref from the subdirectory rewritten: sub/b.json's
	// "y" and "z" properties point at a.json's renamed defs.
	b := set.Files["sub/b.json"]
	bProps := b["properties"].(map[string]any)
	yRef := bProps["y"].(map[string]any)["$ref"]
	if yRef != "../a.json#/$defs/thing" {
		t.Errorf("cross-file ref not rewritten: got %v, want ../a.json#/$defs/thing", yRef)
	}
	zRef := bProps["z"].(map[string]any)["$ref"]
	if zRef != "../a.json#/$defs/dev_ucp_ext_other/properties/o" {
		t.Errorf("cross-file tailed ref not rewritten: got %v, want ../a.json#/$defs/dev_ucp_ext_other/properties/o", zRef)
	}

	// 4. The same cross-file rename, but reached through a ref that
	// PreprocessDocument left dangling inside a slim (unresolved) allOf:
	// sub/b.json's $defs.wrapper.allOf[0] points at a.json's
	// dev.ucp.ext.thing across the directory boundary, and — because
	// "../a.json#/..." never resolves as a local ref — MergeAllOf can't
	// touch it, so it's still a raw $ref sitting inside node["allOf"]
	// when RewriteExternalDefsRefs runs. That later pass must still find
	// and rewrite it via its own IterNodes walk.
	wrapper := b["$defs"].(map[string]any)["wrapper"].(map[string]any)
	wrapperAllOf := wrapper["allOf"].([]any)
	found := false
	for _, item := range wrapperAllOf {
		if ref, _ := item.(map[string]any)["$ref"].(string); ref == "../a.json#/$defs/thing" {
			found = true
		}
	}
	if !found {
		t.Errorf("cross-file ref nested inside wrapper's slim allOf not rewritten: %v", wrapperAllOf)
	}
}
