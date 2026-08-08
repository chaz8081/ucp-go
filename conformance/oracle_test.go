// Package conformance verifies generated models against the canonical
// JSON Schemas using a draft-2020-12 validator as the oracle.
package conformance

import (
	"encoding/json"
	"errors"
	"os"
	"regexp"
	"sync"
	"testing"
	"unicode/utf8"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

const fixtureSchema = "../cmd/ucpgen/preprocess/testdata/schemas/test/link.json"

// Link mirrors the struct ucpgen emits for the fixture schema. Once
// Phase 2 wires real generated packages, this local copy is replaced by
// an import of the generated code.
type Link struct {
	Title *string `json:"title,omitempty"`
	// Link relation type.
	Type string `json:"type"`
	// Target URL.
	URL string `json:"url"`
}

// The following is copied verbatim from ucpgen output for test/link.json;
// replaced by generated import in Phase 2.
var pattern_Link_Title = sync.OnceValue(func() *regexp.Regexp { return regexp.MustCompile("^[^\\n]*$") })

// Validate reports the first constraint violation, or nil.
func (v *Link) Validate() error {
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
	c := jsonschema.NewCompiler()
	sch, err := c.Compile(fixtureSchema)
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
