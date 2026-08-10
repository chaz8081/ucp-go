package preprocess

import (
	"reflect"
	"sort"
	"testing"
)

func TestDistributeToBranches(t *testing.T) {
	node := map[string]any{
		"type":     "object",
		"required": []any{"kind"},
		"properties": map[string]any{
			"kind": map[string]any{"type": "string"},
		},
		"oneOf": []any{
			map[string]any{
				"required": []any{"card"},
				"properties": map[string]any{
					"card": map[string]any{"type": "string"},
					"kind": map[string]any{"const": "card"},
				},
			},
		},
	}
	DistributeToBranches(node)
	branch := node["oneOf"].([]any)[0].(map[string]any)
	props := branch["properties"].(map[string]any)
	// Base property inherited; branch override wins for colliding key.
	if props["kind"].(map[string]any)["const"] != "card" {
		t.Errorf("branch property must override base: %v", props["kind"])
	}
	if _, ok := props["card"]; !ok {
		t.Error("branch-own property lost")
	}
	var req []string
	for _, r := range branch["required"].([]any) {
		req = append(req, r.(string))
	}
	sort.Strings(req)
	if !reflect.DeepEqual(req, []string{"card", "kind"}) {
		t.Errorf("required = %v, want union [card kind]", req)
	}
	if branch["type"] != "object" {
		t.Errorf("branch must inherit base type, got %v", branch["type"])
	}
	// Base node's own properties untouched (branches got copies).
	if _, has := node["properties"].(map[string]any)["card"]; has {
		t.Error("distribution mutated the base node's properties")
	}
}

func TestDistributeToBranchesTypeArrayNotAliased(t *testing.T) {
	node := map[string]any{
		"type": []any{"object", "null"},
		"properties": map[string]any{
			"kind": map[string]any{"type": "string"},
		},
		"oneOf": []any{
			map[string]any{"properties": map[string]any{"a": map[string]any{"type": "string"}}},
			map[string]any{"properties": map[string]any{"b": map[string]any{"type": "string"}}},
		},
	}
	DistributeToBranches(node)
	branches := node["oneOf"].([]any)
	t0 := branches[0].(map[string]any)["type"].([]any)
	t1 := branches[1].(map[string]any)["type"].([]any)
	t0[0] = "MUTATED"
	if t1[0] == "MUTATED" {
		t.Error("branch type slices are aliased — mutating one branch changed another")
	}
	baseType := node["type"].([]any)
	if baseType[0] == "MUTATED" {
		t.Error("branch type slice aliased the node's own base type")
	}
}
