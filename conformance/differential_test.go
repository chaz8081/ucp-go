package conformance

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/chaz8081/ucp-go/cmd/ucpgen/emit"
	"github.com/chaz8081/ucp-go/cmd/ucpgen/preprocess"
	"github.com/chaz8081/ucp-go/shopping/types"
)

// loadGoldens reads the committed corpus.
func loadGoldens(t testing.TB) map[string]map[string]any {
	t.Helper()
	repoRoot, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	set, err := preprocess.LoadSchemasIncludingVariants(filepath.Join(repoRoot, "goldens", goldenVersion))
	if err != nil {
		t.Fatalf("load goldens: %v", err)
	}
	return set.Files
}

// buildIndex resolves what Go type each schema produces.
func buildIndex(t testing.TB, files map[string]map[string]any) *emit.TypeIndex {
	t.Helper()
	idx, err := emit.BuildTypeIndex(files, "github.com/chaz8081/ucp-go")
	if err != nil {
		t.Fatalf("build type index: %v", err)
	}
	return idx
}

// TestDifferentialAgreement drives the same JSON bytes through the
// generated models and through a real draft-2020-12 validator, and
// requires the two to reach the same verdict.
//
// This is the check the rest of the suite cannot make. A golden test proves
// the emitter is reproducible and a round-trip test proves the types
// decode; neither says whether Validate agrees with the schema. Only a
// comparison against an independent implementation does, which is also why
// the oracle compiles patterns with ECMA-262 semantics rather than the RE2
// the generated code uses — otherwise pattern agreement would be true by
// construction.
//
// Both sides are driven from the same bytes, never from a Go value: an
// invalid UTF-8 sequence in a Go string is rewritten to U+FFFD on marshal,
// which shifts rune counts and would manufacture maxLength disagreements
// that exist nowhere in the protocol.
func TestDifferentialAgreement(t *testing.T) {
	files := loadGoldens(t)
	idx := buildIndex(t, files)

	// Decide what to exercise, and what to skip and why.
	b := &builder{corpus: files}
	type target struct {
		rel   string
		typ   emit.TypeRef
		cases []payload
	}
	var targets []target
	skipped := map[string]int{}

	rels := make([]string, 0, len(files))
	for rel := range files {
		rels = append(rels, rel)
	}
	sort.Strings(rels)

	for _, rel := range rels {
		schema := files[rel]
		ref, ok := idx.Lookup(rel, "")
		if !ok {
			skipped["no file-level type"]++
			continue
		}
		if kw := usesOutOfScope(schema, files, rel); kw != "" {
			// Documented as unenforced, so the oracle may legitimately
			// reject where we accept. Counted, never silent.
			skipped["out-of-scope keyword: "+kw]++
			continue
		}
		cases := b.mutations(schema, rel)
		if len(cases) == 0 {
			skipped["no object instance"]++
			continue
		}
		targets = append(targets, target{rel: rel, typ: ref, cases: cases})
	}

	if len(targets) == 0 {
		t.Fatal("no schema was exercised; the harness is not testing anything")
	}

	// Ask the oracle for its verdicts, and compare.
	oracle, ids, err := newCorpusCompiler(files)
	if err != nil {
		t.Fatalf("register corpus with oracle: %v", err)
	}
	var mismatches []string
	total := 0
	for _, tg := range targets {
		id, ok := ids[tg.rel]
		if !ok {
			t.Errorf("%s: schema has no $id to compile by", tg.rel)
			continue
		}
		compiled, err := oracle.Compile(id)
		if err != nil {
			// capability.json refs "#/$defs/version" but defines no such
			// $def, so anything reaching it is uncompilable. The goldens are
			// byte-identical to the official python preprocessor's output,
			// so this is inherited from upstream rather than introduced
			// here — counted and named, not silently passed over.
			skipped["oracle cannot compile: "+firstLine(err.Error())]++
			continue
		}
		for _, c := range tg.cases {
			total++
			var inst any
			if err := json.Unmarshal(c.json, &inst); err != nil {
				t.Fatalf("%s/%s: generated payload is not JSON: %v", tg.rel, c.name, err)
			}
			oracleOK := compiled.Validate(inst) == nil
			make, ok := models[tg.rel]
			if !ok {
				t.Fatalf("%s: no model registered; TestModelsCoverCorpus should have caught this", tg.rel)
			}
			v := make()
			// A decode failure is a verdict too: the payload did not fit the
			// type the schema describes, which is a rejection just as much as
			// a failed constraint is. Driven from the same bytes as the
			// oracle, never from a Go value — re-marshaling would rewrite
			// invalid UTF-8 to U+FFFD and shift the rune counts maxLength is
			// measured in.
			//
			// The reason is kept, not just the verdict: a disagreement has to
			// be triaged into a missing check, a wrong check, or an
			// out-of-scope keyword, and "sdk=false" alone does not say which.
			sdkErr := json.Unmarshal(c.json, v)
			if sdkErr == nil {
				sdkErr = v.Validate()
			}
			sdkOK := sdkErr == nil
			if oracleOK != sdkOK {
				why := "accepted it"
				if sdkErr != nil {
					why = sdkErr.Error()
				}
				mismatches = append(mismatches, fmt.Sprintf(
					"%s [%s]\n    payload: %s\n    oracle=%v sdk=%v (%s)",
					tg.rel, c.name, c.json, oracleOK, sdkOK, why))
			}
		}
	}

	t.Logf("exercised %d payloads across %d schemas", total, len(targets))
	for _, reason := range sortedKeys(skipped) {
		t.Logf("skipped %3d schemas: %s", skipped[reason], reason)
	}
	if len(mismatches) > 0 {
		sort.Strings(mismatches)
		shown := mismatches
		if len(shown) > 25 {
			shown = shown[:25]
		}
		t.Errorf("%d of %d payloads disagree:\n%s",
			len(mismatches), total, strings.Join(shown, "\n"))
	}
}

// firstLine keeps a multi-line library error readable in a skip tally.
func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// FuzzValidateAgreement keeps searching after the table-driven cases pass.
//
// It fixes one schema rather than sweeping the corpus: it targets the
// hand-written fixture at cmd/ucpgen/preprocess/testdata/schemas/test/link.json
// because that is where the maxLength and pattern constraints live — the
// shipped shopping/types/link.json carries neither (it uses format: uri
// instead), so exercising it here is the only way this harness reaches that
// constraint-emission code at all. link.json is the schema the mirrored
// Link in oracle_test.go tracks, which is what lets the SDK side run
// in-process here.
func FuzzValidateAgreement(f *testing.F) {
	for _, seed := range []string{
		`{}`,
		`{"type":"terms","url":"https://example.test/terms"}`,
		`{"type":"terms","url":"https://example.test/terms","title":"Terms"}`,
		`{"type":"terms","url":"https://example.test/terms","title":"a\nb"}`,
		`{"type":"terms"}`,
		`{"url":"https://example.test/terms"}`,
		`{"type":1}`,
		`[]`,
		`null`,
	} {
		f.Add([]byte(seed))
	}

	compiled, err := newCompiler().Compile(fixtureSchema)
	if err != nil {
		f.Fatalf("compile fixture: %v", err)
	}

	f.Fuzz(func(t *testing.T, payload []byte) {
		var inst any
		if err := json.Unmarshal(payload, &inst); err != nil {
			return // not JSON: neither side is being asked anything
		}
		oracleOK := compiled.Validate(inst) == nil

		// Driven from the same bytes as the oracle, never from a Go value:
		// re-marshaling would rewrite invalid UTF-8 to U+FFFD and shift the
		// rune counts maxLength is measured in.
		var l Link
		sdkOK := json.Unmarshal(payload, &l) == nil && l.Validate() == nil

		if oracleOK != sdkOK {
			t.Errorf("verdict drift on %s: oracle=%v sdk=%v", payload, oracleOK, sdkOK)
		}
	})
}

// FuzzReverseDomainNameAgreement fuzzes a type that actually ships.
// FuzzValidateAgreement targets a test fixture, because that is where the
// maxLength and pattern constraints live; reverse_domain_name.json is the
// corpus's own pattern-carrying type, so this covers generated code a
// consumer would really call.
func FuzzReverseDomainNameAgreement(f *testing.F) {
	for _, seed := range []string{
		`"dev.ucp.buyer_ip"`, `"com.example.device_id"`, `"nodots"`,
		`"UPPER.case"`, `""`, `"a.b"`, `"1.2"`, `null`, `[]`, `{}`,
	} {
		f.Add([]byte(seed))
	}

	corpus := loadGoldens(f)
	oracle, ids, err := newCorpusCompiler(corpus)
	if err != nil {
		f.Fatalf("register corpus: %v", err)
	}
	const rel = "shopping/types/reverse_domain_name.json"
	compiled, err := oracle.Compile(ids[rel])
	if err != nil {
		f.Fatalf("compile %s: %v", rel, err)
	}

	f.Fuzz(func(t *testing.T, payload []byte) {
		var inst any
		if err := json.Unmarshal(payload, &inst); err != nil {
			return // not JSON: neither side is being asked anything
		}
		oracleOK := compiled.Validate(inst) == nil

		var v types.ReverseDomainName
		sdkOK := json.Unmarshal(payload, &v) == nil && v.Validate() == nil

		if oracleOK != sdkOK {
			t.Errorf("verdict drift on %s: oracle=%v sdk=%v", payload, oracleOK, sdkOK)
		}
	})
}
