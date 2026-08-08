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
