package emit

import (
	"strings"
	"testing"
)

// C1 — non-object top-level schemas must error, not silently emit an empty
// struct that accepts anything.

func TestEmitFileRejectsNonObjectTopLevel(t *testing.T) {
	schema := map[string]any{
		"title":   "ReverseDomainName",
		"type":    "string",
		"pattern": "^[a-z]+$",
	}
	_, err := EmitFile("shopping", "types/reverse_domain_name.json", schema, "release/test@deadbeef")
	if err == nil {
		t.Fatalf("EmitFile: expected error for non-object top-level type, got nil")
	}
	if !strings.Contains(err.Error(), "top-level type") {
		t.Errorf("error = %q, want mention of top-level type", err.Error())
	}
}

func TestEmitFileRejectsMissingTypeAndProperties(t *testing.T) {
	schema := map[string]any{
		"title": "Empty",
	}
	_, err := EmitFile("shopping", "types/empty.json", schema, "release/test@deadbeef")
	if err == nil {
		t.Fatalf("EmitFile: expected error for missing type and properties, got nil")
	}
	if !strings.Contains(err.Error(), "top-level type") {
		t.Errorf("error = %q, want mention of top-level type", err.Error())
	}
}

func TestEmitFileAllowsImplicitObjectType(t *testing.T) {
	schema := map[string]any{
		"title": "Implicit",
		"properties": map[string]any{
			"name": map[string]any{"type": "string"},
		},
	}
	src, err := EmitFile("shopping", "types/implicit.json", schema, "release/test@deadbeef")
	if err != nil {
		t.Fatalf("EmitFile: %v", err)
	}
	if !strings.Contains(src, "type Implicit struct {") {
		t.Errorf("missing struct decl\n%s", src)
	}
}

// C2 — wrong-typed constraint values must error, not vanish.

func TestEmitFileRejectsMaxLengthWrongType(t *testing.T) {
	schema := map[string]any{
		"title": "BadMaxLength",
		"type":  "object",
		"properties": map[string]any{
			"name": map[string]any{"type": "string", "maxLength": 5}, // int, not float64
		},
	}
	_, err := EmitFile("shopping", "test/badmaxlength.json", schema, "release/test@deadbeef")
	if err == nil {
		t.Fatalf("EmitFile: expected error for non-numeric maxLength, got nil")
	}
	if !strings.Contains(err.Error(), "maxLength") {
		t.Errorf("error = %q, want mention of maxLength", err.Error())
	}
}

func TestEmitFileRejectsPatternWrongType(t *testing.T) {
	schema := map[string]any{
		"title": "BadPatternType",
		"type":  "object",
		"properties": map[string]any{
			"name": map[string]any{"type": "string", "pattern": 123},
		},
	}
	_, err := EmitFile("shopping", "test/badpatterntype.json", schema, "release/test@deadbeef")
	if err == nil {
		t.Fatalf("EmitFile: expected error for non-string pattern, got nil")
	}
	if !strings.Contains(err.Error(), "pattern") {
		t.Errorf("error = %q, want mention of pattern", err.Error())
	}
}

func TestEmitFileRejectsBooleanRequired(t *testing.T) {
	schema := map[string]any{
		"title":    "OpenRPCParam",
		"type":     "object",
		"required": true,
		"properties": map[string]any{
			"name": map[string]any{"type": "string"},
		},
	}
	_, err := EmitFile("shopping", "test/openrpcparam.json", schema, "release/test@deadbeef")
	if err == nil {
		t.Fatalf("EmitFile: expected error for boolean top-level required, got nil")
	}
	if !strings.Contains(err.Error(), "required") {
		t.Errorf("error = %q, want mention of required", err.Error())
	}
}

// C3 — skipped property types carrying string constraints must error.

func TestEmitFileRejectsStringConstraintsOnUnsupportedType(t *testing.T) {
	schema := map[string]any{
		"title": "FulfillmentAvailableMethod",
		"type":  "object",
		"properties": map[string]any{
			"method": map[string]any{"type": []any{"string", "null"}, "pattern": "^[a-z]+$"},
		},
	}
	_, err := EmitFile("shopping", "test/fulfillment_available_method.json", schema, "release/test@deadbeef")
	if err == nil {
		t.Fatalf("EmitFile: expected error for string constraints on unsupported type, got nil")
	}
	if !strings.Contains(err.Error(), "unsupported type") {
		t.Errorf("error = %q, want mention of unsupported type", err.Error())
	}
}

func TestEmitFileRejectsPropertiesWrongType(t *testing.T) {
	schema := map[string]any{
		"title":      "BadProperties",
		"type":       "object",
		"properties": 123, // not a map[string]any
	}
	_, err := EmitFile("shopping", "test/badproperties.json", schema, "release/test@deadbeef")
	if err == nil {
		t.Fatalf("EmitFile: expected error for non-object properties, got nil")
	}
	if !strings.Contains(err.Error(), "properties") {
		t.Errorf("error = %q, want mention of properties", err.Error())
	}
}

// I5 — collision detection.

func TestEmitFileRejectsFieldCollision(t *testing.T) {
	schema := map[string]any{
		"title": "Collision",
		"type":  "object",
		"properties": map[string]any{
			"line_item": map[string]any{"type": "string"},
			"line-item": map[string]any{"type": "string"},
		},
	}
	_, err := EmitFile("shopping", "test/collision.json", schema, "release/test@deadbeef")
	if err == nil {
		t.Fatalf("EmitFile: expected error for field name collision, got nil")
	}
	// Must be caught by explicit collision detection, not incidentally by a
	// downstream gofmt parse failure on the (also invalid) generated code.
	if strings.Contains(err.Error(), "does not parse") {
		t.Fatalf("error = %q, collision must be caught before source generation, not via a parse failure", err.Error())
	}
	if !strings.Contains(err.Error(), "LineItem") {
		t.Errorf("error = %q, want mention of colliding field name LineItem", err.Error())
	}
}

func TestEmitFileRejectsValidateNameCollision(t *testing.T) {
	schema := map[string]any{
		"title": "ValidateCollision",
		"type":  "object",
		"properties": map[string]any{
			"validate": map[string]any{"type": "string"},
		},
	}
	_, err := EmitFile("shopping", "test/validatecollision.json", schema, "release/test@deadbeef")
	if err == nil {
		t.Fatalf("EmitFile: expected error for Validate name collision, got nil")
	}
	if !strings.Contains(err.Error(), "Validate") {
		t.Errorf("error = %q, want mention of Validate collision", err.Error())
	}
}

// I6 — GoName sanitization via EmitFile's title path.

func TestEmitFileSanitizesParentheticalTitle(t *testing.T) {
	schema := map[string]any{
		"title": "Capability (Business Schema)",
		"type":  "object",
		"properties": map[string]any{
			"name": map[string]any{"type": "string"},
		},
	}
	src, err := EmitFile("shopping", "test/capability.json", schema, "release/test@deadbeef")
	if err != nil {
		t.Fatalf("EmitFile: %v", err)
	}
	if !strings.Contains(src, "type CapabilityBusinessSchema struct {") {
		t.Errorf("missing type decl\n%s", src)
	}
}

// I7 — Validate() is always emitted, even with zero checks.

func TestEmitFileAlwaysEmitsValidate(t *testing.T) {
	schema := map[string]any{
		"title": "No Constraints",
		"type":  "object",
		"properties": map[string]any{
			"name": map[string]any{"type": "string"},
		},
	}
	src, err := EmitFile("shopping", "test/noconstraints.json", schema, "release/test@deadbeef")
	if err != nil {
		t.Fatalf("EmitFile: %v", err)
	}
	for _, want := range []string{
		"func (v *NoConstraints) Validate() error {",
		"return nil",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("missing %q\n%s", want, src)
		}
	}
	for _, unwanted := range []string{`"fmt"`, `"errors"`} {
		if strings.Contains(src, unwanted) {
			t.Errorf("unconstrained schema should not import %s\n%s", unwanted, src)
		}
	}
}

// I8 — multi-line descriptions are escaped into valid comment continuations.

func TestEmitFileEscapesMultilineDescription(t *testing.T) {
	schema := map[string]any{
		"title":       "Multi Line",
		"description": "Line one.\nLine two.",
		"type":        "object",
		"properties": map[string]any{
			"name": map[string]any{"type": "string", "description": "Field line one.\nField line two."},
		},
	}
	src, err := EmitFile("shopping", "test/multiline.json", schema, "release/test@deadbeef")
	if err != nil {
		t.Fatalf("EmitFile: %v", err)
	}
	for _, want := range []string{
		"// MultiLine Line one.",
		"// Line two.",
		"// Field line one.",
		"// Field line two.",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("missing %q\n%s", want, src)
		}
	}
}
