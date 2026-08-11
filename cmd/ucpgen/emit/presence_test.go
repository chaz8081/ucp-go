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

// A schema with no required properties needs no presence machinery at all.
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
	if strings.Contains(src, "UnmarshalJSON") {
		t.Errorf("a closed object with no required properties needs no decoder:\n%s", src)
	}
}
