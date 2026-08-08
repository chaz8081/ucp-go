# UCP Go SDK (`ucp-go`) — Design

**Date:** 2026-08-08
**Status:** Draft for review
**Repo:** `github.com/chaz8081/ucp-go` (new, dedicated)
**Goal:** A Go SDK for the Universal Commerce Protocol built to upstream-contribution quality, mirroring the techniques of the official Python and JS SDKs, with a stdlib-first dependency policy.

## 1. Context and intent

The official UCP SDKs ([python-sdk](https://github.com/Universal-Commerce-Protocol/python-sdk), [js-sdk](https://github.com/Universal-Commerce-Protocol/js-sdk)) are **models-only, schema-first, code-generated** libraries. Both derive from the canonical JSON Schemas in the [spec repo](https://github.com/universal-commerce-protocol/ucp) (`source/schemas/`, 94 files as of release 2026-04-08) via the same pipeline shape:

1. `generate_models.sh <version>` clones the spec at `release/<version>`
2. **Preprocess** schemas: merge `allOf` chains, flatten dotted `$defs`, generate per-operation request variants (Create/Update/Complete) from required-per-op annotations, normalize metadata unions
3. Run a generator (Python: `datamodel-code-generator` + custom pydantic_v2 templates; JS: `quicktype` → single `spec_generated.ts` + Zod)
4. **Postprocess**: inject constraints the generator cannot express (`minProperties`, array `contains`/`minContains`/`maxContains`, `uniqueItems`)
5. Format (`ruff` / `prettier`); SDK version maps to schema version (`0.4.x` ↔ `2026-04-08`)

No official Go SDK exists. This project fills that slot with the same architecture, adapted to Go idiom. Scope is phased: **v1 is models-only parity** (matching the official SDKs' scope exactly); the design leaves room for a client (phase 2) and server toolkit (phase 3) without reshaping v1.

## 2. Guiding decisions

- **The spec schemas are the source of truth, not the sibling SDKs.** Conformance is defined against `source/schemas/*.json`, with the official SDKs treated as parallel projections. This is what makes a pure-Go pipeline (no Python anywhere in the contributor toolchain) safe.
- **Stdlib-first.** The published SDK module has **zero** `require` lines. Any dependency anywhere in the repo must be vetted and its justification documented. Currently exactly one is approved, quarantined in a separate module (§6).
- **Custom Go generator** (`ucpgen`), not quicktype/go-jsonschema. Off-the-shelf Go generators fumble the parts that matter here (role variants, per-op variants, unions, injected constraints); the Python SDK effectively ended up half-custom anyway (custom templates + postprocessor). The emitter owns constraint injection at emit time, so no separate postprocess pass is needed.

## 3. Repository layout

```
ucp-go/
  go.mod                     # module github.com/chaz8081/ucp-go — zero require lines
  ucp.go                     # root types: UCP envelope, entity base
  service.go  capability.go  payment_handler.go  profile.go
  shopping/                  # checkout, cart, order, payment, ap2_mandate, …
    types/                   # line_item, total, card_credential, …
  transports/                # jsonrpc, mcp_tool_call, a2a_message, embedded_*
  common/                    # identity_linking, loyalty
  cmd/ucpgen/                # generator CLI (stdlib-only, lives in root module)
    preprocess/              # Go port of schema normalization
    emit/                    # templates + emitter
  conformance/               # SEPARATE MODULE — oracle tests, corpus, fuzz
    go.mod                   # carries the jsonschema oracle dependency
  goldens/                   # preprocessed-schema fixtures per spec release
  generate.sh                # clone spec release/<ver> → ucpgen → gofmt → manifest check
  MANIFEST.json              # generated coverage manifest (checked in)
  docs/specs/                # this document and successors
```

Package structure mirrors the Python SDK's domain layout (`ucp_sdk.models.schemas.shopping` → `ucp-go/shopping`) so a reviewer from the UCP org recognizes the shape immediately.

## 4. Generation pipeline (`ucpgen`)

`generate.sh <version>`:

1. Clone `Universal-Commerce-Protocol/ucp` at `release/<version>` (default `main`), record tag + commit SHA
2. `ucpgen preprocess` — Go implementation of the official normalization: `allOf` merge, local/external `$ref` resolution, dotted-`$defs` flattening, per-op variant generation, metadata-union normalization, transitive dependency propagation. Output: normalized schema set written to a temp dir **and diffed against `goldens/<version>/`** (§6.3)
3. `ucpgen emit` — walks normalized schemas, emits one `.go` file per schema file into the domain package, plus `MANIFEST.json`
4. `gofmt`/`go/format` on output; build + vet; manifest check fails the run on any gap

Determinism: sorted map iteration everywhere, no timestamps in output (the Python SDK passes `--disable-timestamp` for the same reason). Same spec SHA in → byte-identical output. Every generated file's header carries the spec release tag and commit SHA.

## 5. Generated code shape

- **Required fields**: value types. **Optional fields**: pointers with `omitzero` (falling back to `omitempty` where appropriate). Descriptions from schemas become doc comments (parity with `--use-schema-description`).
- **Extra fields**: parity with pydantic's `extra="allow"`. Generated `UnmarshalJSON` captures unknown keys into `Extra map[string]json.RawMessage`; generated `MarshalJSON` re-emits them. Lossless round-trip of payloads carrying fields newer than the SDK.
- **Role variants** (platform/business/response schemas per entity) and **per-op variants** (Create/Update/Complete requests): distinct named Go types, mirroring the Python SDK's `checkout.py` / `checkout_create_request.py` split.
- **Unions** (`oneOf`/`anyOf`, 11 schema files): a Go interface with a generated dispatch in `UnmarshalJSON` — discriminator-driven where the schema provides one (`const` fields), sequential validate-and-match otherwise.
- **Validation**: every type gets a generated `Validate() error` implementing the full constraint set, including the ones the Python SDK must inject in postprocess: numeric/string bounds, `pattern`, `minItems`/`maxItems`, `contains`/`minContains`/`maxContains`, `uniqueItems`, `minProperties`. Pattern regexes compile lazily via `sync.OnceValue`. Validation errors report JSON-pointer-style paths.
- **Regex caveat**: Go `regexp` is RE2; JSON Schema patterns are ECMA-262. `ucpgen` compiles every pattern at generation time and **fails generation** on any pattern RE2 cannot express (none exist in the current spec), forcing an explicit case-by-case decision rather than silent misbehavior.

## 6. Conformance spine (the no-drift guarantees)

Four layers, each guarding a different failure mode:

1. **Schema-as-oracle equality (semantic drift).** In `conformance/`: for any payload, `GeneratedType.Validate()` must agree with a draft-2020-12 JSON Schema validator run against the raw spec schema. Driven by native Go fuzzing (`testing.F`) plus a schema-driven payload generator (valid instances + targeted mutations). Disagreement = red build.
2. **Coverage manifest, fail-closed (coverage drift).** `MANIFEST.json` maps every schema file and `$def` to an emitted type. Generation fails if any spec schema file is unconsumed, any `$ref` unresolved, or any manifest entry orphaned. A new schema file in a spec release cannot be silently skipped.
3. **Golden preprocessed fixtures (convention drift).** Per-op variant generation is UCP convention, not JSON Schema standard — the oracle cannot check it. `goldens/<version>/` holds the normalized schema set produced by the official Python preprocessor, committed as plain JSON (produced once per spec release; contributors never run Python). `ucpgen preprocess` output must byte-match. An optional scheduled CI job re-derives goldens in a container as belt-and-suspenders.
4. **Spec-examples corpus (regression floor).** Every example payload in the spec repo's docs must unmarshal, `Validate()` clean, and round-trip losslessly. Versioned with the spec.

Release watch: a scheduled CI job checks for new spec `release/*` branches, regenerates, and opens a PR with the diff — version drift is surfaced, never silent.

## 7. Dependency policy

| Surface | Dependencies | Justification |
|---|---|---|
| SDK module (published) | none | stdlib `encoding/json`, `regexp`, `time`, `sync` suffice; validation is generated code |
| `cmd/ucpgen` | none | `text/template`, `go/format`, `encoding/json` |
| `conformance/` module | `github.com/santhosh-tekuri/jsonschema/v6` | The oracle. A correct draft-2020-12 validator is a project in itself; this is the de-facto standard pure-Go implementation with no transitive dependencies. Quarantined in its own module so the published SDK's `go.mod` stays empty. |

Adding any dependency requires: a documented reason in this spec's successor ADRs, a review of transitive deps, and confirmation stdlib cannot reasonably cover it.

## 8. Versioning and releases

- SDK `v0.x.y` maps to spec schema releases the same way the official SDKs do (`v0.4.x` ↔ schema `2026-04-08`); mapping recorded in README table and generated headers.
- Module path `github.com/chaz8081/ucp-go`. If the UCP org adopts the project, the path migration is a standard, documented move.
- Tagged releases only after the full conformance spine passes against the target spec release.

## 9. Testing strategy

- **Emitter unit tests** (table-driven): each preprocessing transform and emit feature tested against minimal schema fixtures.
- **Conformance module**: oracle fuzz, corpus, golden diffs (§6).
- **CI matrix**: latest two Go releases; `go vet`, `gofmt -l`, build, tests, short fuzz; scheduled long-fuzz and release-watch jobs.

## 10. Phases 2 and 3 (room reserved, not designed)

- **Phase 2 — client**: typed REST client for discovery/checkout (`client/` package or separate module). Depends only on the models package + `net/http`.
- **Phase 3 — server toolkit**: handler/middleware helpers for building UCP-speaking services (merchant or payment-handler side). Same rule.
- v1 constraint honored by both: the models packages import nothing from them, and the root module's zero-dependency guarantee applies to v1's published surface regardless of later phases (later phases may live in separate modules if they need dependencies).

## 11. Success criteria

1. `go get github.com/chaz8081/ucp-go` pulls zero transitive dependencies.
2. Full generation from spec release `2026-04-08` covers all 94 schema files with a green manifest.
3. Conformance spine green: oracle fuzz, goldens, spec-examples corpus.
4. Regeneration against the same spec SHA is byte-identical.
5. README documents the pipeline clearly enough that a UCP maintainer could run `generate.sh` unassisted.
