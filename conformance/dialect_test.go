package conformance

import (
	"testing"
)

// The emitter classifies `format` as an annotation rather than an unmet
// obligation, and the oracle compiles without AssertFormat. Both rest on
// one fact about the corpus: it uses the plain draft 2020-12 dialect, where
// format is annotation-only, and never opts in to the Format-Assertion
// vocabulary.
//
// That fact is upstream's to change, not ours. If a future spec release
// declared the assertion vocabulary, `format` would become a real
// obligation, the manifest's not_asserted entries would silently become
// understatements, and the SDK would be laxer than the schemas require
// while still reporting full coverage. This test makes that a build
// failure rather than a discovery.
func TestCorpusUsesAnnotationOnlyFormat(t *testing.T) {
	const dialect2020 = "https://json-schema.org/draft/2020-12/schema"

	files := loadGoldens(t)
	if len(files) == 0 {
		t.Fatal("no goldens loaded; this test would pass vacuously")
	}

	declaredDialect := 0
	for name, doc := range files {
		if v, ok := doc["$vocabulary"]; ok {
			t.Errorf("%s declares $vocabulary (%v): format may no longer be "+
				"annotation-only, so emit.annotationOnlyKeywords and the "+
				"oracle's AssertFormat setting both need review", name, v)
		}
		schema, ok := doc["$schema"]
		if !ok {
			// Most files inherit the dialect from the resource that
			// references them; only a declared one can be wrong.
			continue
		}
		declaredDialect++
		if schema != dialect2020 {
			t.Errorf("%s declares $schema %q, want %q: a different dialect may "+
				"assert format", name, schema, dialect2020)
		}
	}
	if declaredDialect == 0 {
		t.Error("no schema declared a $schema; the dialect check proved nothing")
	}
}
