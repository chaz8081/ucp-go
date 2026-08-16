package emit

import (
	"strings"
	"testing"
)

// The decoder is this SDK's JSON type check: a string payload fails to
// decode into an int64, and that decode failure IS the rejection. `null` is
// the one input encoding/json lets through for every type, so without a
// decoder of its own a named scalar accepted a null document and validated
// it as though it were a real value.
func TestEmitScalarAliasRejectsNull(t *testing.T) {
	schema := map[string]any{
		"title": "Amount", "type": "integer", "minimum": float64(0),
	}
	src, err := emitOne(t, "test/amount.json", schema)
	if err != nil {
		t.Fatalf("EmitFile: %v", err)
	}
	for _, want := range []string{
		"func (v *Amount) UnmarshalJSON(data []byte) error {",
		`if string(data) == "null" {`,
		`return errors.New("Amount: null is not a valid integer")`,
		"type AmountAlias Amount",
		"*v = Amount(alias)",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("missing %q in:\n%s", want, src)
		}
	}
}

// An array root has the same hole, and Totals made it worse: its contains
// count is guarded on the slice being non-nil, so a null document skipped
// the only rule the type exists for.
func TestEmitArrayAliasRejectsNull(t *testing.T) {
	schema := map[string]any{
		"title": "Totals", "type": "array",
		"items": map[string]any{"type": "string"},
	}
	src, err := emitOne(t, "test/totals.json", schema)
	if err != nil {
		t.Fatalf("EmitFile: %v", err)
	}
	for _, want := range []string{
		"func (v *Totals) UnmarshalJSON(data []byte) error {",
		`if string(data) == "null" {`,
		`return errors.New("Totals: null is not a valid array")`,
		"type TotalsAlias Totals",
		"*v = Totals(alias)",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("missing %q in:\n%s", want, src)
		}
	}
}

// A schema that permits null must keep accepting it. The corpus has no
// such named type — goTypeExpr renders `["string","null"]` as *string, and
// Go forbids a method on a defined pointer type, so the generated Validate
// would not compile — but the rule the codec is selected by has to state
// the exemption regardless, or a spec release that adds one ships a type
// that rejects the values it was written to allow.
func TestNullCodecSkipsTypesThatPermitNull(t *testing.T) {
	for _, underlying := range []string{"*string", "*int64", "*float64"} {
		if needsNullCodec(underlying) {
			t.Errorf("%s permits null and must not get a null-rejecting decoder", underlying)
		}
	}
}

// Exactly one UnmarshalJSON per type or the package does not compile. The
// codec is emitted only on the alias path, which is the one branch of
// renderNamedType that writes no decoder of its own; these pin that the
// other branches are untouched and that the non-scalar, non-array aliases
// stay out of scope.
func TestNullCodecScope(t *testing.T) {
	for _, underlying := range []string{"string", "int64", "float64", "bool", "[]Total", "[]any"} {
		if !needsNullCodec(underlying) {
			t.Errorf("%s is a scalar or array root and must reject a bare null", underlying)
		}
	}
	// A $ref root aliases another named type, and a property-less object
	// root becomes a map. Neither is in this fix's scope.
	for _, underlying := range []string{"types.Fulfillment", "map[string]any", "map[string]string", "json.RawMessage"} {
		if needsNullCodec(underlying) {
			t.Errorf("%s is out of scope and must not get a null-rejecting decoder", underlying)
		}
	}
}

// A struct root already generates an UnmarshalJSON for presence tracking,
// and a second one would not compile.
func TestEmitStructKeepsExactlyOneDecoder(t *testing.T) {
	src, err := emitOne(t, "test/thing.json", requiredFixture())
	if err != nil {
		t.Fatalf("EmitFile: %v", err)
	}
	if n := strings.Count(src, "func (v *Thing) UnmarshalJSON("); n != 1 {
		t.Errorf("want exactly 1 UnmarshalJSON for Thing, got %d:\n%s", n, src)
	}
	if strings.Contains(src, `null is not a valid`) {
		t.Errorf("an object root rejects null through its required-property check, not a second decoder:\n%s", src)
	}
}
