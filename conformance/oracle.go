// Package conformance verifies generated models against the canonical JSON
// Schemas using a draft-2020-12 validator as the oracle.
package conformance

import (
	"fmt"
	"path/filepath"

	"github.com/dlclark/regexp2"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

// ecmaRegexp compiles a pattern with ECMA-262 semantics, which is what JSON
// Schema specifies. jsonschema's default engine is Go's regexp (RE2); using
// it would make the oracle agree with the generated code by construction on
// every pattern, since the generated code is also RE2. The differences are
// real — \s covers the vertical tab and NBSP in ECMA-262 but not in RE2,
// and lookarounds are valid ECMA-262 — so the oracle compiles patterns the
// way the spec means them.
func ecmaRegexp(pattern string) (jsonschema.Regexp, error) {
	re, err := regexp2.Compile(pattern, regexp2.ECMAScript)
	if err != nil {
		return nil, err
	}
	return ecmaMatcher{re: re, pattern: pattern}, nil
}

// ecmaMatcher adapts regexp2 to jsonschema.Regexp, whose MatchString
// returns a bare bool.
type ecmaMatcher struct {
	re      *regexp2.Regexp
	pattern string
}

// MatchString reports whether the pattern matches anywhere in s. JSON
// Schema's `pattern` is an unanchored search. A match error (only possible
// on catastrophic backtracking, which regexp2 reports rather than hangs on)
// counts as no match.
func (m ecmaMatcher) MatchString(s string) bool {
	ok, err := m.re.MatchString(s)
	return err == nil && ok
}

func (m ecmaMatcher) String() string { return m.pattern }

// newCompiler returns a compiler configured the way every conformance check
// should use it.
func newCompiler() *jsonschema.Compiler {
	c := jsonschema.NewCompiler()
	c.UseRegexpEngine(ecmaRegexp)
	return c
}

// newCorpusCompiler returns a compiler with every schema in a corpus
// already registered under its own $id.
//
// The spec's schemas identify themselves by https URLs and reference each
// other relatively, so those references resolve against the $id rather than
// against the file on disk. Without registering them the compiler would try
// to fetch ucp.dev over the network — which would make the conformance
// suite depend on the internet, and silently test a different version of
// the spec than the goldens do.
func newCorpusCompiler(corpus map[string]map[string]any) (*jsonschema.Compiler, map[string]string, error) {
	c := newCompiler()
	ids := make(map[string]string, len(corpus))
	for rel, doc := range corpus {
		id := schemaBaseURI + filepath.ToSlash(rel)
		// python-sdk 3e1aace strips $id from preprocessed output so its own
		// generator resolves relative refs from disk instead of fetching
		// ucp.dev. Three files keep one (ucp.json and its two request
		// variants, which the pipeline skips), and where it survives it must
		// agree with the path — a mismatch would mean the corpus is laid out
		// differently than this derivation assumes, and every relative $ref
		// would then resolve against the wrong base.
		if embedded, ok := doc["$id"].(string); ok && embedded != id {
			return nil, nil, fmt.Errorf("%s: declares $id %q, want %q: the corpus layout no longer "+
				"matches how base URIs are derived, so relative refs would resolve wrongly", rel, embedded, id)
		}
		if err := c.AddResource(id, doc); err != nil {
			return nil, nil, fmt.Errorf("%s: add resource: %w", rel, err)
		}
		ids[rel] = id
	}
	return c, ids, nil
}

// schemaBaseURI is the identity the spec gives its schemas. Registering
// every document under it keeps the compiler resolving relative refs inside
// the corpus rather than over the network, which is what the embedded $id
// used to do before upstream removed it.
const schemaBaseURI = "https://ucp.dev/schemas/"
