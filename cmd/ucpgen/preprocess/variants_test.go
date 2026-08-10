package preprocess

import (
	"reflect"
	"sort"
	"testing"
)

func TestRequiredOps(t *testing.T) {
	schema := map[string]any{
		"properties": map[string]any{
			"a": map[string]any{"ucp_request": "required"},
			"b": map[string]any{"ucp_request": map[string]any{"complete": "required", "update": "omit"}},
			"c": map[string]any{"type": "string"},
		},
	}
	got := RequiredOps(schema)
	sort.Strings(got)
	// string marker -> create+update (:386-388); dict marker -> its keys (:389-390)
	want := []string{"complete", "create", "update"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("RequiredOps = %v, want %v", got, want)
	}
	if got := RequiredOps(map[string]any{}); len(got) != 0 {
		t.Errorf("no markers -> no ops, got %v", got)
	}
}

func TestEvalPropInclusion(t *testing.T) {
	baseReq := []any{"id"}
	cases := []struct {
		name     string
		propName string
		data     map[string]any
		op       string
		include  bool
		required bool
	}{
		{"no marker, not in base required", "x", map[string]any{}, "create", true, false},
		{"nil data, in base required", "id", nil, "create", true, true},
		{"omit string", "x", map[string]any{"ucp_request": "omit"}, "create", false, false},
		{"required string", "x", map[string]any{"ucp_request": "required"}, "create", true, true},
		{"optional string overrides base", "id", map[string]any{"ucp_request": "optional"}, "create", true, false},
		{"dict op required", "x", map[string]any{"ucp_request": map[string]any{"create": "required"}}, "create", true, true},
		{"dict op optional", "id", map[string]any{"ucp_request": map[string]any{"create": "optional"}}, "create", true, false},
		{"dict op omit", "x", map[string]any{"ucp_request": map[string]any{"create": "omit"}}, "create", false, false},
		{"dict missing op key -> omit", "x", map[string]any{"ucp_request": map[string]any{"update": "required"}}, "create", false, false},
		{"dict unknown value includes", "x", map[string]any{"ucp_request": map[string]any{"create": "banana"}}, "create", true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			inc, req := EvalPropInclusion(tc.propName, tc.data, tc.op, baseReq)
			if inc != tc.include || req != tc.required {
				t.Errorf("got (include=%v, required=%v), want (%v, %v)", inc, req, tc.include, tc.required)
			}
		})
	}
}
