package emit

import (
	"strings"
	"testing"
)

// C1 — non-object top-level schemas must error, not silently emit an empty
// struct that accepts anything.

// A scalar top-level schema is a named alias type as of phase 3; it used
// to be rejected outright.
func TestEmitFileScalarTopLevelEmitsAlias(t *testing.T) {
	schema := map[string]any{
		"title": "Reverse Domain Name",
		"type":  "string",
	}
	src, err := emitOne(t, "types/reverse_domain_name.json", schema)
	if err != nil {
		t.Fatalf("emitOne: %v", err)
	}
	if !strings.Contains(collapse(src), "type ReverseDomainName string") {
		t.Errorf("scalar schema should emit a named alias:\n%s", src)
	}
}

func TestEmitFileRejectsMissingTypeAndProperties(t *testing.T) {
	schema := map[string]any{
		"title": "Empty",
	}
	_, err := emitOne(t, "types/empty.json", schema)
	if err == nil {
		t.Fatalf("EmitFile: expected error for missing type and properties, got nil")
	}
	if !strings.Contains(err.Error(), "nothing to emit") {
		t.Errorf("error = %q, want mention of an empty schema", err.Error())
	}
}

func TestEmitFileAllowsImplicitObjectType(t *testing.T) {
	schema := map[string]any{
		"title": "Implicit",
		"properties": map[string]any{
			"name": map[string]any{"type": "string"},
		},
	}
	src, err := emitOne(t, "types/implicit.json", schema)
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
	_, err := emitOne(t, "test/badmaxlength.json", schema)
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
	_, err := emitOne(t, "test/badpatterntype.json", schema)
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
	_, err := emitOne(t, "test/openrpcparam.json", schema)
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
	_, err := emitOne(t, "test/fulfillment_available_method.json", schema)
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
	_, err := emitOne(t, "test/badproperties.json", schema)
	if err == nil {
		t.Fatalf("EmitFile: expected error for non-object properties, got nil")
	}
	if !strings.Contains(err.Error(), "properties") {
		t.Errorf("error = %q, want mention of properties", err.Error())
	}
}

// C5 — known-but-unimplemented JSON Schema assertion keywords must fail
// generation loudly rather than silently vanishing from Validate().

// additionalProperties in its schema form is now a map value type.
func TestEmitFileAdditionalPropertiesSchemaFormBecomesMap(t *testing.T) {
	schema := map[string]any{
		"title": "Has AP",
		"type":  "object",
		"properties": map[string]any{
			"tags": map[string]any{
				"type":                 "object",
				"additionalProperties": map[string]any{"type": "string"},
			},
		},
	}
	src, err := emitOne(t, "test/hasap.json", schema)
	if err != nil {
		t.Fatalf("emitOne: %v", err)
	}
	if !strings.Contains(collapse(src), "Tags map[string]string") {
		t.Errorf("additionalProperties schema form should emit a map:\n%s", src)
	}
}
func TestEmitFileAllowsAdditionalPropertiesBooleanForm(t *testing.T) {
	schema := map[string]any{
		"title":                "HasAPBool",
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"name": map[string]any{"type": "string"},
		},
	}
	if _, err := emitOne(t, "test/hasapbool.json", schema); err != nil {
		t.Fatalf("EmitFile: unexpected error for boolean additionalProperties: %v", err)
	}
}

func TestEmitFileAllowsFormatAnnotation(t *testing.T) {
	// format is annotation-only in draft-2020-12 (assertion behavior needs
	// an opt-in vocabulary the spec doesn't enable), and the conformance
	// oracle runs with assertFormat off — so format must NOT error.
	schema := map[string]any{
		"title": "Has Format",
		"type":  "object",
		"properties": map[string]any{
			"when": map[string]any{"type": "string", "format": "date-time"},
		},
	}
	src, err := emitOne(t, "test/hasformat.json", schema)
	if err != nil {
		t.Fatalf("EmitFile: unexpected error for format-only schema: %v", err)
	}
	if !strings.Contains(src, "type HasFormat struct {") {
		t.Errorf("missing struct decl\n%s", src)
	}
}

// C6 — maxLength must be a non-negative, integer-valued number.

func TestEmitFileRejectsNegativeMaxLength(t *testing.T) {
	schema := map[string]any{
		"title": "NegativeMaxLength",
		"type":  "object",
		"properties": map[string]any{
			"name": map[string]any{"type": "string", "maxLength": float64(-1)},
		},
	}
	_, err := emitOne(t, "test/negativemaxlength.json", schema)
	if err == nil {
		t.Fatalf("EmitFile: expected error for negative maxLength, got nil")
	}
	if !strings.Contains(err.Error(), "maxLength") {
		t.Errorf("error = %q, want mention of maxLength", err.Error())
	}
}

func TestEmitFileRejectsFractionalMaxLength(t *testing.T) {
	schema := map[string]any{
		"title": "FractionalMaxLength",
		"type":  "object",
		"properties": map[string]any{
			"name": map[string]any{"type": "string", "maxLength": float64(2.5)},
		},
	}
	_, err := emitOne(t, "test/fractionalmaxlength.json", schema)
	if err == nil {
		t.Fatalf("EmitFile: expected error for fractional maxLength, got nil")
	}
	if !strings.Contains(err.Error(), "maxLength") {
		t.Errorf("error = %q, want mention of maxLength", err.Error())
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
	_, err := emitOne(t, "test/collision.json", schema)
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
	_, err := emitOne(t, "test/validatecollision.json", schema)
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
	src, err := emitOne(t, "test/capability.json", schema)
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
	src, err := emitOne(t, "test/noconstraints.json", schema)
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
	if strings.Contains(src, `"fmt"`) {
		t.Errorf("unconstrained schema should not import \"fmt\"\n%s", src)
	}
	// "errors" IS imported now, and used: every generated decoder opens by
	// rejecting a bare null, since encoding/json would otherwise accept one
	// as a no-op and leave the zero value to pass every check.
	if !strings.Contains(collapse(src), `if string(data) == "null" {`) {
		t.Errorf("decoder is missing its null guard\n%s", src)
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
	src, err := emitOne(t, "test/multiline.json", schema)
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
