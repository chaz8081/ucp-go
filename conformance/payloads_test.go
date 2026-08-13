package conformance

import "testing"

// TestOutOfScopeScanFollowsDeepRefChains pins a regression that turned a
// skip into a false pass.
//
// fulfillment_method.json declares no conditional of its own. It reaches
// one through fulfillment_group -> fulfillment_option -> total.json, and
// total.json's conditionals are out of scope, so the oracle can reject
// where the generated code accepts. The scanner must see that.
//
// An earlier depth cap of 12 found the keyword at exactly depth 12. When
// upstream started preserving conditional allOf branches, the keyword moved
// one level deeper and the cap hid it — the harness then exercised the
// schema as though its verdict were predictable.
func TestOutOfScopeScanFollowsDeepRefChains(t *testing.T) {
	corpus := loadGoldens(t)
	const rel = "shopping/types/fulfillment_method.json"
	schema, ok := corpus[rel]
	if !ok {
		t.Fatalf("%s missing from the corpus", rel)
	}
	if kw := usesOutOfScope(schema, corpus, rel); kw == "" {
		t.Errorf("%s transitively reaches conditional logic but was not flagged; "+
			"the reference walk is stopping short", rel)
	}
}
