# ucp-go

Go SDK for the [Universal Commerce Protocol](https://ucp.dev). Code-generated
from the canonical JSON Schemas in the
[UCP specification](https://github.com/Universal-Commerce-Protocol/ucp),
mirroring the architecture of the official
[python-sdk](https://github.com/Universal-Commerce-Protocol/python-sdk) and
[js-sdk](https://github.com/Universal-Commerce-Protocol/js-sdk).

**Zero runtime dependencies.** The published module's `go.mod` has no `require` lines.

Status: pre-release. See `docs/specs/` for the design.

## Regenerating models

`ucpgen` has two stages, which `./generate.sh <version>` runs in order.

**Stage 1 — `preprocess`** normalizes the raw spec schemas: whole-document
`allOf` flattening, `ucp.json#/$defs/entity` inlining, union-branch
distribution, dotted-`$defs` renaming, metadata-union normalization, and
generation of the `*_create_request.json` / `_update_request.json` /
`_complete_request.json` variants from `ucp_request` markers.

    go run ./cmd/ucpgen preprocess -schemas <spec>/source/schemas -out-schemas .gen-schemas

This stage handles the **full real spec**: the 78 source schemas in
`release/2026-04-08` normalize into 145 files (78 sources plus 67 generated
request variants), byte-identical to the output of the official python-sdk
preprocessor — see [Goldens](#goldens) below.

**Stage 2 — `emit`** renders Go models from the normalized schemas.

    go run ./cmd/ucpgen emit -schemas .gen-schemas -out . -spec-ref <branch@sha>

Emitting the whole normalized spec is **Phase 3**: the emitter deliberately
rejects shapes it doesn't yet support (schemas whose content lives entirely
in `$defs`, unions, cross-file type references, and most constraint
keywords) rather than silently generating models that under-validate. It
runs today against flat object schemas, exercised by `go test ./...`.

### Goldens

`goldens/2026-04-08/` holds the preprocessed schema set produced by the
**official python-sdk preprocessor**, re-encoded through this repo's
canonical JSON encoder so that any difference is a difference in content
rather than formatting. `conformance/` requires the Go preprocessor to
reproduce it byte-for-byte, and `generate.sh` fails the run on any
divergence.

Maintainers regenerate goldens once per spec release with
`scripts/make-goldens.sh <version>` (the only step that needs Python —
contributors never do, since the goldens are committed JSON).

### MANIFEST.json

Every run writes a `MANIFEST.json` alongside the generated `.go` files,
mapping each consumed schema file to its emitted Go type, package, output
path, and field count. Generation is **fail-closed**: a schema file that
doesn't produce a manifest entry is treated as an error, not a silent gap —
so the manifest doubles as a coverage record of exactly what got generated
from what.

### conformance/

`conformance/` is a separate Go module that checks generated models agree
with a real draft-2020-12 JSON Schema validator (the "oracle") on the same
fixture payloads. It's the one place in the repo that carries an external
dependency (`github.com/santhosh-tekuri/jsonschema/v6`); see `docs/specs/`
§7 for why it's approved and quarantined there instead of in the published
SDK module.
