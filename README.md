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

// A checkout as a UCP server returns it. totals carries a subtotal entry
// because the schema requires exactly one.
const response = `{"id":"chk_1","currency":"USD","status":"ready_for_complete",
	"line_items":[],"links":[],
	"totals":[{"type":"subtotal","amount":1000,"display_text":"Subtotal"}],
	"ucp":{"version":"2026-04-08"}}`

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
**1,024 payloads across 228 generated types (137 of them schema-file roots),
zero disagreements.**

This layer catches wrong *enforcement*. Golden tests prove the emitter is
reproducible; round-trip tests prove the types decode. Neither says whether
`Validate` agrees with the schema. Only comparison against an independent
implementation does.

The denominator counts **emitted Go types**, which is a different quantity
from the schema **files** it used to count, and larger: the emitter produces
231 types from 145 files, because a file's `$defs` become types of their
own. Iterating files reached only each file's root type, so every `$defs`
type went unchecked — and eight files, holding the whole capability model
along with ap2_mandate, buyer_consent, discount, fulfillment and
identity_linking, have no root type at all and were skipped entire. That gap
is now closed: each `$def` that emits a type is a target in its own right.
The two `$defs` that emit nothing are extension mount points — namespaces
grouping other schemas — and are not a gap.

Each widening has found real defects. Covering union-, array- and
scalar-rooted schemas found four request-variant types that were empty
structs accepting any JSON object, and every named type accepting a bare
`null`. Covering `$defs` found two more. A `$def` consisting only of a
`$ref` was emitted as a Go *defined* type over its target, which copies the
target's fields and none of its methods, so five types accepted `{}` and
`null` unconditionally; they are Go type aliases now. And the `allOf` merge
let an inherited property definition overwrite the node's own, so a schema
that narrows what it inherits lost the narrowing — `const: "card"` was never
enforced, and `CardPaymentInstrument.Display` was flattened to
`map[string]any`, six typed card fields and all. The node's own definition
now wins, which recovered five field types and three constant checks. All
are fixed.

That last one is a narrowing rather than the intersection `allOf` really
specifies, correct for every overlapping redefinition in this corpus and not
in general; the emitter says so at the code, and a corpus that grew a
widening redefinition would surface here as a disagreement.

Nothing is suppressed to reach zero. There is no skip list of known-failing
payloads, and a disagreement fails the suite.

Figures below the headline are equally literal. 3 targets are skipped, for a
union alongside sibling `properties` that the harness does not model; they
are reported as skips, not folded into the exercised count.

This number used to be 71. Every one of those was a schema the oracle could
not compile because of the dangling `#/$defs/version` references described
below, now fixed upstream. Unblocking them roughly tripled what the harness
actually compares — from 693 payloads across 157 types to 1,024 across
228 — and the very first run of the wider corpus found a real gap, described
next. The coverage figure had looked healthy the whole time.

One payload disagrees and is reported rather than counted as agreement:
`shopping/types/error_response.json`'s `ucp` property is carried as
`json.RawMessage`, so `Validate` cannot see inside it and cannot reject a
malformed value there. The harness proves that attribution instead of
assuming it — every leaf of the oracle's rejection must fall inside a raw
field, or the payload stays a mismatch — and prints the count every run
(`TestRawFieldExplainsRejection` pins both directions). Two properties are
carried this way: this one, to break an import cycle, and capability's
`extends`, whose schema has no single Go shape.

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
- **A bare `null` is rejected by every generated decoder.**
  `json.Unmarshal([]byte("null"), &v)` is a documented no-op for every Go
  type, and this SDK leans on the decoder as its JSON type check — a string
  payload failing to decode into an `int64` *is* the rejection. `null` was
  the one input the decoder let through for everything, so a null document
  validated as though it were a real value. Each decoder now rejects it
  outright. An optional property may still be `null` or absent, which
  `encoding/json` handles at the pointer before the type's decoder is
  reached.
- **`format` is not asserted** (144 occurrences). In draft 2020-12 `format`
  is an annotation, not an assertion, unless a validator opts in. Every
  schema in the corpus declares plain `draft/2020-12/schema` and none
  declares a `$vocabulary`, so the Format-Assertion vocabulary is not in
  effect; the oracle runs with format assertions off as well. Asserting it
  would make this SDK *stricter* than the schemas require and put it in
  disagreement with the oracle.

  `MANIFEST.json` therefore records these under `not_asserted`, separate
  from `unenforced`. The two used to share a key, which made the manifest
  report 150 unmet obligations where there are **6** — `format` outnumbers
  the real gap by twenty-four to one, so merging them buried it. Both
  numbers stay visible; neither is a summary of the other.
  `TestCorpusUsesAnnotationOnlyFormat` fails the build if a future spec
  release opts into the assertion vocabulary, which would turn every
  `not_asserted` entry into an understatement.
- **`if`/`then` and the `contains` family are enforced.** A condition
  compiles to a Go `if` guarding the consequent's checks; `contains`
  compiles to a count of the elements matching its subschema. Two
  restrictions are deliberate and fail generation rather than being
  approximated: `else`, which no corpus schema declares, and a condition
  testing more than one property, where a wrong conjunction would silently
  mis-scope the rule it guards.

  `contains` was verified only by unit tests when it shipped. `totals.json`
  is array-rooted, and the differential harness produced no payloads for a
  non-object root, so the rule reached the oracle for the first time one
  phase later — at which point it agreed. The coverage figure above was
  accurate throughout and still did not cover this; a number counts what it
  counts, and the thing worth stating is which schemas were behind it.
- **Two `if`/`then` pairs remain unenforced**, on `TotalCreateRequest` and
  `TotalUpdateRequest`. A request variant is a projection that drops every
  property marked `ucp_request:omit` while keeping the rules, so those two
  arrive carrying rules about properties the Go struct no longer has. There
  is nothing to bind them to and no rewriting recovers them, so they are
  reported as gaps rather than approximated.
- **A conditional whose condition negates a test is enforced only for
  decoded values.** The Go zero value satisfies a negation, so an unset
  field would match a rule that should not apply to it; only the decoder's
  presence record tells the two apart. This affects one rule, on the
  `totals` element type, and the affected types say so in their doc
  comments. Relatedly, a JSON `null` for an optional property is
  indistinguishable from an absent one after decoding, so a conditional
  requiring that property accepts `null` where JSON Schema would not.
- **One import cycle is broken by carrying a single edge as raw JSON.** Go
  forbids import cycles and JSON Schema does not. The corpus contains
  exactly one: `shopping/types/error_response.json`'s `ucp` property points
  at the root metadata union, while the root points back down into
  `shopping/types`. The deeper-to-shallower edge is broken and carried as
  raw JSON, with a comment on the field saying why.

  **The consequence is a real gap, not just a typing inconvenience:**
  `Validate` cannot see inside a `json.RawMessage`, so a malformed `ucp`
  object on an error response is accepted. The differential harness reports
  and counts it every run rather than absorbing it. `capability`'s `extends`
  is raw for the unrelated reason above — a schema with no single Go shape —
  and has the same consequence. Closing this needs the shared type moved
  somewhere both packages can import; it is not fixable at the field.

None of this is folklore. Every gap is recorded per schema under
`unenforced` in `MANIFEST.json` — with keywords this dialect defines as
annotations kept apart under `not_asserted`, so conformant behaviour is
never counted as a shortfall — and carried as a doc comment on the affected
type or field wherever there is a declaration to attach one to — so the
coverage boundary is machine-readable and reviewable rather than something
you have to reconstruct by reading the emitter. (Two `enum` gaps, both on
nested array elements, are manifest-only: the element has no Go field to
carry a comment.)

Keywords that would change a schema's *shape* rather than merely constrain
it — currently `patternProperties` — fail generation outright, because no
correct Go type can be produced for them.

### Resolved upstream: dangling entity references

Reported as [python-sdk#72](https://github.com/Universal-Commerce-Protocol/python-sdk/issues/72) and **fixed** in python-sdk `d650f0b` ([PR #79](https://github.com/Universal-Commerce-Protocol/python-sdk/pull/79)). Recorded because the mechanism generalizes.

`flatten_entity_reference` deep-copied `ucp.json#/$defs/entity` into
`capability.json`, `payment_handler.json` and `service.json`. The entity
body contains `"version": {"$ref": "#/$defs/version"}` — a *document-relative*
pointer. Copied into a host that defines no such `$def`, it resolved to
nothing: 24 dangling references, and 9 of 145 schemas that no conforming
validator could compile.

The spec's source schemas were correct. The defect was introduced by
preprocessing, one layer above where it showed.

A dangling `$ref` does not fail loudly — a generator types the field as
`Any`. python-sdk's released package therefore accepted any value for
`version` on every model derived from the entity, where the spec requires
`^\d{4}-\d{2}-\d{2}$`. The visible symptom was not a crash but a check
that had silently stopped happening.

The fix resolves the entity's own local references **once, at extraction,
while it still sits in `ucp.json`**, so every copy made afterwards is
self-contained. `flatten_entity_reference` itself never changed. Rebasing
the pointer to `ucp.json#/$defs/version` instead — the obvious repair —
closes a cycle, because `ucp.json` already references into all three hosts;
`datamodel-codegen` responds by collapsing the package into one private
module and renaming every colliding class.

`ucp-go` ports the fix as `preprocess.ResolveLocalRefs`, called from
`Preprocess` at the same point. `ResolveRef` previously carried a narrow
fallback that resolved these references against `ucp.json`, which is why the
emitted models were never affected; the corpus now contains no unresolvable
local reference at all, so that fallback is deleted rather than left to rot.
`TestResolveRefDoesNotRescueDanglingLocalRefs` pins the stricter rule: a
local `$ref` resolves in its own document or fails.

### Resolved upstream: the `ucp` metadata union

Reported as [python-sdk#73](https://github.com/Universal-Commerce-Protocol/python-sdk/issues/73) and **fixed** in python-sdk `35af25c`. Recorded here because the reasoning is still useful, and because it shows what this SDK's conformance work is for.

Source `ucp.json` declares no `oneOf` — its root carries only `$id`, `$schema`, `title`, `description` and `$defs`. Preprocessing synthesized one over the six profile `$defs`. Three of them — `response_cart_schema`, `response_catalog_schema` and `response_order_schema` — are identical apart from `title` and `description`, which do not affect validation.

`oneOf` means *exactly one*. Any instance matching one of the three matched all three, so the union had no satisfiable instance. Since `ucp` is required on `Cart`, `Checkout` and `Order`, none of the protocol's primary response types could validate.

Upstream now synthesizes the union with `anyOf`, which is what it always meant: a type union for code generation, not an exclusivity constraint. This SDK tracks that.

**The emitter still guards the general case.** When a `oneOf`'s members are structurally identical, it stops enforcing exclusivity for that union and says so in the generated doc comment. Nothing in the current corpus trips it; `TestUnsatisfiableOneOfDegradesToAnyOf` keeps it honest.

**How it was found, which is the part worth keeping.** Not by the differential harness — at the time it could not have caught it. `ucp.json` was among the schemas the oracle could not compile, for the dangling-reference reason above, so it was skipped before any verdict was compared. One upstream defect hid the other from the tool built to find it. It surfaced when the example in this README was run and printed an error instead of a result. Pydantic's `Union` resolves to the first matching member rather than enforcing `oneOf`, which is why the Python SDK never saw it.

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
| `cmd/ucpgen/` | The generator: `preprocess`, `emit` and `canonicalize` subcommands |
| `goldens/<version>/` | Committed preprocessed schemas, produced by the official python-sdk preprocessor |
| `goldens/<version>.provenance.txt` | The spec commit **and** the python-sdk commit the goldens were built from. Goldens depend on the preprocessor as much as on the spec, and only the spec version used to be recoverable, so an upstream preprocessor change was indistinguishable from no change at all |
| `conformance/` | Separate module holding every dependency: the oracle, the differential harness, the fuzz targets and the drift guards |
| `MANIFEST.json` | Per-schema coverage record: emitted type, package, output path, field count, every unenforced keyword, and separately every keyword this dialect makes annotation-only |
| `docs/specs/` | Design documents |

Generation is fail-closed: a schema file that produces no manifest entry is
an error, not a silent gap, so `MANIFEST.json` is a complete record of what
was generated from what.
