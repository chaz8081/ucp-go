package emit

import (
	"strings"
	"testing"
)

func TestFieldsForCarriesGoType(t *testing.T) {
	schema := map[string]any{
		"type":     "object",
		"required": []any{"type"},
		"properties": map[string]any{
			"type":         map[string]any{"type": "string"},
			"display_text": map[string]any{"type": "string"},
			"tags":         map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		},
	}
	e := newFileEmitter(idxFixture(t), "shopping/types/line_item.json", "types")
	fields, err := fieldsFor(e, "Total", schema)
	if err != nil {
		t.Fatalf("fieldsFor: %v", err)
	}
	got := map[string]structField{}
	for _, f := range fields {
		got[f.jsonName] = f
	}
	if got["type"].goType != "string" {
		t.Errorf("required scalar: goType = %q, want %q", got["type"].goType, "string")
	}
	if got["display_text"].goType != "*string" {
		t.Errorf("optional scalar: goType = %q, want %q", got["display_text"].goType, "*string")
	}
	// The proxy `required` would call this a pointer. It is not: fieldsFor
	// leaves nilable types unpointered so callers write v.Tags[0].
	if got["tags"].goType != "[]string" {
		t.Errorf("optional slice: goType = %q, want %q", got["tags"].goType, "[]string")
	}
}

// fieldsFor sets the nested-type prefix itself. A caller that had to set
// it first would get silently mis-named types on forgetting to, so the
// invariant is checked here rather than trusted at every call site.
func TestFieldsForNamespacesNestedTypesWithoutCallerSetup(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"adjustment": map[string]any{
				"type":       "object",
				"properties": map[string]any{"reason": map[string]any{"type": "string"}},
			},
		},
	}
	e := newFileEmitter(idxFixture(t), "shopping/types/line_item.json", "types")
	before := e.prefix
	fields, err := fieldsFor(e, "Total", schema)
	if err != nil {
		t.Fatalf("fieldsFor: %v", err)
	}
	if got, want := fields[0].goType, "*TotalAdjustment"; got != want {
		t.Errorf("inline object goType = %q, want %q", got, want)
	}
	if _, registered := e.nestedSchemas["TotalAdjustment"]; !registered {
		t.Errorf("nested type was not registered under the enclosing type's name: %v", e.nestedSchemas)
	}
	// The prefix is restored, so renderStruct's own later work — and any
	// sibling call — sees the emitter exactly as it left it.
	if e.prefix != before {
		t.Errorf("prefix = %q after fieldsFor, want it restored to %q", e.prefix, before)
	}
}

func totalFields() []structField {
	return []structField{
		{jsonName: "amount", goName: "Amount", goType: "SignedAmount", required: true},
		{jsonName: "display_text", goName: "DisplayText", goType: "*string"},
		{jsonName: "type", goName: "Type", goType: "string", required: true},
	}
}

func TestPredicateConstOnRequiredField(t *testing.T) {
	node := map[string]any{
		"properties": map[string]any{"type": map[string]any{"const": "subtotal"}},
		"required":   []any{"type"},
	}
	got, err := predicate(newFileEmitter(idxFixture(t), "shopping/types/total.json", "types"), "Total", "v", node, totalFields())
	if err != nil {
		t.Fatalf("predicate: %v", err)
	}
	want := `v.Type == "subtotal"`
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestPredicateEnumOnRequiredField(t *testing.T) {
	node := map[string]any{
		"properties": map[string]any{"type": map[string]any{"enum": []any{"discount", "items_discount"}}},
		"required":   []any{"type"},
	}
	got, err := predicate(newFileEmitter(idxFixture(t), "shopping/types/total.json", "types"), "Total", "v", node, totalFields())
	if err != nil {
		t.Fatalf("predicate: %v", err)
	}
	want := `(v.Type == "discount" || v.Type == "items_discount")`
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// A negated enum is the one shape the zero value falsely satisfies: "" is
// not in the excluded list, so a hand-built value would match. It must be
// gated on the presence record.
func TestPredicateNegatedEnumGatesOnPresence(t *testing.T) {
	node := map[string]any{
		"properties": map[string]any{
			"type": map[string]any{"not": map[string]any{"enum": []any{"subtotal", "total"}}},
		},
		"required": []any{"type"},
	}
	got, err := predicate(newFileEmitter(idxFixture(t), "shopping/types/total.json", "types"), "Total", "v", node, totalFields())
	if err != nil {
		t.Fatalf("predicate: %v", err)
	}
	want := `v.present != nil && v.present["type"] && v.Type != "subtotal" && v.Type != "total"`
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestPredicateRequiredOnlyUsesPointerNilCheck(t *testing.T) {
	node := map[string]any{"required": []any{"display_text"}}
	got, err := predicate(newFileEmitter(idxFixture(t), "shopping/types/total.json", "types"), "Total", "v", node, totalFields())
	if err != nil {
		t.Fatalf("predicate: %v", err)
	}
	want := `v.DisplayText != nil`
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestPredicateRejectsOptionalPropertyTestWithoutRequired(t *testing.T) {
	// `properties` without `required` also matches an absent property, so
	// compiling it as present-and-matching would under-apply the rule the
	// condition guards.
	node := map[string]any{
		"properties": map[string]any{"display_text": map[string]any{"const": "x"}},
	}
	_, err := predicate(newFileEmitter(idxFixture(t), "shopping/types/line_item.json", "types"), "Total", "v", node, totalFields())
	if err == nil {
		t.Fatal("expected an error for a properties test with no required, got nil")
	}
}

func TestConditionalLiteralRejectsFractionalIntegerBound(t *testing.T) {
	f := structField{jsonName: "count", goName: "Count", goType: "int64", required: true}
	if _, err := conditionalLiteral("Thing", f, 1.5); err == nil {
		t.Fatal("expected an error for a fractional literal against int64, got nil")
	}
	if _, err := conditionalLiteral("Thing", f, float64(3)); err != nil {
		t.Errorf("integral literal against int64 should be accepted: %v", err)
	}
}

func TestPredicateRejectsUnsupportedForms(t *testing.T) {
	cases := map[string]map[string]any{
		"multi-property discriminator": {
			"properties": map[string]any{
				"type":   map[string]any{"const": "a"},
				"amount": map[string]any{"const": float64(1)},
			},
			"required": []any{"type", "amount"},
		},
		"unsupported keyword": {
			"properties": map[string]any{"type": map[string]any{"minLength": float64(3)}},
			"required":   []any{"type"},
		},
		"unknown property": {
			"properties": map[string]any{"nope": map[string]any{"const": "x"}},
			"required":   []any{"nope"},
		},
		"nested subschema keyword": {
			"allOf": []any{map[string]any{"const": "x"}},
		},
	}
	for name, node := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := predicate(newFileEmitter(idxFixture(t), "shopping/types/total.json", "types"), "Total", "v", node, totalFields()); err == nil {
				t.Fatal("expected an error, got nil")
			}
		})
	}
}

func TestCompileConditionalEmitsGuardedThen(t *testing.T) {
	schema := map[string]any{
		"type":     "object",
		"required": []any{"has_next_page"},
		"properties": map[string]any{
			"cursor":        map[string]any{"type": "string"},
			"has_next_page": map[string]any{"type": "boolean"},
		},
		"if": map[string]any{
			"properties": map[string]any{"has_next_page": map[string]any{"const": true}},
			"required":   []any{"has_next_page"},
		},
		"then": map[string]any{"required": []any{"cursor"}},
	}
	e := newFileEmitter(idxFixture(t), "shopping/types/line_item.json", "types")
	fields, err := fieldsFor(e, "PaginationResponse", schema)
	if err != nil {
		t.Fatalf("fieldsFor: %v", err)
	}
	var c constraintSet
	if err := compileConditional(e, &c, "PaginationResponse", schema, fields); err != nil {
		t.Fatalf("compileConditional: %v", err)
	}
	got := c.checks.String()
	for _, want := range []string{
		"if v.HasNextPage == true {",
		"if v.Cursor == nil {",
		"cursor: required property is missing when has_next_page is true",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("emitted code missing %q:\n%s", want, got)
		}
	}
	if !e.enforced.has(schema, "if") || !e.enforced.has(schema, "then") {
		t.Error("if/then not marked enforced; the doc comment would still report them as gaps")
	}
}

func TestCompileConditionalRejectsElse(t *testing.T) {
	// else has zero occurrences in the corpus. Every other unimplemented
	// keyword in this emitter fails loudly rather than guessing, and the
	// first real else can justify itself.
	schema := map[string]any{
		"type":       "object",
		"required":   []any{"type"},
		"properties": map[string]any{"type": map[string]any{"type": "string"}},
		"if": map[string]any{
			"properties": map[string]any{"type": map[string]any{"const": "a"}},
			"required":   []any{"type"},
		},
		"then": map[string]any{"required": []any{"type"}},
		"else": map[string]any{"required": []any{"type"}},
	}
	e := newFileEmitter(idxFixture(t), "shopping/types/line_item.json", "types")
	fields, err := fieldsFor(e, "Thing", schema)
	if err != nil {
		t.Fatalf("fieldsFor: %v", err)
	}
	var c constraintSet
	if err := compileConditional(e, &c, "Thing", schema, fields); err == nil {
		t.Fatal("expected an error for else, got nil")
	}
}

func TestCompileConditionalRejectsHalfADeclaration(t *testing.T) {
	schema := map[string]any{
		"type":       "object",
		"required":   []any{"type"},
		"properties": map[string]any{"type": map[string]any{"type": "string"}},
		"then":       map[string]any{"required": []any{"type"}},
	}
	e := newFileEmitter(idxFixture(t), "shopping/types/line_item.json", "types")
	fields, err := fieldsFor(e, "Thing", schema)
	if err != nil {
		t.Fatalf("fieldsFor: %v", err)
	}
	var c constraintSet
	if err := compileConditional(e, &c, "Thing", schema, fields); err == nil {
		t.Fatal("expected an error for then without if, got nil")
	}
}

// The description lands in an error message an SDK user reads, not a Go
// programmer: %v over the schema's []any would render `[discount
// items_discount]`, which is Go's syntax for a slice rather than the
// values the user wrote in their JSON.
func TestConditionalDescriptionReadsAsProse(t *testing.T) {
	cases := map[string]struct {
		props map[string]any
		want  string
	}{
		"boolean const": {
			map[string]any{"has_next_page": map[string]any{"const": true}},
			"has_next_page is true",
		},
		"string const": {
			map[string]any{"type": map[string]any{"const": "subtotal"}},
			`type is "subtotal"`,
		},
		"enum": {
			map[string]any{"type": map[string]any{"enum": []any{"discount", "items_discount"}}},
			`type is one of "discount", "items_discount"`,
		},
		"negated enum": {
			map[string]any{"type": map[string]any{"not": map[string]any{"enum": []any{"subtotal", "total"}}}},
			`type is not one of "subtotal", "total"`,
		},
		"unrecognized form": {
			map[string]any{"type": map[string]any{"minLength": float64(1)}},
			"the schema's condition holds",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := describe(map[string]any{"properties": tc.props}); got != tc.want {
				t.Errorf("describe = %q, want %q", got, tc.want)
			}
		})
	}
}

// A consequent's property rules are compiled into a local set so the
// statements can be wrapped in the guard. Anything else that compiler
// produces belongs to the enclosing type: a compiled pattern is declared
// at package level, and loop variables are numbered across the whole
// Validate body. Left in the local set they would be dropped, and the
// guarded check would call a variable the generated file never declares.
func TestCompileConditionalPropagatesGeneratedVarsAndLoops(t *testing.T) {
	schema := map[string]any{
		"type":     "object",
		"required": []any{"kind"},
		"properties": map[string]any{
			"kind": map[string]any{"type": "string"},
			"code": map[string]any{"type": "string"},
			"tags": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		},
		"if": map[string]any{
			"properties": map[string]any{"kind": map[string]any{"const": "coded"}},
			"required":   []any{"kind"},
		},
		"then": map[string]any{
			"properties": map[string]any{
				"code": map[string]any{"type": "string", "pattern": "^[a-z]+$"},
				"tags": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "uniqueItems": true},
			},
		},
	}
	e := newFileEmitter(idxFixture(t), "shopping/types/line_item.json", "types")
	fields, err := fieldsFor(e, "Thing", schema)
	if err != nil {
		t.Fatalf("fieldsFor: %v", err)
	}
	var c constraintSet
	if err := compileConditional(e, &c, "Thing", schema, fields); err != nil {
		t.Fatalf("compileConditional: %v", err)
	}
	if !strings.Contains(c.checks.String(), "pattern_Thing_Code()") {
		t.Fatalf("guarded check does not reference the compiled pattern:\n%s", c.checks.String())
	}
	if !strings.Contains(c.vars.String(), `regexp.MustCompile("^[a-z]+$")`) {
		t.Errorf("compiled pattern was dropped instead of reaching the enclosing type: %q", c.vars.String())
	}
	if c.loops == 0 {
		t.Error("loop variables consumed inside the consequent were not counted; a later loop would reuse the same name")
	}
}

func TestCompileConditionalReadsAllOfResidual(t *testing.T) {
	schema := map[string]any{
		"type":     "object",
		"required": []any{"amount", "type"},
		"properties": map[string]any{
			"amount": map[string]any{"type": "number"},
			"type":   map[string]any{"type": "string"},
		},
		"allOf": []any{
			map[string]any{
				"if": map[string]any{
					"properties": map[string]any{"type": map[string]any{"enum": []any{"discount", "items_discount"}}},
					"required":   []any{"type"},
				},
				"then": map[string]any{
					"properties": map[string]any{"amount": map[string]any{"exclusiveMaximum": float64(0)}},
				},
			},
			map[string]any{
				"if": map[string]any{
					"properties": map[string]any{"type": map[string]any{"enum": []any{"subtotal", "tax"}}},
					"required":   []any{"type"},
				},
				"then": map[string]any{
					"properties": map[string]any{"amount": map[string]any{"minimum": float64(0)}},
				},
			},
		},
	}
	e := newFileEmitter(idxFixture(t), "shopping/types/line_item.json", "types")
	fields, err := fieldsFor(e, "Total", schema)
	if err != nil {
		t.Fatalf("fieldsFor: %v", err)
	}
	var c constraintSet
	if err := compileConditional(e, &c, "Total", schema, fields); err != nil {
		t.Fatalf("compileConditional: %v", err)
	}
	got := c.checks.String()
	// Both rules must survive. The upstream bug this mirrors kept only the last.
	for _, want := range []string{
		`(v.Type == "discount" || v.Type == "items_discount")`,
		`(v.Type == "subtotal" || v.Type == "tax")`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("emitted code missing %q:\n%s", want, got)
		}
	}
	// enumTest parenthesizes a multi-member disjunction, so each guard opens
	// with "if (". Counting them is what proves the second rule was not
	// overwritten by the first rather than merely present somewhere.
	if strings.Count(got, "if (v.Type ==") != 2 {
		t.Errorf("expected two guards, got:\n%s", got)
	}
	// Both consequents have to reach the amount, and in opposite directions:
	// a discount is strictly negative, a charge is not negative.
	for _, want := range []string{"if v.Amount >= 0 {", "if v.Amount < 0 {"} {
		if !strings.Contains(got, want) {
			t.Errorf("emitted code missing %q:\n%s", want, got)
		}
	}
	// The branch, not the parent, carries the keywords; marking the parent
	// would leave unenforcedKeywords still reporting them as a coverage gap.
	for i, b := range schema["allOf"].([]any) {
		bm := b.(map[string]any)
		if !e.enforced.has(bm, "if") || !e.enforced.has(bm, "then") {
			t.Errorf("allOf branch %d: if/then not marked enforced on the branch", i)
		}
	}
	if kws, _ := e.unenforcedKeywords(schema); len(kws) != 0 {
		t.Errorf("unenforcedKeywords = %v, want none once both rules compile", kws)
	}
}

// A ucp_request variant keeps its parent's rules while dropping the
// properties they talk about, so total_create_request.json arrives with
// total.json's two amount rules over an empty property set. The rule is
// about a shape this type does not have, so no check is emitted — and,
// critically, if/then stay unmarked so the doc comment and manifest keep
// reporting the gap rather than claiming an enforcement that never happened.
func TestCompileConditionalSkipsRuleOverDroppedProperties(t *testing.T) {
	branch := map[string]any{
		"if": map[string]any{
			"properties": map[string]any{"type": map[string]any{"enum": []any{"discount"}}},
			"required":   []any{"type"},
		},
		"then": map[string]any{
			"properties": map[string]any{"amount": map[string]any{"exclusiveMaximum": float64(0)}},
		},
	}
	schema := map[string]any{
		"type":       "object",
		"required":   []any{},
		"properties": map[string]any{},
		"allOf":      []any{branch},
	}
	e := newFileEmitter(idxFixture(t), "shopping/types/line_item.json", "types")
	fields, err := fieldsFor(e, "TotalCreateRequest", schema)
	if err != nil {
		t.Fatalf("fieldsFor: %v", err)
	}
	var c constraintSet
	if err := compileConditional(e, &c, "TotalCreateRequest", schema, fields); err != nil {
		t.Fatalf("compileConditional: %v", err)
	}
	if got := c.checks.String(); got != "" {
		t.Errorf("emitted checks for properties the type does not have:\n%s", got)
	}
	if e.enforced.has(branch, "if") || e.enforced.has(branch, "then") {
		t.Error("if/then marked enforced though no check was emitted")
	}
	if kws, _ := e.unenforcedKeywords(schema); len(kws) != 2 || kws[0] != "if" || kws[1] != "then" {
		t.Errorf("unenforcedKeywords = %v, want [if then] still reported as a gap", kws)
	}
}

// containsNode mirrors totals.json: an array whose elements are objects,
// with a rule that exactly one of them is the subtotal entry.
func containsNode() map[string]any {
	return map[string]any{
		"type": "array",
		"items": map[string]any{
			"type":     "object",
			"required": []any{"type"},
			"properties": map[string]any{
				"type": map[string]any{"type": "string"},
			},
		},
		"contains": map[string]any{
			"properties": map[string]any{"type": map[string]any{"const": "subtotal"}},
			"required":   []any{"type"},
		},
		"minContains": float64(1),
		"maxContains": float64(1),
	}
}

func TestCompileContainsCountsMatchingElements(t *testing.T) {
	node := containsNode()
	e := newFileEmitter(idxFixture(t), "shopping/types/line_item.json", "types")
	var c constraintSet
	tgt := target{typeName: "Totals", varStem: "Totals", label: "Totals", expr: "v", access: accessValue}
	if err := compileContains(e, &c, tgt, node); err != nil {
		t.Fatalf("compileContains: %v", err)
	}
	got := c.checks.String()
	for _, want := range []string{"range v", `== "subtotal"`, "< 1", "> 1"} {
		if !strings.Contains(got, want) {
			t.Errorf("emitted code missing %q:\n%s", want, got)
		}
	}
	// An unmarked keyword keeps being reported as a coverage gap, so a
	// check that exists but is not recorded makes the doc comment lie.
	for _, kw := range []string{"contains", "minContains", "maxContains"} {
		if !e.enforced.has(node, kw) {
			t.Errorf("%s checked but not marked enforced", kw)
		}
	}
}

// A bound with nothing to count asserts nothing. Silently ignoring it
// would hide a schema the generator does not actually understand.
func TestCompileContainsRejectsBoundWithoutContains(t *testing.T) {
	node := map[string]any{
		"type":        "array",
		"items":       map[string]any{"type": "string"},
		"minContains": float64(1),
	}
	e := newFileEmitter(idxFixture(t), "shopping/types/line_item.json", "types")
	var c constraintSet
	tgt := target{typeName: "Totals", varStem: "Totals", label: "Totals", expr: "v", access: accessValue}
	if err := compileContains(e, &c, tgt, node); err == nil {
		t.Fatal("expected an error for minContains without contains, got nil")
	}
}

// contains defaults to minContains 1: the array must hold at least one
// matching element even when no bound is written down.
func TestCompileContainsDefaultsToAtLeastOne(t *testing.T) {
	node := containsNode()
	delete(node, "minContains")
	delete(node, "maxContains")
	e := newFileEmitter(idxFixture(t), "shopping/types/line_item.json", "types")
	var c constraintSet
	tgt := target{typeName: "Totals", varStem: "Totals", label: "Totals", expr: "v", access: accessValue}
	if err := compileContains(e, &c, tgt, node); err != nil {
		t.Fatalf("compileContains: %v", err)
	}
	got := c.checks.String()
	if !strings.Contains(got, "< 1") {
		t.Errorf("absent minContains must still require one match:\n%s", got)
	}
	if strings.Contains(got, "> ") {
		t.Errorf("absent maxContains must not emit an upper bound:\n%s", got)
	}
	if !e.enforced.has(node, "contains") {
		t.Error("contains not marked enforced")
	}
	if e.enforced.has(node, "maxContains") {
		t.Error("maxContains marked enforced though the schema does not declare it")
	}
}

// A conditional gated on the presence record does nothing for values built
// in Go. A rule that silently does nothing is worse than an absent one,
// because the reader has no way to tell, so the type has to say so.
func TestPresenceGatedConditionalIsRecordedForDisclosure(t *testing.T) {
	gated := map[string]any{
		"type":     "object",
		"required": []any{"type"},
		"properties": map[string]any{
			"display_text": map[string]any{"type": "string"},
			"type":         map[string]any{"type": "string"},
		},
		"if": map[string]any{
			"properties": map[string]any{
				"type": map[string]any{"not": map[string]any{"enum": []any{"subtotal", "total"}}},
			},
			"required": []any{"type"},
		},
		"then": map[string]any{"required": []any{"display_text"}},
	}
	// The same shape with a positive test needs no presence record: the
	// zero value cannot satisfy `== "subtotal"`.
	plain := map[string]any{
		"type":     "object",
		"required": []any{"type"},
		"properties": map[string]any{
			"display_text": map[string]any{"type": "string"},
			"type":         map[string]any{"type": "string"},
		},
		"if": map[string]any{
			"properties": map[string]any{"type": map[string]any{"const": "subtotal"}},
			"required":   []any{"type"},
		},
		"then": map[string]any{"required": []any{"display_text"}},
	}
	for name, tc := range map[string]struct {
		schema map[string]any
		want   bool
	}{"negated": {gated, true}, "positive": {plain, false}} {
		t.Run(name, func(t *testing.T) {
			e := newFileEmitter(idxFixture(t), "shopping/types/line_item.json", "types")
			fields, err := fieldsFor(e, "Item", tc.schema)
			if err != nil {
				t.Fatalf("fieldsFor: %v", err)
			}
			var c constraintSet
			if err := compileConditional(e, &c, "Item", tc.schema, fields); err != nil {
				t.Fatalf("compileConditional: %v", err)
			}
			if got := e.presenceGated["Item"]; got != tc.want {
				t.Errorf("presenceGated = %v, want %v", got, tc.want)
			}
		})
	}
}
