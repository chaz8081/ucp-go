package preprocess

import "testing"

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
