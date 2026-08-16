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
