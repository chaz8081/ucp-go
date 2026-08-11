package preprocess

import (
	"reflect"
	"sort"
	"testing"
)

func TestMergeAllOf(t *testing.T) {
	root := map[string]any{
		"$defs": map[string]any{
			"base": map[string]any{
				"type":     "object",
				"required": []any{"id"},
				"properties": map[string]any{
					"id": map[string]any{"type": "string"},
				},
			},
		},
	}
	node := map[string]any{
		"allOf": []any{
			map[string]any{"$ref": "#/$defs/base"},
			map[string]any{
				"type":     "object",
				"required": []any{"name"},
				"properties": map[string]any{
					"name": map[string]any{"type": "string"},
				},
			},
		},
	}
	if err := MergeAllOf(node, root); err != nil {
		t.Fatalf("MergeAllOf: %v", err)
	}
	if _, has := node["allOf"]; has {
		t.Error("allOf should be removed after merge")
	}
	props := node["properties"].(map[string]any)
	for _, want := range []string{"id", "name"} {
		if _, ok := props[want]; !ok {
			t.Errorf("merged properties missing %q", want)
		}
	}
	var req []string
	for _, r := range node["required"].([]any) {
		req = append(req, r.(string))
	}
	sort.Strings(req)
	if !reflect.DeepEqual(req, []string{"id", "name"}) {
		t.Errorf("required = %v, want [id name]", req)
	}
}

func TestMergeAllOfPrecedence(t *testing.T) {
	root := map[string]any{
		"$defs": map[string]any{
			"base": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"services": map[string]any{"$ref": "service.json#/$defs/base"},
				},
			},
		},
	}
	node := map[string]any{
		"description": "node docs",
		"properties": map[string]any{
			"extra":    map[string]any{"type": "string"},
			"services": map[string]any{"type": "string"},
		},
		"allOf": []any{
			map[string]any{"$ref": "#/$defs/base"},
			map[string]any{
				"type":        "object",
				"description": "branch docs",
				"properties": map[string]any{
					"services": map[string]any{"$ref": "service.json#/$defs/platform_schema"},
				},
			},
		},
	}
	if err := MergeAllOf(node, root); err != nil {
		t.Fatalf("MergeAllOf: %v", err)
	}
	props := node["properties"].(map[string]any)
	// Last branch wins among branches, and branches override the node's own key.
	svc := props["services"].(map[string]any)
	if got := svc["$ref"]; got != "service.json#/$defs/platform_schema" {
		t.Errorf("services = %v, want last-branch platform_schema ref", props["services"])
	}
	// Node keys with no branch collision survive.
	if _, ok := props["extra"]; !ok {
		t.Error("node-only property 'extra' lost")
	}
	// Scalar keywords: node wins.
	if got := node["description"]; got != "node docs" {
		t.Errorf("description = %v, want node docs", got)
	}
}

func TestMergeAllOfRemainingRefs(t *testing.T) {
	root := map[string]any{}
	node := map[string]any{
		"allOf": []any{
			map[string]any{"$ref": "payment_credential.json"},
			map[string]any{"$ref": "#/$defs/missing"},
			map[string]any{
				"type":       "object",
				"properties": map[string]any{"n": map[string]any{"type": "string"}},
			},
		},
	}
	if err := MergeAllOf(node, root); err != nil {
		t.Fatalf("unresolvable refs must not error: %v", err)
	}
	rem, ok := node["allOf"].([]any)
	if !ok || len(rem) != 2 {
		t.Fatalf("want slim allOf with 2 remaining refs, got %v", node["allOf"])
	}
	if rem[0].(map[string]any)["$ref"] != "payment_credential.json" {
		t.Errorf("external ref not preserved: %v", rem[0])
	}
	if _, ok := node["properties"].(map[string]any)["n"]; !ok {
		t.Error("inline branch properties lost")
	}
}

func TestMergeAllOfPolyExtractionAndDocCarry(t *testing.T) {
	root := map[string]any{}
	node := map[string]any{
		"allOf": []any{
			map[string]any{
				"title":       "Branch Title",
				"description": "branch docs",
				"oneOf":       []any{map[string]any{"type": "string"}},
			},
			map[string]any{
				"anyOf": []any{map[string]any{"type": "integer"}},
			},
		},
	}
	if err := MergeAllOf(node, root); err != nil {
		t.Fatalf("MergeAllOf: %v", err)
	}
	if got := node["title"]; got != "Branch Title" {
		t.Errorf("title = %v, want carried from branch", got)
	}
	if got := node["description"]; got != "branch docs" {
		t.Errorf("description = %v, want carried", got)
	}
	if len(node["oneOf"].([]any)) != 1 || len(node["anyOf"].([]any)) != 1 {
		t.Errorf("poly branches not extracted onto node: %v", node)
	}
}

func TestMergeAllOfDeepCopiesResolvedRefs(t *testing.T) {
	base := map[string]any{
		"type":       "object",
		"properties": map[string]any{"id": map[string]any{"type": "string"}},
	}
	root := map[string]any{"$defs": map[string]any{"base": base}}
	node := map[string]any{"allOf": []any{map[string]any{"$ref": "#/$defs/base"}}}
	if err := MergeAllOf(node, root); err != nil {
		t.Fatalf("MergeAllOf: %v", err)
	}
	node["properties"].(map[string]any)["id"].(map[string]any)["type"] = "MUTATED"
	if base["properties"].(map[string]any)["id"].(map[string]any)["type"] != "string" {
		t.Error("resolved ref was aliased, not deep-copied — mutation bled into $defs")
	}
}

func TestMergeAllOfSiblingDefRetainedRefs(t *testing.T) {
	root := map[string]any{
		"$defs": map[string]any{
			"base": map[string]any{
				"allOf": []any{
					map[string]any{"$ref": "other.json#/$defs/x"},
					map[string]any{"type": "object", "required": []any{"a"},
						"properties": map[string]any{"a": map[string]any{"type": "string"}}},
				},
			},
		},
	}
	node := map[string]any{"allOf": []any{map[string]any{"$ref": "#/$defs/base"}}}
	if err := MergeAllOf(node, root); err != nil {
		t.Fatalf("MergeAllOf: %v", err)
	}
	if _, ok := node["properties"].(map[string]any)["a"]; !ok {
		t.Error("property from sibling def lost")
	}
	if _, has := node["allOf"]; has {
		t.Errorf("branch's slim allOf must not carry onto parent: %v", node["allOf"])
	}
}

func TestMergeAllOfEmptyResolvedRefRemains(t *testing.T) {
	root := map[string]any{"$defs": map[string]any{"x": map[string]any{}}}
	node := map[string]any{"allOf": []any{map[string]any{"$ref": "#/$defs/x"}}}
	if err := MergeAllOf(node, root); err != nil {
		t.Fatalf("MergeAllOf: %v", err)
	}
	rem, ok := node["allOf"].([]any)
	if !ok || len(rem) != 1 {
		t.Fatalf("want slim allOf with 1 remaining ref for empty resolved object, got %v", node["allOf"])
	}
	if rem[0].(map[string]any)["$ref"] != "#/$defs/x" {
		t.Errorf("ref not preserved: %v", rem[0])
	}
}

func TestMergeAllOfNonObjectRefDropped(t *testing.T) {
	root := map[string]any{"$defs": map[string]any{"str": "not-an-object"}}
	node := map[string]any{
		"allOf": []any{
			map[string]any{"$ref": "#/$defs/str"},
			map[string]any{
				"type":       "object",
				"properties": map[string]any{"n": map[string]any{"type": "string"}},
			},
		},
	}
	if err := MergeAllOf(node, root); err != nil {
		t.Fatalf("MergeAllOf: %v", err)
	}
	if _, has := node["allOf"]; has {
		t.Errorf("ref resolving to a non-object must be dropped, not carried into allOf: %v", node["allOf"])
	}
	if _, ok := node["properties"].(map[string]any)["n"]; !ok {
		t.Error("sibling branch properties lost")
	}
}

func TestMergeAllOfRequiredPreservesNodeOwnList(t *testing.T) {
	root := map[string]any{}
	node := map[string]any{
		"required": []any{"b", "b", ""},
		"allOf": []any{
			map[string]any{"required": []any{"a", "b"}},
		},
	}
	if err := MergeAllOf(node, root); err != nil {
		t.Fatalf("MergeAllOf: %v", err)
	}
	got := node["required"].([]any)
	want := []any{"b", "b", "", "a"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("required = %v, want %v", got, want)
	}
}

func TestMergeAllOfRequiredUntouchedWhenNoBranchContributes(t *testing.T) {
	root := map[string]any{}
	node := map[string]any{
		"required": []any{"x", "x", ""},
		"allOf":    []any{map[string]any{"type": "object"}},
	}
	if err := MergeAllOf(node, root); err != nil {
		t.Fatalf("MergeAllOf: %v", err)
	}
	want := []any{"x", "x", ""}
	if !reflect.DeepEqual(node["required"], want) {
		t.Errorf("required = %v, want untouched %v", node["required"], want)
	}
}

func TestMergeAllOfErrors(t *testing.T) {
	tests := []struct {
		name string
		node map[string]any
	}{
		{
			name: "allOf not an array",
			node: map[string]any{
				"allOf": "not-an-array",
			},
		},
		{
			name: "branch properties not an object",
			node: map[string]any{
				"allOf": []any{
					map[string]any{"properties": "not-an-object"},
				},
			},
		},
		{
			name: "required entry not a string",
			node: map[string]any{
				"allOf": []any{
					map[string]any{"required": []any{"ok", 5}},
				},
			},
		},
	}
	root := map[string]any{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := MergeAllOf(tt.node, root); err == nil {
				t.Fatal("MergeAllOf: expected error, got nil")
			}
		})
	}
}
