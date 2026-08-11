package conformance

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"
)

const fixtureSchema = "../cmd/ucpgen/preprocess/testdata/schemas/test/link.json"

// Link mirrors the struct ucpgen emits for the fixture schema. Once
// Phase 2 wires real generated packages, this local copy is replaced by
// an import of the generated code.
type Link struct {
	Title *string `json:"title,omitzero"`
	// Link relation type.
	Type string `json:"type"`
	// Target URL.
	URL string `json:"url"`

	Extra map[string]json.RawMessage `json:"-"`

	present map[string]bool
}

// UnmarshalJSON is copied from ucpgen output. The oracle harness drives
// both sides from JSON bytes, so the SDK side has to decode through the
// generated codec — that is where the required-property presence the
// mirrored Validate reads on gets recorded.
func (v *Link) UnmarshalJSON(data []byte) error {
	type LinkAlias Link
	var named LinkAlias
	if err := json.Unmarshal(data, &named); err != nil {
		return err
	}
	*v = Link(named)

	var all map[string]json.RawMessage
	if err := json.Unmarshal(data, &all); err != nil {
		return err
	}
	v.present = make(map[string]bool, 2)
	for _, name := range []string{"type", "url"} {
		if _, ok := all[name]; ok {
			v.present[name] = true
		}
	}
	delete(all, "title")
	delete(all, "type")
	delete(all, "url")
	if len(all) > 0 {
		v.Extra = all
	}
	return nil
}

// The following is copied verbatim from ucpgen output for test/link.json;
// replaced by generated import in Phase 2.
var pattern_Link_Title = sync.OnceValue(func() *regexp.Regexp { return regexp.MustCompile("^[^\\n]*$") })

// Validate reports the first constraint violation, or nil.
func (v *Link) Validate() error {
	if v.present != nil {
		if !v.present["type"] {
			return errors.New("type: required property is missing")
		}
		if !v.present["url"] {
			return errors.New("url: required property is missing")
		}
	}
	if v.Title != nil && !pattern_Link_Title().MatchString(*v.Title) {
		return errors.New("title: does not match pattern")
	}
	if utf8.RuneCountInString(v.URL) > 2048 {
		return errors.New("url: exceeds maxLength 2048")
	}
	return nil
}

func oracleVerdict(t *testing.T, payload []byte) bool {
	t.Helper()
	sch, err := newCompiler().Compile(fixtureSchema)
	if err != nil {
		t.Fatalf("compile schema: %v", err)
	}
	var inst any
	if err := json.Unmarshal(payload, &inst); err != nil {
		t.Fatalf("payload: %v", err)
	}
	return sch.Validate(inst) == nil
}

func sdkVerdict(t *testing.T, payload []byte) bool {
	t.Helper()
	var l Link
	if err := json.Unmarshal(payload, &l); err != nil {
		return false
	}
	return l.Validate() == nil
}

// canonicalPatternVar and canonicalValidate are copied verbatim from the
// Link mirror above (itself copied verbatim from ucpgen's own output for
// test/link.json). TestGeneratedOutputMatchesMirror runs the real
// generator against that same fixture and asserts its output contains
// these exact lines — so if the emitter ever changes what it generates
// for maxLength/pattern constraints, this test fails instead of letting
// the hand-copied mirror silently drift out of sync with reality.
const (
	canonicalPatternVar = `var pattern_Link_Title = sync.OnceValue(func() *regexp.Regexp { return regexp.MustCompile("^[^\\n]*$") })`
	canonicalValidate   = `// Validate reports the first constraint violation, or nil.
func (v *Link) Validate() error {
	if v.present != nil {
		if !v.present["type"] {
			return errors.New("type: required property is missing")
		}
		if !v.present["url"] {
			return errors.New("url: required property is missing")
		}
	}
	if v.Title != nil && !pattern_Link_Title().MatchString(*v.Title) {
		return errors.New("title: does not match pattern")
	}
	if utf8.RuneCountInString(v.URL) > 2048 {
		return errors.New("url: exceeds maxLength 2048")
	}
	return nil
}`
	canonicalPresenceCapture = `	v.present = make(map[string]bool, 2)
	for _, name := range []string{"type", "url"} {
		if _, ok := all[name]; ok {
			v.present[name] = true
		}
	}`
)

func TestGeneratedOutputMatchesMirror(t *testing.T) {
	outDir := t.TempDir()

	// cmd/ucpgen belongs to the root module, not this conformance module
	// (see the go.mod comment on the replace directive), so `go run` must
	// execute with its working directory at the repo root — invoking it
	// from here (conformance/'s own module) fails to resolve the package.
	rootDir, err := filepath.Abs("..")
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}

	cmd := exec.Command("go", "run", "./cmd/ucpgen", "emit",
		"-schemas", "./cmd/ucpgen/preprocess/testdata/schemas",
		"-out", outDir,
		"-spec-ref", "drift-check",
	)
	cmd.Dir = rootDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go run ./cmd/ucpgen emit: %v\n%s", err, out)
	}

	generated, err := os.ReadFile(filepath.Join(outDir, "test", "link.go"))
	if err != nil {
		t.Fatalf("read generated test/link.go: %v", err)
	}
	src := string(generated)

	for _, want := range []string{canonicalPatternVar, canonicalValidate, canonicalPresenceCapture} {
		if !strings.Contains(src, want) {
			t.Errorf("ucpgen's real output for test/link.json no longer contains:\n%s\n\nthe Link mirror in this file is stale — update it to match\n---\ngenerated:\n%s", want, src)
		}
	}
}

func TestOracleAgreement(t *testing.T) {
	for _, tc := range []struct{ name, path string }{
		{"valid", "testdata/link_valid.json"},
		{"invalid", "testdata/link_invalid.json"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			payload, err := os.ReadFile(tc.path)
			if err != nil {
				t.Fatal(err)
			}
			oracle, sdk := oracleVerdict(t, payload), sdkVerdict(t, payload)
			if oracle != sdk {
				t.Errorf("verdict drift: oracle=%v sdk=%v", oracle, sdk)
			}
		})
	}
}
