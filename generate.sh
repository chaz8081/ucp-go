#!/usr/bin/env bash
# Regenerate UCP models from the canonical spec schemas.
# Usage: ./generate.sh [version]   e.g. ./generate.sh 2026-04-08
set -euo pipefail
cd "$(dirname "$0")"

VERSION="${1:-}"
BRANCH="main"
[ -n "$VERSION" ] && BRANCH="release/$VERSION"

rm -rf .ucp-spec
git clone -b "$BRANCH" --depth 1 \
  https://github.com/Universal-Commerce-Protocol/ucp .ucp-spec
SPEC_SHA="$(git -C .ucp-spec rev-parse HEAD)"
echo "spec: branch=$BRANCH sha=$SPEC_SHA"

GEN_SCHEMAS=".gen-schemas"
rm -rf "$GEN_SCHEMAS"

# Stage 1: normalize the spec schemas (allOf flattening, entity inlining,
# dotted-$defs renaming, metadata union, request variants).
go run ./cmd/ucpgen preprocess \
  -schemas .ucp-spec/source/schemas \
  -out-schemas "$GEN_SCHEMAS"

# Stage 2: compare against the committed goldens for this release, which
# were produced by the official python-sdk preprocessor. Any divergence is
# a parity regression and must fail the run rather than reach the emitter.
if [ -n "$VERSION" ] && [ -d "goldens/$VERSION" ]; then
  if ! diff -r "goldens/$VERSION" "$GEN_SCHEMAS" >/dev/null; then
    echo "ERROR: preprocessed schemas diverge from goldens/$VERSION" >&2
    diff -r "goldens/$VERSION" "$GEN_SCHEMAS" | head -40 >&2
    exit 1
  fi
  echo "goldens: matched goldens/$VERSION"
fi

# Stage 3: emit Go models from the normalized schemas.
go run ./cmd/ucpgen emit \
  -schemas "$GEN_SCHEMAS" \
  -out . \
  -spec-ref "$BRANCH@$SPEC_SHA"

# Format only our own tree — .ucp-spec and .gen-schemas hold third-party and
# generated JSON, and gofmt's directory walk does not skip dot-directories.
gofmt -w cmd
go build ./...
go vet ./...
