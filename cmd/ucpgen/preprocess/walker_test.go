package preprocess

import (
	"reflect"
	"testing"
)

func TestCopyTree(t *testing.T) {
	orig := map[string]any{
		"a": []any{map[string]any{"b": float64(1)}},
		"s": "x",
	}
	cp := CopyTree(orig).(map[string]any)
	if !reflect.DeepEqual(orig, cp) {
		t.Fatalf("copy differs: %v vs %v", orig, cp)
	}
	cp["a"].([]any)[0].(map[string]any)["b"] = float64(2)
	if orig["a"].([]any)[0].(map[string]any)["b"] != float64(1) {
		t.Error("mutating copy changed original — not a deep copy")
	}
}

func TestIterNodes(t *testing.T) {
	root := map[string]any{
		"properties": map[string]any{
			"x": map[string]any{"type": "string"},
		},
		"list": []any{map[string]any{"deep": true}},
	}
	var dicts int
	for _, n := range IterNodes(root) {
		if _, ok := n.(map[string]any); ok {
			dicts++
		}
	}
	// root, properties, x, deep-holder = 4 dict nodes (the []any list node is not a dict)
	if dicts != 4 {
		t.Errorf("dict nodes = %d, want 4", dicts)
	}
}

func TestIterNodesDeterministicOrder(t *testing.T) {
	root := map[string]any{
		"alpha":   map[string]any{"z": true},
		"beta":    map[string]any{"y": true},
		"gamma":   map[string]any{"x": true},
		"delta":   map[string]any{"w": true},
		"epsilon": map[string]any{"v": true},
	}
	first := IterNodes(root)
	for i := 0; i < 20; i++ {
		got := IterNodes(root)
		if !reflect.DeepEqual(got, first) {
			t.Fatalf("run %d: IterNodes order is nondeterministic:\n first=%v\n got=%v", i, first, got)
		}
	}
}

func TestSchemaSetPaths(t *testing.T) {
	set := &SchemaSet{Files: map[string]map[string]any{
		"b.json": {}, "a/c.json": {}, "a.json": {},
	}}
	got := set.Paths()
	want := []string{"a.json", "a/c.json", "b.json"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Paths() = %v, want %v", got, want)
	}
}
