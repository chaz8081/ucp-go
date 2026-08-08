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

go run ./cmd/ucpgen \
  -schemas .ucp-spec/source/schemas \
  -out . \
  -spec-ref "$BRANCH@$SPEC_SHA"
gofmt -w .
go build ./...
go vet ./...
