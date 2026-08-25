package conformance

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// recordedUnenforced reads MANIFEST.json and returns, per schema path, the
// validation-only keywords the emitter reports it does not check.
//
// The differential harness already skips schemas using a keyword that is
// out of scope *everywhere* (`usesOutOfScope`). This is the finer case: a
// keyword the emitter implements in general but cannot compile at one
// particular node. `contains` on `totals_create_request` is the live
// example — every property of `total.json` is dropped for the create op, so
// the element type is `map[string]any` and there is no field for a
// predicate to test.
//
// Reading it from the manifest rather than from a list here is deliberate.
// The manifest is the record the README quotes and the doc comments mirror,
// so a skip taken on this basis is one the project has already published as
// a gap. A hand-maintained list in the harness could drift away from what
// the models actually enforce, and would then be excusing failures nobody
// had disclosed.
func recordedUnenforced() (map[string]map[string]bool, error) {
	raw, err := os.ReadFile(filepath.Join("..", "MANIFEST.json"))
	if err != nil {
		return nil, err
	}
	var m struct {
		Schemas map[string]struct {
			Unenforced map[string][]string `json:"unenforced"`
		} `json:"schemas"`
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	out := make(map[string]map[string]bool, len(m.Schemas))
	for rel, entry := range m.Schemas {
		for _, kws := range entry.Unenforced {
			for _, kw := range kws {
				if out[rel] == nil {
					out[rel] = map[string]bool{}
				}
				out[rel][kw] = true
			}
		}
	}
	return out, nil
}

// unenforcedOnNode returns the first keyword (sorted, for determinism) that
// node declares and that the manifest records as unenforced for rel, or "".
//
// Both halves are required: the manifest entry proves the gap is disclosed,
// and the node's own keys prove the gap is what this particular target
// would trip over. Skipping on the manifest entry alone would excuse a
// target that never uses the keyword.
func unenforcedOnNode(rel string, node map[string]any, recorded map[string]map[string]bool) string {
	kws := recorded[rel]
	if len(kws) == 0 {
		return ""
	}
	// Sorted, so which keyword names the skip does not depend on map order.
	for _, kw := range []string{
		"contains", "maxContains", "minContains",
		"dependentRequired", "dependentSchemas", "else", "if", "then",
		"not", "propertyNames", "enum", "const",
	} {
		if !kws[kw] {
			continue
		}
		if _, declared := node[kw]; declared {
			return kw
		}
	}
	return ""
}
