package preprocess

import (
	"encoding/json"
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

func TestPreprocessDocumentNestedDefs(t *testing.T) {
	schema := map[string]any{
		"title": "Wrapper",
		"$defs": map[string]any{
			"inner": map[string]any{
				"allOf": []any{
					map[string]any{
						"type":       "object",
						"required":   []any{"a"},
						"properties": map[string]any{"a": map[string]any{"type": "string"}},
					},
					map[string]any{
						"required":   []any{"b"},
						"properties": map[string]any{"b": map[string]any{"type": "string"}},
					},
				},
			},
		},
	}
	if err := PreprocessDocument(schema, nil); err != nil {
		t.Fatalf("PreprocessDocument: %v", err)
	}
	inner := schema["$defs"].(map[string]any)["inner"].(map[string]any)
	if _, has := inner["allOf"]; has {
		t.Error("nested $defs allOf not merged — document walk missing it")
	}
	props := inner["properties"].(map[string]any)
	if len(props) != 2 {
		t.Errorf("merged properties = %v, want a+b", props)
	}
}

func TestPreprocessDocumentEntityFlattening(t *testing.T) {
	entity := map[string]any{
		"title":       "Entity",
		"description": "base entity",
		"type":        "object",
		"properties":  map[string]any{"id": map[string]any{"type": "string"}},
	}
	schema := map[string]any{
		"title": "Thing",
		"allOf": []any{
			map[string]any{"$ref": "ucp.json#/$defs/entity"},
			map[string]any{"required": []any{"id"}},
		},
	}
	if err := PreprocessDocument(schema, entity); err != nil {
		t.Fatalf("PreprocessDocument: %v", err)
	}
	if _, has := schema["allOf"]; has {
		t.Errorf("entity ref should be inlined and merged, allOf remains: %v", schema["allOf"])
	}
	if _, ok := schema["properties"].(map[string]any)["id"]; !ok {
		t.Error("entity properties not inlined")
	}
	// Entity title/description are stripped before inlining (:246-247),
	// so the node's own title survives untouched.
	if schema["title"] != "Thing" {
		t.Errorf("title = %v, want Thing", schema["title"])
	}
	// Entity def itself must not be mutated.
	if _, has := entity["required"]; has {
		t.Error("shared entity definition mutated")
	}
}

func TestPreprocessDocumentDeterministic(t *testing.T) {
	build := func() map[string]any {
		return map[string]any{
			"title": "Svc",
			"$defs": map[string]any{
				"base": map[string]any{
					"allOf": []any{
						map[string]any{"$ref": "ucp.json#/$defs/entity"},
						map[string]any{"type": "object", "required": []any{"id"},
							"properties": map[string]any{"id": map[string]any{"type": "string"}}},
					},
				},
				"platform_schema": map[string]any{
					"allOf": []any{map[string]any{"$ref": "#/$defs/base"}},
				},
				"business_schema": map[string]any{
					"allOf": []any{map[string]any{"$ref": "#/$defs/base"}}},
			},
		}
	}
	entity := map[string]any{
		"title": "Entity", "type": "object",
		"properties": map[string]any{"version": map[string]any{"type": "string"}},
	}
	first := ""
	for i := 0; i < 20; i++ {
		s := build()
		if err := PreprocessDocument(s, entity); err != nil {
			t.Fatalf("run %d: %v", i, err)
		}
		raw, err := json.Marshal(s)
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			first = string(raw)
			continue
		}
		if string(raw) != first {
			t.Fatalf("run %d differs from run 0:\n%s\n---\n%s", i, raw, first)
		}
	}
	// And the outcome is the FLATTENED one: platform_schema must have
	// inherited version+id, with no dangling allOf.
	s := build()
	if err := PreprocessDocument(s, entity); err != nil {
		t.Fatal(err)
	}
	ps := s["$defs"].(map[string]any)["platform_schema"].(map[string]any)
	if _, has := ps["allOf"]; has {
		t.Errorf("platform_schema kept dangling allOf: %v", ps["allOf"])
	}
	props := ps["properties"].(map[string]any)
	for _, want := range []string{"version", "id"} {
		if _, ok := props[want]; !ok {
			t.Errorf("platform_schema missing inherited %q; has %v", want, props)
		}
	}
}
