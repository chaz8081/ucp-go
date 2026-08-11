package conformance

import "testing"

// TestECMAEngineDiffersFromRE2 pins the reason the oracle needs its own
// engine: Go's RE2 and ECMA-262 disagree on patterns that both accept, so
// an oracle using Go regexp agrees with the generated code by construction
// and can never fail on a pattern divergence.
func TestECMAEngineDiffersFromRE2(t *testing.T) {
	// \s excludes the vertical tab in Go's RE2 but includes it in ECMA-262.
	re, err := ecmaRegexp(`^\s$`)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if !re.MatchString("\v") {
		t.Error(`ECMA-262 \s should match a vertical tab; engine looks like RE2`)
	}
	// A lookahead is valid ECMA-262 and rejected by RE2 — the generator's
	// RE2 gate exists precisely because of this class.
	if _, err := ecmaRegexp(`^(?=x)x$`); err != nil {
		t.Errorf("ECMA-262 engine should accept a lookahead: %v", err)
	}
}
