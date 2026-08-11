module github.com/chaz8081/ucp-go/conformance

go 1.24

// The differential harness imports the emitter to work out which Go type
// each schema produces, so it can drive the generated models by name. The
// generated models themselves are not imported — they are not committed —
// so the harness generates them into a temporary module and talks to a
// probe program built there.
//
// This is also why every dependency lives in this module: the root must
// keep zero require lines.
replace github.com/chaz8081/ucp-go => ..

require (
	github.com/dlclark/regexp2 v1.11.0
	github.com/santhosh-tekuri/jsonschema/v6 v6.0.3
)

require (
	github.com/chaz8081/ucp-go v0.0.0
	golang.org/x/text v0.14.0 // indirect
)
