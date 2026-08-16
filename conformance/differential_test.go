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

// target is one emitted Go type and the payloads to try against it.
//
// location and oracleID used to be the same string: a schema path both
// named the type and, through the $id map, addressed the schema to compile.
// A $def breaks that. It is located by path and def name — "rel#def", which
// is how models keys it and how a failure should name it — but the oracle
// knows it only as a JSON pointer into the containing document's $id.
// Deriving one from the other at each use, in three places, would put the
// same translation in three places to get wrong; carrying both is cheaper
// and says which is which.
//
// models is keyed by exactly the location a failure names, so there is one
// field rather than two identical ones. A second field claiming to be a
// different identifier, while always holding the same string, would be a
// distinction the code does not actually make.
type target struct {
	location string // what a failure names and models is keyed by: rel, or rel#def
	oracleID string // URL to compile: the document $id, or $id#/$defs/<name>
	cases    []payload
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

	// The oracle is built first because a target now has to carry the URL it
	// will be compiled by, and for a $def that URL is derived from the
	// containing document's $id rather than from the schema path.
	oracle, ids, err := newCorpusCompiler(files)
	if err != nil {
		t.Fatalf("register corpus with oracle: %v", err)
	}

	// Decide what to exercise, and what to skip and why.
	//
	// A target is one emitted Go type, not one schema file. Those used to be
	// the same thing here, and that identification is what hid the $defs: the
	// loop asked each file for its file-level type, and eight files — the
	// whole capability model, ap2_mandate, discount, fulfillment among them —
	// have none, because all their content lives in $defs. They were counted
	// as "no file-level type" and dropped, taking every type their $defs emit
	// out of the comparison.
	b := &builder{corpus: files}
	var targets []target
	skipped := map[string]int{}
	rootless := 0

	rels := make([]string, 0, len(files))
	for rel := range files {
		rels = append(rels, rel)
	}
	sort.Strings(rels)

	// consider turns one schema node into a target, or into a named skip.
	// Both the file-level type and each $def go through it, so a skip reason
	// means the same thing whichever produced it.
	consider := func(rel, location, oracleID string, node map[string]any) {
		if kw := usesOutOfScope(node, files, rel); kw != "" {
			// Documented as unenforced, so the oracle may legitimately
			// reject where we accept. Counted, never silent.
			skipped["out-of-scope keyword: "+kw]++
			return
		}
		cases := b.mutations(node, rel)
		if len(cases) == 0 {
			skipped["no object instance"]++
			return
		}
		targets = append(targets, target{
			location: location, oracleID: oracleID, cases: cases,
		})
	}

	for _, rel := range rels {
		schema := files[rel]
		id, ok := ids[rel]
		if !ok {
			t.Errorf("%s: schema has no $id to compile by", rel)
			continue
		}
		if _, ok := idx.Lookup(rel, ""); ok {
			consider(rel, rel, id, schema)
		} else {
			// Not a gap and not a skip: the file is a $defs container, and
			// the targets it contributes are the $defs below.
			rootless++
		}
		defs, _ := schema["$defs"].(map[string]any)
		names := make([]string, 0, len(defs))
		for name := range defs {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			if _, ok := idx.Lookup(rel, name); !ok {
				continue // a namespace grouping other schemas; no type emitted
			}
			node, ok := defs[name].(map[string]any)
			if !ok {
				continue
			}
			// The node is the $def, but rel stays the containing file: a
			// $def's relative $refs resolve against the document it sits in,
			// not against anything derived from its name.
			consider(rel, rel+"#"+name, id+"#/$defs/"+name, node)
		}
	}

	if len(targets) == 0 {
		t.Fatal("no schema was exercised; the harness is not testing anything")
	}

	// Ask the oracle for its verdicts, and compare.
	//
	// comparedTypes counts the targets a verdict was actually reached on,
	// not the targets built: a target the oracle cannot compile is already
	// tallied as a skip, and counting it here as well would let the same
	// target be reported as both exercised and skipped.
	var mismatches []string
	total, comparedTypes, comparedFiles := 0, 0, 0
	for _, tg := range targets {
		compiled, err := oracle.Compile(tg.oracleID)
		if err != nil {
			// capability.json, payment_handler.json and service.json each
			// ref "#/$defs/version" but define no such $def, so anything
			// reaching one of them is uncompilable. The goldens are
			// byte-identical to the official python preprocessor's output,
			// so this is inherited from upstream rather than introduced
			// here — counted and named, not silently passed over.
			skipped["oracle cannot compile: "+oracleSkipReason(err)]++
			continue
		}
		comparedTypes++
		if !strings.Contains(tg.location, "#") {
			comparedFiles++
		}
		for _, c := range tg.cases {
			total++
			var inst any
			if err := json.Unmarshal(c.json, &inst); err != nil {
				t.Fatalf("%s/%s: generated payload is not JSON: %v", tg.location, c.name, err)
			}
			oracleOK := compiled.Validate(inst) == nil
			make, ok := models[tg.location]
			if !ok {
				t.Fatalf("%s: no model registered; TestModelsCoverCorpus or "+
					"TestModelsCoverDefs should have caught this", tg.location)
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
					tg.location, c.name, c.json, oracleOK, sdkOK, why))
			}
		}
	}

	// The denominator is types, not files, and saying "schemas" for a number
	// that counts something else is the same dishonesty as an unnamed skip.
	// Both figures stay visible so neither can drift unnoticed.
	t.Logf("exercised %d payloads across %d types (%d schema files)", total, comparedTypes, comparedFiles)
	t.Logf("%d schema files have no file-level type and contribute $defs targets only", rootless)
	for _, reason := range sortedKeys(skipped) {
		t.Logf("skipped %3d targets: %s", skipped[reason], reason)
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

// oracleSkipReason turns a compiler error into a tally key that does not
// move between runs.
//
// The three files that ref a missing "#/$defs/version" are reached through
// each other, and which of the three URLs a given target trips over first
// depends on the compiler's internal map order. Keying the tally on the raw
// message therefore split the same 71 targets across three lines
// differently on every run: the total was stable, the attribution was
// noise. A figure that moves while the code stands still is not evidence of
// anything, so the resource half of the URL — the part that varies — is
// dropped and the pointer that is actually missing is what the line names.
func oracleSkipReason(err error) string {
	msg := firstLine(err.Error())
	open := strings.IndexByte(msg, '"')
	if open < 0 {
		return msg
	}
	rest := msg[open+1:]
	closed := strings.IndexByte(rest, '"')
	if closed < 0 {
		return msg
	}
	url := rest[:closed]
	if i := strings.IndexByte(url, '#'); i >= 0 {
		url = url[i:]
	}
	return msg[:open+1] + url + rest[closed:]
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
