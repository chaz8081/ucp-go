// Package conformance verifies generated models against the canonical JSON
// Schemas using a draft-2020-12 validator as the oracle.
package conformance

import (
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
