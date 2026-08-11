package emit

import (
	"reflect"
	"testing"
)

func TestFindCycle(t *testing.T) {
	cases := []struct {
		name  string
		graph PackageGraph
		want  bool
	}{
		{"acyclic", PackageGraph{"a": {"b": true}, "b": {"c": true}, "c": {}}, false},
		{"two cycle", PackageGraph{"a": {"b": true}, "b": {"a": true}}, true},
		{"three cycle", PackageGraph{"a": {"b": true}, "b": {"c": true}, "c": {"a": true}}, true},
		{"self edge", PackageGraph{"a": {"a": true}}, true},
		{"empty", PackageGraph{}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srcs := make([]string, 0, len(c.graph))
			for s := range c.graph {
				srcs = append(srcs, s)
			}
			got := findCycle(c.graph, srcs) != nil
			if got != c.want {
				t.Errorf("findCycle = %v, want %v", got, c.want)
			}
		})
	}
}

func TestCycleBreaksPrefersDeeperSource(t *testing.T) {
	// One file per package fixes each package's depth: root is depth 0,
	// shopping/types is depth 2, so the edge out of the deeper package is
	// the one that gets cut.
	files := map[string]map[string]any{
		"ucp.json":                      {},
		"shopping/types/line_item.json": {},
	}
	g := PackageGraph{
		"m":                {"m/shopping/types": true},
		"m/shopping/types": {"m": true},
	}
	breaks := CycleBreaks(g, files, "m")
	if !breaks["m/shopping/types"]["m"] {
		t.Errorf("expected the deeper package's edge to be broken, got %v", breaks)
	}
	if breaks["m"]["m/shopping/types"] {
		t.Errorf("the shallower package's edge must survive: %v", breaks)
	}
}

func TestCycleBreaksResolvesEveryCycle(t *testing.T) {
	// Two disjoint cycles plus a three-cycle: the loop must keep going
	// until the graph is acyclic, not stop at the first break.
	files := map[string]map[string]any{
		"a.json": {}, "b/x.json": {}, "c/y.json": {}, "d/z.json": {},
	}
	g := PackageGraph{
		"m":   {"m/b": true},
		"m/b": {"m": true},
		"m/c": {"m/d": true},
		"m/d": {"m/c": true},
	}
	breaks := CycleBreaks(g, files, "m")
	if len(breaks) == 0 {
		t.Fatal("expected breaks for two disjoint cycles")
	}
	if findCycle(g, []string{"m", "m/b", "m/c", "m/d"}) != nil {
		t.Error("graph still contains a cycle after CycleBreaks")
	}
}

func TestCollectRefsSkipsNonTypeProducingPositions(t *testing.T) {
	// A $ref reachable only through propertyNames or if/then produces no Go
	// type, so it must not appear as a package edge — a phantom edge that
	// closed a cycle would degrade a real, typeable field to raw JSON.
	schema := map[string]any{
		"properties": map[string]any{
			"real": map[string]any{"$ref": "types/line_item.json"},
			"keys": map[string]any{
				"type":          "object",
				"propertyNames": map[string]any{"$ref": "types/reverse_domain_name.json"},
			},
		},
		"if":   map[string]any{"$ref": "types/conditional.json"},
		"then": map[string]any{"$ref": "types/branch.json"},
	}
	got := collectRefs(schema)
	want := []string{"types/line_item.json"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("collectRefs = %v, want %v", got, want)
	}
}
