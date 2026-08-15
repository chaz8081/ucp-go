# ucp-go

Go SDK for the [Universal Commerce Protocol](https://ucp.dev). Code-generated
from the canonical JSON Schemas in the
[UCP specification](https://github.com/Universal-Commerce-Protocol/ucp),
mirroring the architecture of the official
[python-sdk](https://github.com/Universal-Commerce-Protocol/python-sdk) and
[js-sdk](https://github.com/Universal-Commerce-Protocol/js-sdk).

Targets spec version `2026-04-08`: 145 preprocessed schemas emit 145 Go files
across five packages, with no runtime dependencies.

## Status

Pre-release, `v0.x`. **There is no API-stability guarantee.** Type names,
field types and the shape of the generated surface may change between
releases — the generator is still being refined, and a change to the emitter
changes every model it produces.

This repository is built as a candidate for adoption as an official UCP Go
SDK. It is not a product launch, and nothing here should be read as a
commitment to a stable interface. What is stable is the process: the models
are derived from the spec's own schemas by a generator whose output is
reproducible and checked against the reference implementation.

## Install and use

    go get github.com/chaz8081/ucp-go

Models are committed, so consumers never run the generator.

```go
package main

import (
	"encoding/json"
	"fmt"
	"log"

	"github.com/chaz8081/ucp-go/shopping"
)

// A checkout as a UCP server returns it.
const response = `{"id":"chk_1","currency":"USD","status":"ready_for_complete",
	"line_items":[],"links":[],"totals":[],"ucp":{"version":"2026-04-08"}}`

func main() {
	var c shopping.Checkout
	if err := json.Unmarshal([]byte(response), &c); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("%s: %s (%s)\n", c.ID, c.Status, c.Currency)

	// Validate reports the first constraint violation, or nil.
	if err := c.Validate(); err != nil {
		log.Fatal(err)
	}
	fmt.Println("valid")

	// A required property that was absent is distinguishable from one
	// decoded to its zero value, so an incomplete payload is rejected.
	var partial shopping.Checkout
	if err := json.Unmarshal([]byte(`{"id":"chk_2"}`), &partial); err != nil {
		log.Fatal(err)
	}
	fmt.Println("incomplete:", partial.Validate())
}
```

    chk_1: ready_for_complete (USD)
    valid
    incomplete: currency: required property is missing

The five generated packages are `github.com/chaz8081/ucp-go` (package `ucp`,
the protocol root), `/common`, `/shopping`, `/shopping/types` and
`/transports`.

## How correctness is established

Three layers. Each exists because the others cannot see the class of defect
it catches.

**1. Preprocessor parity.** `ucpgen preprocess` reproduces the output of the
official python-sdk preprocessor **byte-for-byte** on all 145 files (78
source schemas, plus 67 request variants generated from `ucp_request`
markers). The committed goldens in `goldens/2026-04-08/` *are* the Python
preprocessor's output, re-encoded through this repo's canonical JSON encoder
so that any difference is a difference in content rather than formatting.

This layer catches divergence in *what gets modeled*. Preprocessing is where
`allOf` flattening, entity inlining, union-branch distribution and variant
generation happen; a bug there silently changes the schema the emitter sees,
and every downstream check would agree with the wrong answer. Parity against
an independent implementation is the only thing that catches it.

**2. Differential agreement.** The same JSON bytes are driven through the
generated models' `Validate` and through a real draft-2020-12 validator
(`santhosh-tekuri/jsonschema/v6`), and the two must reach the same verdict:
**324 payloads across 99 schemas, zero disagreements.**

This layer catches wrong *enforcement*. Golden tests prove the emitter is
reproducible; round-trip tests prove the types decode. Neither says whether
`Validate` agrees with the schema. Only comparison against an independent
implementation does.

The oracle compiles `pattern` with **ECMA-262** semantics, via
`dlclark/regexp2`, rather than the RE2 that Go's `regexp` and therefore the
generated code use. This matters more than it looks: if the oracle used RE2,
every `pattern` comparison would run the identical engine on both sides and
agreement would be true by construction — the check would pass without
testing anything, and no amount of fuzzing could ever fail on a pattern. The
two engines genuinely differ (`\s` covers the vertical tab in ECMA-262 but
not in RE2; lookarounds are valid ECMA-262 and rejected by RE2), and
`TestECMAEngineDiffersFromRE2` pins that difference so the oracle cannot
quietly degrade into a mirror of the thing it is checking.

**3. Fuzzing.** Two targets, both differential against the same oracle.
Recent runs: 10.1M executions against `ReverseDomainName` — a shipped type
whose entire purpose is its pattern — and 5.1M against a fixture carrying
`maxLength` and `pattern`, with no drift found.

This layer catches what a hand-written corpus does not reach: inputs nobody
thought to write down. Both sides are driven from the same raw bytes rather
than from a Go value, because re-marshaling a Go string rewrites invalid
UTF-8 to U+FFFD, which shifts the rune counts `maxLength` is measured in and
would manufacture disagreements that exist nowhere in the protocol.

Run the suites:

    go test ./...
    (cd conformance && go test ./...)

`conformance/` is a separate module, so the root's `./...` does not reach it.
Anything that changes the `ucpgen` command-line surface or the emitter's
output needs both.

## Zero dependencies

The root `go.mod` has **zero** `require` lines. The JSON Schema oracle and
its ECMA-262 regexp engine are quarantined in the separate `conformance/`
module, which depends on the root rather than the other way round, so nothing
a consumer builds ever pulls them in.

This is enforced, not asserted: a CI step fails the build on any `^require`
line in the root `go.mod`, so a stray `go get` cannot land unnoticed.

## What is and isn't modeled

Generated models decode, re-encode and validate. Where they fall short of a
full JSON Schema implementation, the gap is specific:

- **Unions with no shared properties are carried as `json.RawMessage`.**
  A `$ref`-only union becomes a struct with one optional typed field per
  member plus `UnmarshalJSON`/`MarshalJSON`; a union that also carries its
  own properties becomes a struct. Everything else is raw JSON. Typed
  alternatives for those are future work.
- **Open objects preserve unknown keys.** Objects the schema leaves open
  carry an `Extra map[string]json.RawMessage` and re-emit it on marshal, so
  extension keys survive a round trip rather than being dropped.
- **Required-property presence is tracked by the decoder.** Each decoded
  value records which properties it actually saw, so an absent required
  property is distinguishable from one decoded to its zero value. A value
  built in Go rather than decoded from JSON skips the presence check —
  otherwise every hand-constructed request would fail before it could be
  sent — but still gets every value check.
- **`format` is not asserted** (82 occurrences). In draft 2020-12 `format`
  is an annotation, not an assertion, unless a validator opts in. The oracle
  runs with format assertions off as well, so this is agreement with the
  spec's own default rather than a gap between the two implementations.
- **`if`/`then`/`else`, `not`, and the `contains` family are not
  evaluated** (23 occurrences: 7 `if`, 7 `then`, 3 each of `contains`,
  `minContains`, `maxContains`; `not` and `else` do not occur in this
  corpus). The differential harness skips these schemas by name rather than
  letting them pass by accident.
- **One import cycle is broken by carrying a single edge as raw JSON.** Go
  forbids import cycles and JSON Schema does not. The corpus contains
  exactly one: `shopping/types/error_response.json`'s `ucp` property points
  at the root metadata union, while the root points back down into
  `shopping/types`. The deeper-to-shallower edge is broken and carried as
  raw JSON, with a comment on the field saying why.

None of this is folklore. Every gap is recorded per schema under
`unenforced` in `MANIFEST.json`, and carried as a doc comment on the affected
type or field wherever there is a declaration to attach one to — so the
coverage boundary is machine-readable and reviewable rather than something
you have to reconstruct by reading the emitter. (Two `enum` gaps, both on
nested array elements, are manifest-only: the element has no Go field to
carry a comment.)

Keywords that would change a schema's *shape* rather than merely constrain
it — currently `patternProperties` — fail generation outright, because no
correct Go type can be produced for them.

### Known upstream limitation

The official preprocessor produces schemas with dangling references, and
this SDK reproduces that behaviour deliberately.

`preprocess_schemas.py:245` (`flatten_entity_reference` in python-sdk)
deep-copies `ucp.json#/$defs/entity` into `capability.json`,
`payment_handler.json` and `service.json` without rebasing the entity's
document-relative `$ref`s. The entity body contains
`"version": {"$ref": "#/$defs/version"}`; once copied, that pointer resolves
against its new host, which defines no such `$def`. The result is 24
dangling references, and 9 of 145 schemas that no conforming JSON Schema
validator can compile — the three hosts themselves plus everything
transitively referencing them, including `ucp.json`.

The spec's source schemas are correct — the defect is introduced by
preprocessing.

`ucp-go` mirrors it on purpose: `cmd/ucpgen/preprocess/document.go`'s
`flattenEntityRef` is a faithful port, and byte-for-byte parity with the
Python preprocessor is an enforced invariant (`TestPreprocessMatchesGoldens`).
Diverging unilaterally would break the parity that makes the committed
goldens trustworthy. The emitted models are unaffected: `ResolveRef` carries
a narrow, documented fallback that resolves these references against
`ucp.json`, which is where they were written.

The conformance harness skips the affected schemas by name and counts them,
rather than passing over them silently. Its tally reports four, because the
other five are already skipped a step earlier for out-of-scope keywords.

### Resolved upstream: the `ucp` metadata union

Reported as [python-sdk#73](https://github.com/Universal-Commerce-Protocol/python-sdk/issues/73) and **fixed** in python-sdk `35af25c`. Recorded here because the reasoning is still useful, and because it shows what this SDK's conformance work is for.

Source `ucp.json` declares no `oneOf` — its root carries only `$id`, `$schema`, `title`, `description` and `$defs`. Preprocessing synthesized one over the six profile `$defs`. Three of them — `response_cart_schema`, `response_catalog_schema` and `response_order_schema` — are identical apart from `title` and `description`, which do not affect validation.

`oneOf` means *exactly one*. Any instance matching one of the three matched all three, so the union had no satisfiable instance. Since `ucp` is required on `Cart`, `Checkout` and `Order`, none of the protocol's primary response types could validate.

Upstream now synthesizes the union with `anyOf`, which is what it always meant: a type union for code generation, not an exclusivity constraint. This SDK tracks that.

**The emitter still guards the general case.** When a `oneOf`'s members are structurally identical, it stops enforcing exclusivity for that union and says so in the generated doc comment. Nothing in the current corpus trips it; `TestUnsatisfiableOneOfDegradesToAnyOf` keeps it honest.

**How it was found, which is the part worth keeping.** Not by the differential harness — that could not have caught it. `ucp.json` is among the schemas the oracle cannot compile, for the dangling-reference reason above, so it is skipped before any verdict is compared. It surfaced when the example in this README was run and printed an error instead of a result. Pydantic's `Union` resolves to the first matching member rather than enforcing `oneOf`, which is why the Python SDK never saw it.

## Regenerating

    ./generate.sh 2026-04-08

Three stages, each with a job:

1. **Preprocess.** Clone the spec at `release/<version>` and normalize its
   source schemas: whole-document `allOf` flattening, `ucp.json#/$defs/entity`
   inlining, union-branch distribution, dotted-`$defs` renaming, metadata-union
   normalization, and generation of the `*_create_request.json` /
   `*_update_request.json` / `*_complete_request.json` variants.
2. **Diff against the committed goldens, and fail on any divergence.** This
   is the gate that matters: a parity regression must never reach the
   emitter. If preprocessing has drifted from the reference implementation,
   the run stops here rather than producing models that look fine and encode
   a different protocol.
3. **Emit.** Render Go models from the normalized schemas, then `gofmt`,
   `go build` and `go vet` the result.

Committed models must be regenerated and committed whenever the emitter
changes. `TestCommittedModelsMatchGenerator` in `conformance/` fails if what
is checked in no longer matches what the generator produces, so stale models
cannot be merged.

Maintainers regenerate the goldens themselves once per spec release with
`scripts/make-goldens.sh <version>` — the only step that needs Python.
Contributors never do, since the goldens are committed JSON.

## Repository layout

| Path | What it is |
| --- | --- |
| `./` (package `ucp`) | Protocol root: `ucp.json`, `capability.json`, `payment_handler.json`, `service.json` and the root request variants |
| `common/` | Shared cross-domain schemas |
| `shopping/` | Cart, Checkout, Order, Payment, Fulfillment and their request variants |
| `shopping/types/` | The shopping domain's component types |
| `transports/` | Transport-level configuration |
| `cmd/ucpgen/` | The generator: `preprocess` and `emit` subcommands |
| `goldens/<version>/` | Committed preprocessed schemas, produced by the official python-sdk preprocessor |
| `conformance/` | Separate module holding every dependency: the oracle, the differential harness, the fuzz targets, the goldens and drift guards |
| `MANIFEST.json` | Per-schema coverage record: emitted type, package, output path, field count, and every unenforced keyword |
| `docs/specs/` | Design documents |

Generation is fail-closed: a schema file that produces no manifest entry is
an error, not a silent gap, so `MANIFEST.json` is a complete record of what
was generated from what.
