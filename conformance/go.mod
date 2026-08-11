module github.com/chaz8081/ucp-go/conformance

go 1.24

// Inert Phase 2 pre-wiring: no `require` on the root module yet. The
// oracle currently validates against a hand-copied mirror of ucpgen's
// output (see oracle_test.go), not an import — this replace directive
// just has the path ready for when Phase 2 wires a real import.
replace github.com/chaz8081/ucp-go => ..

require (
	github.com/dlclark/regexp2 v1.11.0
	github.com/santhosh-tekuri/jsonschema/v6 v6.0.3
)

require golang.org/x/text v0.14.0 // indirect
