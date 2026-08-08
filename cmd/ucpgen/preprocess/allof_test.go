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
