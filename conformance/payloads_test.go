package conformance

import (
	"fmt"
	"testing"
)

// TestOutOfScopeScanFollowsDeepRefChains pins a regression that turned a
// skip into a false pass.
//
// An earlier depth cap of 12 stopped the reference walk mid-corpus.
// fulfillment_method.json reached total.json's conditionals at exactly
// depth 12 through fulfillment_group and fulfillment_option; when upstream
// began preserving conditional allOf branches, the keyword moved one level
// deeper and the cap hid it. The harness then exercised a schema whose
// verdict it could not legitimately predict, and passed by luck. The cap
// had already been hiding four other schemas.
//
// The chain is synthetic rather than drawn from the corpus. Phase 6
// implemented the conditionals that used to make the real chain
// out-of-scope, so pinning this to whichever keyword happens to be
// unimplemented today would decay again the next time one is implemented.
// What must not regress is the walk's willingness to follow a $ref chain
// of any length, and that is what this measures.
func TestOutOfScopeScanFollowsDeepRefChains(t *testing.T) {
	const depth = 40
	corpus := map[string]map[string]any{}
	for i := range depth {
		corpus[fmt.Sprintf("link%d.json", i)] = map[string]any{
			"type": "object",
			"properties": map[string]any{
				"next": map[string]any{"$ref": fmt.Sprintf("link%d.json", i+1)},
			},
		}
	}
	// Only the far end of the chain carries something out of scope.
	corpus[fmt.Sprintf("link%d.json", depth)] = map[string]any{
		"type":              "object",
		"patternProperties": map[string]any{"^x-": map[string]any{"type": "string"}},
	}

	if kw := usesOutOfScope(corpus["link0.json"], corpus, "link0.json"); kw != "patternProperties" {
		t.Errorf("scan of a %d-link ref chain returned %q, want %q; the walk is stopping short",
			depth, kw, "patternProperties")
	}
}

// A schema reaching nothing out of scope must be exercised, not skipped.
// The counterpart to the test above: a walk that flagged everything would
// also never regress, and would also be useless.
func TestOutOfScopeScanClearsAnOrdinaryChain(t *testing.T) {
	corpus := map[string]map[string]any{
		"a.json": {
			"type":       "object",
			"properties": map[string]any{"next": map[string]any{"$ref": "b.json"}},
		},
		"b.json": {
			"type":       "object",
			"properties": map[string]any{"name": map[string]any{"type": "string"}},
		},
	}
	if kw := usesOutOfScope(corpus["a.json"], corpus, "a.json"); kw != "" {
		t.Errorf("ordinary chain flagged as out of scope: %q", kw)
	}
}

// The harness had the same empty-map bug as the emitter, which is why four
// broken types were reported as a skip line instead of a failure.
func TestUnmodeledUnionIgnoresEmptyProperties(t *testing.T) {
	empty := map[string]any{
		"type":       "object",
		"properties": map[string]any{},
		"oneOf": []any{
			map[string]any{"$ref": "a.json"},
			map[string]any{"$ref": "b.json"},
		},
	}
	if hasUnmodeledUnion(empty) {
		t.Error("a union with an empty properties map has no sibling properties to be unmodeled")
	}

	real := map[string]any{
		"type":       "object",
		"properties": map[string]any{"id": map[string]any{"type": "string"}},
		"oneOf": []any{
			map[string]any{"$ref": "a.json"},
			map[string]any{"$ref": "b.json"},
		},
	}
	if !hasUnmodeledUnion(real) {
		t.Error("a union alongside real properties is still unmodeled")
	}
}

func TestMutationsCoverUnionAlternatives(t *testing.T) {
	corpus := map[string]map[string]any{
		"a.json": {
			"$id": "https://x/a.json", "type": "object",
			"required":   []any{"code"},
			"properties": map[string]any{"code": map[string]any{"type": "string"}},
		},
		"b.json": {
			"$id": "https://x/b.json", "type": "object",
			"required":   []any{"text"},
			"properties": map[string]any{"text": map[string]any{"type": "string"}},
		},
		"u.json": {
			"$id": "https://x/u.json", "type": "object",
			"properties": map[string]any{},
			"oneOf": []any{
				map[string]any{"$ref": "a.json"},
				map[string]any{"$ref": "b.json"},
			},
		},
	}
	b := builder{corpus: corpus}
	got := b.mutations(corpus["u.json"], "u.json")
	if len(got) == 0 {
		t.Fatal("a union-rooted schema produced no payloads")
	}
	names := map[string]bool{}
	for _, p := range got {
		names[p.name] = true
	}
	for _, want := range []string{"alternative:0", "alternative:1", "matches-no-alternative"} {
		if !names[want] {
			t.Errorf("missing payload %q; got %v", want, names)
		}
	}
}

func TestMutationsCoverArrayRoots(t *testing.T) {
	corpus := map[string]map[string]any{
		"t.json": {
			"$id": "https://x/t.json", "type": "array",
			"items": map[string]any{
				"type":       "object",
				"required":   []any{"type"},
				"properties": map[string]any{"type": map[string]any{"type": "string"}},
			},
			"contains": map[string]any{
				"properties": map[string]any{"type": map[string]any{"const": "subtotal"}},
				"required":   []any{"type"},
			},
			"minContains": float64(1),
			"maxContains": float64(1),
		},
	}
	b := builder{corpus: corpus}
	got := b.mutations(corpus["t.json"], "t.json")
	names := map[string]bool{}
	for _, p := range got {
		names[p.name] = true
	}
	for _, want := range []string{"base", "empty-array", "too-few-matching", "too-many-matching"} {
		if !names[want] {
			t.Errorf("missing payload %q; got %v", want, names)
		}
	}
}

func TestMutationsCoverScalarRoots(t *testing.T) {
	corpus := map[string]map[string]any{
		"r.json": {
			"$id": "https://x/r.json", "type": "string",
			"pattern":   "^[a-z]+\\.[a-z]+$",
			"maxLength": float64(64),
		},
		"c.json": {
			"$id": "https://x/c.json", "type": "string",
			"enum": []any{"ok", "failed"},
		},
		"n.json": {
			"$id": "https://x/n.json", "type": "integer",
			"minimum": float64(0),
		},
	}
	b := builder{corpus: corpus}
	for rel, want := range map[string][]string{
		"r.json": {"base", "bad-pattern", "wrong-json-type"},
		"c.json": {"base", "bad-enum", "wrong-json-type"},
		"n.json": {"base", "below-minimum", "wrong-json-type"},
	} {
		names := map[string]bool{}
		for _, p := range b.mutations(corpus[rel], rel) {
			names[p.name] = true
		}
		for _, w := range want {
			if !names[w] {
				t.Errorf("%s: missing payload %q; got %v", rel, w, names)
			}
		}
	}
}
