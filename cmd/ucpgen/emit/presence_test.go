package emit

import (
	"strings"
	"testing"
)

// requiredFixture is an object with one required and one optional property,
// closed so that the presence codec cannot be mistaken for the Extra codec.
func requiredFixture() map[string]any {
	return map[string]any{
		"title": "Thing", "type": "object",
		"additionalProperties": false,
		"required":             []any{"id"},
		"properties": map[string]any{
			"id":    map[string]any{"type": "string"},
			"label": map[string]any{"type": "string"},
		},
	}
}

// TestEmitPresenceTracksRequiredProperties pins the whole mechanism: a
// required property absent from the JSON is otherwise indistinguishable
// from one present with its zero value, so Validate would accept payloads
// the schema rejects.
func TestEmitPresenceTracksRequiredProperties(t *testing.T) {
	src, err := emitOne(t, "test/thing.json", requiredFixture())
	if err != nil {
		t.Fatalf("EmitFile: %v", err)
	}
	for _, want := range []string{
		"present map[string]bool",
		"func (v *Thing) UnmarshalJSON(data []byte) error {",
		`for _, name := range []string{"id"} {`,
		`if _, ok := all[name]; ok {`,
		`if !v.present["id"] {`,
		`return errors.New("id: required property is missing")`,
	} {
		if !strings.Contains(src, want) {
			t.Errorf("missing %q in:\n%s", want, src)
		}
	}
	// Only required properties are tracked; an optional one has nothing to
	// report and would only grow the map.
	if strings.Contains(src, `if !v.present["label"]`) {
		t.Errorf("optional properties must not be presence-checked:\n%s", src)
	}
}

// TestEmitPresenceSkippedWhenUnknown is the rule that keeps the SDK usable
// for building requests: a value constructed in Go was never decoded, so it
// carries no presence information and must not be judged on it.
func TestEmitPresenceSkippedWhenUnknown(t *testing.T) {
	src, err := emitOne(t, "test/thing.json", requiredFixture())
	if err != nil {
		t.Fatalf("EmitFile: %v", err)
	}
	if !strings.Contains(src, "if v.present != nil {") {
		t.Errorf("presence checks must be guarded on presence being known:\n%s", src)
	}
}

// A closed object gets a decoder purely for presence — it has no Extra to
// collect — and must not grow a MarshalJSON it does not need.
func TestEmitPresenceCodecOnClosedObjectHasNoExtra(t *testing.T) {
	src, err := emitOne(t, "test/thing.json", requiredFixture())
	if err != nil {
		t.Fatalf("EmitFile: %v", err)
	}
	if strings.Contains(src, "Extra map[string]json.RawMessage") {
		t.Errorf("a closed object must not get an Extra field:\n%s", src)
	}
	if strings.Contains(src, "func (v Thing) MarshalJSON()") {
		t.Errorf("a closed object needs no custom marshaler:\n%s", src)
	}
}

// A schema with no required properties needs no presence machinery — but it
// still needs a decoder, because nothing else rejects a bare null.
//
// This test used to assert the opposite, that such a type needs no
// UnmarshalJSON at all. That was wrong for the reason recorded on
// renderNullOnlyObjectCodec: object roots were exempted from the null guard
// because their presence check would catch it, which silently assumes there
// is something required to be missing. spec 2026-08-25's
// common/types/constraint_expression requires nothing, and accepted null.
func TestEmitNoPresenceWithoutRequired(t *testing.T) {
	schema := map[string]any{
		"title": "Loose", "type": "object",
		"additionalProperties": false,
		"properties":           map[string]any{"id": map[string]any{"type": "string"}},
	}
	src, err := emitOne(t, "test/loose.json", schema)
	if err != nil {
		t.Fatalf("EmitFile: %v", err)
	}
	if strings.Contains(src, "present map[string]bool") {
		t.Errorf("no required properties means no presence field:\n%s", src)
	}
	if !strings.Contains(src, "func (v *Loose) UnmarshalJSON") {
		t.Errorf("a closed object still needs a decoder to reject null:\n%s", src)
	}
	if !strings.Contains(src, `null is not a valid object`) {
		t.Errorf("the decoder must reject a bare null:\n%s", src)
	}
	// The decoder exists for the null guard alone; it must not smuggle in
	// presence tracking there is nothing to track.
	if strings.Contains(src, "v.present") {
		t.Errorf("no required properties means no presence capture:\n%s", src)
	}
}

func TestDependentRequiredMutualRule(t *testing.T) {
	// common/types/time_interval: opens and closes require each other, and
	// neither is required on its own. Such a type used to get no presence
	// record at all — the record was emitted only for unconditionally
	// required properties — leaving the rule with nothing to read.
	schema := map[string]any{
		"title": "Interval", "type": "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"opens":  map[string]any{"type": "string"},
			"closes": map[string]any{"type": "string"},
		},
		"dependentRequired": map[string]any{
			"opens":  []any{"closes"},
			"closes": []any{"opens"},
		},
	}
	src, err := emitOne(t, "test/interval.json", schema)
	if err != nil {
		t.Fatalf("EmitFile: %v", err)
	}
	if !strings.Contains(src, "present map[string]bool") {
		t.Fatalf("a dependentRequired rule needs the presence record:\n%s", src)
	}
	for _, want := range []string{
		`if v.present["opens"] && !v.present["closes"] {`,
		`errors.New("closes: required when opens is present")`,
		`if v.present["closes"] && !v.present["opens"] {`,
		`errors.New("opens: required when closes is present")`,
	} {
		if !strings.Contains(src, want) {
			t.Errorf("missing %q in:\n%s", want, src)
		}
	}
	// Guarded, like every presence check: a value built in Go rather than
	// decoded has no record, and judging a dependency against it would fail
	// every hand-constructed request.
	if !strings.Contains(src, "if v.present != nil {") {
		t.Errorf("dependent checks must be guarded on a decoded value:\n%s", src)
	}
	// The rule is conditional, so neither property may be reported missing
	// on its own.
	if strings.Contains(src, `errors.New("opens: required property is missing")`) {
		t.Errorf("a dependentRequired participant is not unconditionally required:\n%s", src)
	}
}

func TestDependentRequiredReportsRuleNamingAbsentProperty(t *testing.T) {
	// A request variant drops properties marked ucp_request:omit while
	// keeping the rules, so a rule can name a property the Go struct no
	// longer has — every *_create_request form of time_interval and
	// fulfillment_method in spec 2026-08-25 is shaped that way.
	//
	// There is nothing to bind the rule to, so it must be reported rather
	// than approximated, and above all must not be marked enforced.
	schema := map[string]any{
		"title": "Dropped", "type": "object",
		"additionalProperties": false,
		"properties":           map[string]any{"kept": map[string]any{"type": "string"}},
		"dependentRequired":    map[string]any{"gone": []any{"kept"}},
	}
	src, err := emitOne(t, "test/dropped.json", schema)
	if err != nil {
		t.Fatalf("EmitFile: %v", err)
	}
	if strings.Contains(src, "v.present[\"gone\"]") {
		t.Errorf("no check may reference a property the type does not declare:\n%s", src)
	}
	if !strings.Contains(src, "Not enforced yet on the object itself: dependentRequired") {
		t.Errorf("an unbindable rule must be reported as a gap:\n%s", src)
	}
}

func TestPresenceNamesUnionsRequiredAndDependent(t *testing.T) {
	fields := []structField{
		{jsonName: "id", required: true},
		{jsonName: "opens"},
		{jsonName: "closes"},
		{jsonName: "unrelated"},
	}
	schema := map[string]any{
		"dependentRequired": map[string]any{"opens": []any{"closes"}},
	}
	got := presenceNames(schema, fields)
	want := []string{"closes", "id", "opens"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("presenceNames = %v, want %v (required plus the rule's participants, and nothing else)", got, want)
	}
}
