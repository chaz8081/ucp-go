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
		"utf8.RuneCountInString(v.Name) > 8",
		"pattern_Thing_Name().MatchString(v.Name)",
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
