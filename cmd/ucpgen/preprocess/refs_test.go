package preprocess

import (
	"errors"
	"testing"
)

func TestResolveLocalRef(t *testing.T) {
	root := map[string]any{
		"$defs": map[string]any{
			"money": map[string]any{"type": "integer", "minimum": float64(0)},
		},
	}
	got, err := ResolveLocalRef("#/$defs/money", root)
	if err != nil {
		t.Fatalf("ResolveLocalRef: %v", err)
	}
	if got["type"] != "integer" {
		t.Errorf("type = %v, want integer", got["type"])
	}
	if _, err := ResolveLocalRef("#/$defs/missing", root); err == nil {
		t.Error("want error for missing pointer, got nil")
	}
}

func TestResolveLocalRefErrorClasses(t *testing.T) {
	root := map[string]any{
		"$defs": map[string]any{
			"str": "not-an-object",
		},
	}
	if _, err := ResolveLocalRef("external.json#/x", root); !errors.Is(err, ErrRefNotFound) {
		t.Errorf("external ref: err = %v, want ErrRefNotFound", err)
	}
	if _, err := ResolveLocalRef("#/$defs/missing", root); !errors.Is(err, ErrRefNotFound) {
		t.Errorf("missing segment: err = %v, want ErrRefNotFound", err)
	}
	if _, err := ResolveLocalRef("#/$defs/str/nested", root); !errors.Is(err, ErrRefNotFound) {
		t.Errorf("mid-path non-object: err = %v, want ErrRefNotFound", err)
	}
	if _, err := ResolveLocalRef("#/$defs/str", root); !errors.Is(err, ErrRefNotObject) {
		t.Errorf("terminal non-object target: err = %v, want ErrRefNotObject", err)
	}
}

func TestResolveLocalRefsInlinesEntityBody(t *testing.T) {
	// The real case (python-sdk#72): the entity body carries a pointer that
	// only means anything inside ucp.json, and gets copied into documents
	// that define no such $def. Resolving it here makes the body portable.
	root := map[string]any{
		"$defs": map[string]any{
			"version": map[string]any{
				"type":    "string",
				"pattern": `^\d{4}-\d{2}-\d{2}$`,
			},
			"entity": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"version": map[string]any{
						"$ref":        "#/$defs/version",
						"description": "Entity version in YYYY-MM-DD format.",
					},
				},
			},
		},
	}
	entity := CopyTree(root["$defs"].(map[string]any)["entity"]).(map[string]any)
	ResolveLocalRefs(entity, root, nil)

	got := entity["properties"].(map[string]any)["version"].(map[string]any)
	if _, still := got["$ref"]; still {
		t.Fatalf("$ref should be gone after inlining, got %v", got)
	}
	if got["pattern"] != `^\d{4}-\d{2}-\d{2}$` || got["type"] != "string" {
		t.Errorf("target body not inlined: %v", got)
	}
	// Keys written alongside the $ref win over the target's, so a local
	// description is not lost to the shared definition's.
	if got["description"] != "Entity version in YYYY-MM-DD format." {
		t.Errorf("sibling key lost: %v", got["description"])
	}
	// The source document keeps its own $ref: only the copy is flattened.
	src := root["$defs"].(map[string]any)["entity"].(map[string]any)
	srcVer := src["properties"].(map[string]any)["version"].(map[string]any)
	if srcVer["$ref"] != "#/$defs/version" {
		t.Errorf("ucp.json's own entity was mutated: %v", srcVer)
	}
}

func TestResolveLocalRefsLeavesUnresolvableRefsAlone(t *testing.T) {
	// python's resolve_local_ref returns None and the caller skips. An
	// external ref is not ours to resolve at this stage, and a missing
	// local one is left for the later cross-file pass to report.
	frag := map[string]any{
		"a": map[string]any{"$ref": "other.json#/$defs/thing"},
		"b": map[string]any{"$ref": "#/$defs/absent"},
	}
	ResolveLocalRefs(frag, map[string]any{"$defs": map[string]any{}}, nil)

	if got := frag["a"].(map[string]any)["$ref"]; got != "other.json#/$defs/thing" {
		t.Errorf("external ref was touched: %v", got)
	}
	if got := frag["b"].(map[string]any)["$ref"]; got != "#/$defs/absent" {
		t.Errorf("unresolvable local ref was touched: %v", got)
	}
}

func TestResolveLocalRefsWalksArrays(t *testing.T) {
	root := map[string]any{
		"$defs": map[string]any{"money": map[string]any{"type": "number"}},
	}
	frag := map[string]any{
		"allOf": []any{
			map[string]any{"$ref": "#/$defs/money"},
		},
	}
	ResolveLocalRefs(frag, root, nil)
	got := frag["allOf"].([]any)[0].(map[string]any)
	if got["type"] != "number" {
		t.Errorf("ref inside an array was not resolved: %v", got)
	}
}
