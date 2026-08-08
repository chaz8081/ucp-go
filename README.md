# ucp-go

Go SDK for the [Universal Commerce Protocol](https://ucp.dev). Code-generated
from the canonical JSON Schemas in the
[UCP specification](https://github.com/universal-commerce-protocol/ucp),
mirroring the architecture of the official
[python-sdk](https://github.com/Universal-Commerce-Protocol/python-sdk) and
[js-sdk](https://github.com/Universal-Commerce-Protocol/js-sdk).

**Zero runtime dependencies.** The published module's `go.mod` has no `require` lines.

Status: pre-release. See `docs/specs/` for the design.

## Regenerating models

    ./generate.sh 2026-04-08
