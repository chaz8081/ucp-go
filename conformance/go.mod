module github.com/chaz8081/ucp-go/conformance

go 1.24

// The harness imports the committed models directly and calls Validate on
// them in process. It also imports the emitter, to work out which Go type
// each schema produces and so keep the schema-to-type table honest.
//
// Every dependency lives in this module because the root must keep zero
// require lines: the oracle and its regexp engine stay on this side of the
// boundary, the shipped models on the other.
replace github.com/chaz8081/ucp-go => ..

require (
	github.com/dlclark/regexp2 v1.11.0
	github.com/santhosh-tekuri/jsonschema/v6 v6.0.3
)

require (
	github.com/chaz8081/ucp-go v0.0.0
	golang.org/x/text v0.14.0 // indirect
)
