package emit

import (
	"strings"
	"testing"
)

func TestCompileConstraintsStringChecks(t *testing.T) {
	e := newFileEmitter(idxFixture(t), "shopping/checkout.json", "shopping")
	c := &constraintSet{}
	node := map[string]any{"type": "string", "maxLength": float64(8), "pattern": "^[a-z]+$"}
	if err := compileConstraints(e, c, "Thing", "name", "v.Name", accessValue, node); err != nil {
		t.Fatalf("compileConstraints: %v", err)
	}
	got := c.checks.String()
	for _, want := range []string{
		"utf8.RuneCountInString(string(v.Name)) > 8",
		"pattern_Thing_Name().MatchString(string(v.Name))",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

func TestCompileConstraintsOptionalGuardsNil(t *testing.T) {
	e := newFileEmitter(idxFixture(t), "shopping/checkout.json", "shopping")
	c := &constraintSet{}
	node := map[string]any{"type": "string", "maxLength": float64(8)}
	if err := compileConstraints(e, c, "Thing", "name", "v.Name", accessPointer, node); err != nil {
		t.Fatalf("compileConstraints: %v", err)
	}
	got := c.checks.String()
	if !strings.Contains(got, "v.Name != nil") {
		t.Errorf("optional field check must be nil-guarded:\n%s", got)
	}
	if !strings.Contains(got, "*v.Name") {
		t.Errorf("optional field check must dereference:\n%s", got)
	}
}

// TestCompileConstraintsRejectsConstrainedNonString keeps the emitter
// honest: a string constraint on a non-string shape must fail generation
// rather than emit a field that silently enforces nothing.
func TestCompileConstraintsRejectsConstrainedNonString(t *testing.T) {
	e := newFileEmitter(idxFixture(t), "shopping/checkout.json", "shopping")
	c := &constraintSet{}
	node := map[string]any{"type": "integer", "maxLength": float64(8)}
	err := compileConstraints(e, c, "Thing", "count", "v.Count", accessValue, node)
	if err == nil {
		t.Fatal("maxLength on an integer must fail generation")
	}
	if !strings.Contains(err.Error(), "count") {
		t.Errorf("error should name the property: %v", err)
	}
}

// compile is the shared driver for the per-keyword tests below: it runs one
// schema node through the compiler and returns the generated variables and
// checks together, which is what every assertion looks at.
func compile(t *testing.T, node map[string]any, expr string, access accessKind) string {
	t.Helper()
	e := newFileEmitter(idxFixture(t), "shopping/checkout.json", "shopping")
	c := &constraintSet{}
	if err := compileConstraints(e, c, "Thing", "field", expr, access, node); err != nil {
		t.Fatalf("compileConstraints: %v", err)
	}
	return c.vars.String() + c.checks.String()
}

func wants(t *testing.T, got string, want ...string) {
	t.Helper()
	for _, w := range want {
		if !strings.Contains(got, w) {
			t.Errorf("missing %q in:\n%s", w, got)
		}
	}
}

func TestCompileConstraintsEnumAndConst(t *testing.T) {
	wants(t, compile(t, map[string]any{
		"type": "string", "enum": []any{"a", "b"},
	}, "v.Field", accessValue),
		`v.Field != "a" && v.Field != "b"`,
		`field: not one of the permitted values`)

	// An optional field's enum check must not fire on an absent value.
	wants(t, compile(t, map[string]any{
		"type": "string", "enum": []any{"a", "b"},
	}, "v.Field", accessPointer),
		`v.Field != nil && *v.Field != "a" && *v.Field != "b"`)

	wants(t, compile(t, map[string]any{
		"type": "string", "const": "fixed",
	}, "v.Field", accessValue),
		`v.Field != "fixed"`,
		`field: must be \"fixed\"`)

	// The type is implied by the const value when the schema omits it,
	// exactly as goTypeExpr infers the Go type.
	wants(t, compile(t, map[string]any{"const": true}, "v.Field", accessValue),
		`v.Field != true`)
}

func TestCompileConstraintsNumericBounds(t *testing.T) {
	// An integer field takes an integer literal: `v.Field < 1.0` would not
	// even compile against an int64.
	wants(t, compile(t, map[string]any{
		"type": "integer", "minimum": float64(1),
	}, "v.Field", accessValue), `v.Field < 1 {`, `field: below minimum 1`)

	wants(t, compile(t, map[string]any{
		"type": "number", "minimum": float64(0), "exclusiveMaximum": float64(1),
	}, "v.Field", accessValue), `v.Field < 0 {`, `v.Field >= 1 {`)

	wants(t, compile(t, map[string]any{
		"type": "integer", "maximum": float64(10), "exclusiveMinimum": float64(0),
	}, "v.Field", accessValue), `v.Field > 10 {`, `v.Field <= 0 {`)

	wants(t, compile(t, map[string]any{
		"type": "integer", "multipleOf": float64(2),
	}, "v.Field", accessValue), `v.Field%2 != 0`)

	wants(t, compile(t, map[string]any{
		"type": "integer", "minimum": float64(1),
	}, "v.Field", accessPointer), `v.Field != nil && *v.Field < 1`)
}

// TestCompileConstraintsRejectsFractionalIntegerBound catches a bound that
// cannot be written as an integer literal against an int64 field.
func TestCompileConstraintsRejectsFractionalIntegerBound(t *testing.T) {
	e := newFileEmitter(idxFixture(t), "shopping/checkout.json", "shopping")
	c := &constraintSet{}
	node := map[string]any{"type": "integer", "minimum": 1.5}
	if err := compileConstraints(e, c, "Thing", "field", "v.Field", accessValue, node); err == nil {
		t.Fatal("a fractional bound on an integer field must fail generation")
	}
}

func TestCompileConstraintsStringLength(t *testing.T) {
	wants(t, compile(t, map[string]any{
		"type": "string", "minLength": float64(2),
	}, "v.Field", accessValue), `utf8.RuneCountInString(string(v.Field)) < 2`)
}

func TestCompileConstraintsArrayChecks(t *testing.T) {
	got := compile(t, map[string]any{
		"type": "array", "minItems": float64(1), "maxItems": float64(5),
		"items": map[string]any{"type": "string"},
	}, "v.Field", accessValue)
	wants(t, got, `len(v.Field) < 1`, `len(v.Field) > 5`)

	// A comparable element type needs no marshaling to detect duplicates.
	got = compile(t, map[string]any{
		"type": "array", "uniqueItems": true,
		"items": map[string]any{"type": "string"},
	}, "v.Field", accessValue)
	wants(t, got, `map[string]bool`, `range v.Field`, `field: contains duplicate items`)

	// A non-scalar element falls back to JSON equality, which is what the
	// spec means by equal.
	got = compile(t, map[string]any{
		"type": "array", "uniqueItems": true,
		"items": map[string]any{"$ref": "types/line_item.json"},
	}, "v.Field", accessValue)
	wants(t, got, `json.Marshal`, `field: contains duplicate items`)

	// A constraint on a scalar element applies per element.
	got = compile(t, map[string]any{
		"type":  "array",
		"items": map[string]any{"type": "string", "enum": []any{"a", "b"}},
	}, "v.Field", accessValue)
	wants(t, got, `range v.Field`, `!= "a" && `, `field item: not one of the permitted values`)
}

func TestCompileConstraintsMapChecks(t *testing.T) {
	got := compile(t, map[string]any{
		"type": "object", "minProperties": float64(1), "maxProperties": float64(4),
		"additionalProperties": map[string]any{"type": "string"},
	}, "v.Field", accessValue)
	wants(t, got, `len(v.Field) < 1`, `len(v.Field) > 4`)

	// propertyNames constrains the keys, which are always strings, so it
	// reuses the string checks against the loop variable.
	got = compile(t, map[string]any{
		"type":                 "object",
		"additionalProperties": map[string]any{"type": "string"},
		"propertyNames":        map[string]any{"pattern": "^[a-z]+$"},
	}, "v.Field", accessValue)
	wants(t, got,
		`pattern_Thing_Field_Key`,
		`for k := range v.Field`,
		`field property name: does not match pattern`)
}

// TestCompileObjectSelfCountsPresentProperties covers description.json:
// three optional fields and "at least one format must be provided". The
// count has to be of properties actually set, which for a struct means
// non-nil fields plus whatever landed in Extra.
func TestCompileObjectSelfCountsPresentProperties(t *testing.T) {
	e := newFileEmitter(idxFixture(t), "shopping/types/description.json", "types")
	c := &constraintSet{}
	schema := map[string]any{"type": "object", "minProperties": float64(1)}
	fields := []structField{
		{jsonName: "html", goName: "HTML"},
		{jsonName: "plain", goName: "Plain"},
	}
	if err := compileObjectSelf(e, c, "Description", schema, fields, true); err != nil {
		t.Fatalf("compileObjectSelf: %v", err)
	}
	got := c.checks.String()
	for _, want := range []string{
		"if v.HTML != nil {", "if v.Plain != nil {", "len(v.Extra)",
		"Description: has fewer than minProperties 1",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if !e.enforced.has(schema, "minProperties") {
		t.Error("minProperties was checked but not recorded as enforced")
	}
}

// A required property is present by construction, so it contributes to the
// count without a runtime test.
func TestCompileObjectSelfCountsRequiredWithoutATest(t *testing.T) {
	e := newFileEmitter(idxFixture(t), "shopping/types/description.json", "types")
	c := &constraintSet{}
	schema := map[string]any{"type": "object", "minProperties": float64(2)}
	fields := []structField{
		{jsonName: "id", goName: "ID", required: true},
		{jsonName: "plain", goName: "Plain"},
	}
	if err := compileObjectSelf(e, c, "Thing", schema, fields, false); err != nil {
		t.Fatalf("compileObjectSelf: %v", err)
	}
	got := c.checks.String()
	if strings.Contains(got, "v.ID != nil") {
		t.Errorf("a required field needs no presence test:\n%s", got)
	}
	if !strings.Contains(got, "n := 1") {
		t.Errorf("required fields should seed the count:\n%s", got)
	}
}

// TestCompileObjectSelfPropertyNames covers signals.json: an open object
// whose extension keys must match a pattern.
func TestCompileObjectSelfPropertyNames(t *testing.T) {
	e := newFileEmitter(idxFixture(t), "shopping/types/signals.json", "types")
	c := &constraintSet{}
	schema := map[string]any{
		"type":          "object",
		"propertyNames": map[string]any{"pattern": `^[a-z][a-z0-9]*(?:\.[a-z][a-z0-9_]*)+$`},
	}
	fields := []structField{{jsonName: "dev.ucp.buyer_ip", goName: "DevUCPBuyerIP"}}
	if err := compileObjectSelf(e, c, "Signals", schema, fields, true); err != nil {
		t.Fatalf("compileObjectSelf: %v", err)
	}
	got := c.vars.String() + c.checks.String()
	for _, want := range []string{
		"pattern_Signals_Key", "for k := range v.Extra",
		"Signals property name: does not match pattern",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

// A named property that violates the schema's own propertyNames would make
// the generated check incomplete — the runtime loop only sees Extra — so it
// must fail generation rather than pass silently.
func TestCompileObjectSelfRejectsNamedPropertyFailingPropertyNames(t *testing.T) {
	e := newFileEmitter(idxFixture(t), "shopping/types/signals.json", "types")
	c := &constraintSet{}
	schema := map[string]any{
		"type":          "object",
		"propertyNames": map[string]any{"pattern": `^[a-z][a-z0-9]*(?:\.[a-z][a-z0-9_]*)+$`},
	}
	fields := []structField{{jsonName: "NotReverseDomain", goName: "NotReverseDomain"}}
	err := compileObjectSelf(e, c, "Signals", schema, fields, true)
	if err == nil {
		t.Fatal("a named property violating propertyNames must fail generation")
	}
	if !strings.Contains(err.Error(), "NotReverseDomain") {
		t.Errorf("error should name the offending property: %v", err)
	}
}

// TestCompileConstraintsReportsWhatItEnforced lets the caller subtract
// enforced keywords from what it reports as a coverage gap, so a keyword is
// never both checked and advertised as unchecked.
func TestCompileConstraintsReportsWhatItEnforced(t *testing.T) {
	e := newFileEmitter(idxFixture(t), "shopping/checkout.json", "shopping")
	c := &constraintSet{}
	node := map[string]any{"type": "string", "enum": []any{"a"}, "format": "uri"}
	if err := compileConstraints(e, c, "Thing", "field", "v.Field", accessValue, node); err != nil {
		t.Fatalf("compileConstraints: %v", err)
	}
	if !e.enforced.has(node, "enum") {
		t.Error("enum was checked but not recorded as enforced")
	}
	if e.enforced.has(node, "format") {
		t.Error("format is not checked and must not be recorded as enforced")
	}
	// format is reported, but as an annotation the dialect does not make
	// assertable — never as an unmet obligation.
	gap, ann := e.unenforcedKeywords(node)
	if len(gap) != 0 {
		t.Errorf("unenforced gap = %v, want none: format is not a gap", gap)
	}
	if len(ann) != 1 || ann[0] != "format" {
		t.Errorf("notAsserted = %v, want [format]", ann)
	}
}

func TestNestedArrayElementConstraintsAreCompiled(t *testing.T) {
	// common/types/business_split_payments_config: allowed_combinations is
	// an array of arrays, with minItems on BOTH levels. Only the outer one
	// was checked. An array element is neither a scalar (which the descent
	// tested for) nor an object promoted to a named type with its own
	// Validate, so its constraints had nowhere to go and were dropped.
	node := map[string]any{
		"type":     "array",
		"minItems": float64(1),
		"items": map[string]any{
			"type":     "array",
			"minItems": float64(2),
			"items":    map[string]any{"type": "string"},
		},
	}
	e := newFileEmitter(idxFixture(t), "shopping/types/line_item.json", "types")
	var c constraintSet
	tg := target{typeName: "Thing", varStem: "Thing", label: "combos", expr: "v.Combos", access: accessValue}
	if err := compileArray(e, &c, tg, node); err != nil {
		t.Fatalf("compileArray: %v", err)
	}
	got := c.checks.String()
	if !strings.Contains(got, "len(v.Combos) < 1") {
		t.Errorf("outer minItems missing:\n%s", got)
	}
	if !strings.Contains(got, "< 2") {
		t.Errorf("inner minItems missing — the element's own constraints were dropped:\n%s", got)
	}
	inner, _ := node["items"].(map[string]any)
	if !e.enforced.has(inner, "minItems") {
		t.Error("the element's minItems was checked and must be recorded as enforced")
	}
}

func TestMapValueConstraintsAreCompiled(t *testing.T) {
	// common/types/actions is map[string][]ActionsInstance whose VALUE
	// declares minItems 1. compileMap descended only into the keys, so the
	// value's constraints were never reached.
	node := map[string]any{
		"type": "object",
		"additionalProperties": map[string]any{
			"type":     "array",
			"minItems": float64(1),
			"items":    map[string]any{"type": "string"},
		},
	}
	e := newFileEmitter(idxFixture(t), "shopping/types/line_item.json", "types")
	var c constraintSet
	tg := target{typeName: "Bag", varStem: "Bag", label: "Bag", expr: "*v", access: accessValue}
	if err := compileMap(e, &c, tg, node); err != nil {
		t.Fatalf("compileMap: %v", err)
	}
	got := c.checks.String()
	if !strings.Contains(got, "< 1") {
		t.Errorf("the map value's minItems was dropped:\n%s", got)
	}
	vals, _ := node["additionalProperties"].(map[string]any)
	if !e.enforced.has(vals, "minItems") {
		t.Error("the value's minItems was checked and must be recorded as enforced")
	}
}

func TestObjectValuesAreNotCheckedTwice(t *testing.T) {
	// An object element or value is promoted to a named type by goTypeExpr
	// and validates itself, so compiling it here as well would emit the same
	// check twice. The descent must skip it.
	node := map[string]any{
		"type": "object",
		"additionalProperties": map[string]any{
			"type":       "object",
			"required":   []any{"id"},
			"properties": map[string]any{"id": map[string]any{"type": "string"}},
		},
	}
	e := newFileEmitter(idxFixture(t), "shopping/types/line_item.json", "types")
	var c constraintSet
	tg := target{typeName: "Bag", varStem: "Bag", label: "Bag", expr: "*v", access: accessValue}
	if err := compileMap(e, &c, tg, node); err != nil {
		t.Fatalf("compileMap: %v", err)
	}
	if strings.Contains(c.checks.String(), "required property is missing") {
		t.Errorf("an object value validates itself; it must not be compiled here too:\n%s", c.checks.String())
	}
}
