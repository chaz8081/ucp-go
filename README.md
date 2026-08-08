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

The `ucpgen` pipeline (`cmd/ucpgen`) runs end-to-end today — load, `allOf`-merge,
emit, manifest — and is exercised against the fixture schemas under
`cmd/ucpgen/preprocess/testdata/schemas/` in CI:

    go run ./cmd/ucpgen -schemas <schemas-dir> -out <out-dir> -spec-ref <branch@sha>

Full-spec generation is **Phase 2**: the real UCP spec has cross-file `$ref`s
the preprocessor doesn't resolve yet (only local, same-file refs are handled
so far), so running

    ./generate.sh 2026-04-08

against the actual spec schemas currently fails partway through. An allowlist
flag for a subset of files that already round-trip cleanly is planned; until
then, `generate.sh` is wired up but not yet a working end-to-end regeneration
path for the real spec.

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
