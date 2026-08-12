package conformance

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/chaz8081/ucp-go/cmd/ucpgen/emit"
	"github.com/chaz8081/ucp-go/cmd/ucpgen/preprocess"
)

// jsonMarshal is the encoder payloads.go builds cases with, named
// separately so that file needs no encoding/json import of its own.
func jsonMarshal(v any) ([]byte, error) { return json.Marshal(v) }

const probeModule = "github.com/chaz8081/ucp-go"

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
	repoRoot, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	goldens := filepath.Join(repoRoot, "goldens", goldenVersion)
	if _, err := os.Stat(goldens); err != nil {
		t.Skipf("goldens missing (%v)", err)
	}

	set, err := preprocess.LoadSchemasIncludingVariants(goldens)
	if err != nil {
		t.Fatalf("load goldens: %v", err)
	}
	idx, err := emit.BuildTypeIndex(set.Files, probeModule)
	if err != nil {
		t.Fatalf("build type index: %v", err)
	}

	out := generateTree(t, repoRoot, goldens)

	// Decide what to exercise, and what to skip and why.
	b := &builder{corpus: set.Files}
	type target struct {
		rel   string
		typ   emit.TypeRef
		cases []payload
	}
	var targets []target
	skipped := map[string]int{}

	rels := make([]string, 0, len(set.Files))
	for rel := range set.Files {
		rels = append(rels, rel)
	}
	sort.Strings(rels)

	for _, rel := range rels {
		schema := set.Files[rel]
		ref, ok := idx.Lookup(rel, "")
		if !ok {
			skipped["no file-level type"]++
			continue
		}
		if kw := usesOutOfScope(schema, set.Files, rel); kw != "" {
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

	rels = rels[:0]
	for _, tg := range targets {
		rels = append(rels, tg.rel)
	}
	writeProbe(t, out, rels, idx)

	// Ask the generated models for their verdicts.
	var in bytes.Buffer
	for _, tg := range targets {
		for _, c := range tg.cases {
			line, _ := json.Marshal(map[string]any{
				"schema":  tg.rel,
				"payload": json.RawMessage(c.json),
			})
			in.Write(line)
			in.WriteByte('\n')
		}
	}
	sdk := runProbe(t, out, &in)

	// Ask the oracle for its verdicts, and compare.
	oracle, ids, err := newCorpusCompiler(set.Files)
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
			verdict, ok := sdk[tg.rel+"\x00"+string(c.json)]
			if !ok {
				t.Fatalf("%s/%s: probe returned no verdict", tg.rel, c.name)
			}
			if oracleOK != verdict.OK {
				mismatches = append(mismatches, fmt.Sprintf(
					"%s [%s]\n    payload: %s\n    oracle=%v sdk=%v (%s)",
					tg.rel, c.name, c.json, oracleOK, verdict.OK, verdict.Err))
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

// generateTree emits the whole corpus into a temporary module.
func generateTree(t *testing.T, repoRoot, goldens string) string {
	t.Helper()
	out := t.TempDir()
	gen := exec.Command("go", "run", "./cmd/ucpgen", "emit",
		"-schemas", goldens, "-out", out, "-spec-ref", "differential")
	gen.Dir = repoRoot
	if b, err := gen.CombinedOutput(); err != nil {
		t.Fatalf("emit failed: %v\n%s", err, b)
	}
	mod := "module " + probeModule + "\n\ngo 1.24\n"
	if err := os.WriteFile(filepath.Join(out, "go.mod"), []byte(mod), 0o644); err != nil {
		t.Fatal(err)
	}
	return out
}

// FuzzValidateAgreement keeps searching after the table-driven cases pass.
//
// It fixes one schema rather than sweeping the corpus: the probe is a
// separate process, so a per-input round trip would dominate the run and
// the fuzzer would explore almost nothing. link.json is the schema the
// mirrored Link in oracle_test.go tracks, which is what lets the SDK side
// run in-process here.
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

// writeProbe generates the program that gives the SDK's verdicts.
//
// It lives inside the generated module because that is the only place the
// generated packages can be imported from: they are not committed, so this
// module cannot import them directly. The probe speaks JSON lines over
// stdin and stdout, which keeps the oracle and its dependency on this side
// of the boundary and the generated code, with its zero-dependency
// guarantee, on the other.
func writeProbe(t *testing.T, tree string, rels []string, idx *emit.TypeIndex) {
	t.Helper()

	imports := map[string]string{} // import path -> package name
	var body strings.Builder
	for _, rel := range rels {
		ref, ok := idx.Lookup(rel, "")
		if !ok {
			continue
		}
		qualified := ref.Name
		if ref.ImportPath != probeModule {
			imports[ref.ImportPath] = ref.Package
			qualified = ref.Package + "." + ref.Name
		} else {
			imports[probeModule] = "ucp"
			qualified = "ucp." + ref.Name
		}
		fmt.Fprintf(&body, "\t%q: func() validator { return new(%s) },\n", rel, qualified)
	}

	var src strings.Builder
	src.WriteString("package main\n\nimport (\n\t\"bufio\"\n\t\"encoding/json\"\n\t\"fmt\"\n\t\"os\"\n")
	for _, p := range sortedKeys(imports) {
		fmt.Fprintf(&src, "\t%s %q\n", imports[p], p)
	}
	src.WriteString(")\n\n")
	src.WriteString(`// validator is the uniform interface every generated type satisfies.
type validator interface{ Validate() error }

// models maps a schema to a fresh value of the Go type it produces.
var models = map[string]func() validator{
`)
	src.WriteString(body.String())
	src.WriteString(`}

type request struct {
	Schema  string          ` + "`json:\"schema\"`" + `
	Payload json.RawMessage ` + "`json:\"payload\"`" + `
}

func main() {
	in := bufio.NewScanner(os.Stdin)
	in.Buffer(make([]byte, 0, 1<<20), 1<<24)
	out := bufio.NewWriter(os.Stdout)
	defer out.Flush()

	for in.Scan() {
		line := in.Bytes()
		if len(line) == 0 {
			continue
		}
		var req request
		if err := json.Unmarshal(line, &req); err != nil {
			fmt.Fprintf(os.Stderr, "bad request %s: %v\n", line, err)
			os.Exit(1)
		}
		make, ok := models[req.Schema]
		if !ok {
			fmt.Fprintf(os.Stderr, "no model for %s\n", req.Schema)
			os.Exit(1)
		}
		v := make()
		ok, msg := true, ""
		// A decode failure is a verdict too: the payload did not fit the
		// type the schema describes, which is a rejection just as much as a
		// failed constraint is.
		if err := json.Unmarshal(req.Payload, v); err != nil {
			ok, msg = false, "decode: "+err.Error()
		} else if err := v.Validate(); err != nil {
			ok, msg = false, err.Error()
		}
		res, err := json.Marshal(map[string]any{
			"schema":  req.Schema,
			"payload": string(req.Payload),
			"ok":      ok,
			"err":     msg,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "marshal verdict: %v\n", err)
			os.Exit(1)
		}
		out.Write(res)
		out.WriteByte('\n')
	}
	if err := in.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "read: %v\n", err)
		os.Exit(1)
	}
}
`)

	dir := filepath.Join(tree, "probe")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(src.String()), 0o644); err != nil {
		t.Fatal(err)
	}
}

// verdict is what the probe reports for one payload.
type verdict struct {
	Schema  string `json:"schema"`
	Payload string `json:"payload"`
	OK      bool   `json:"ok"`
	Err     string `json:"err"`
}

// runProbe builds and runs the probe, returning its verdicts keyed by
// schema and payload.
func runProbe(t *testing.T, tree string, in *bytes.Buffer) map[string]verdict {
	t.Helper()
	cmd := exec.Command("go", "run", "./probe")
	cmd.Dir = tree
	cmd.Stdin = in
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("probe failed: %v\n%s", err, stderr.String())
	}
	out := map[string]verdict{}
	for _, line := range strings.Split(strings.TrimSpace(stdout.String()), "\n") {
		if line == "" {
			continue
		}
		var v verdict
		if err := json.Unmarshal([]byte(line), &v); err != nil {
			t.Fatalf("probe emitted a bad line %q: %v", line, err)
		}
		out[v.Schema+"\x00"+v.Payload] = v
	}
	return out
}
