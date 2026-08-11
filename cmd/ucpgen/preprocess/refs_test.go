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
