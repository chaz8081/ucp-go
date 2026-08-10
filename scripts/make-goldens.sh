#!/usr/bin/env bash
# Produce golden preprocessed schemas from the OFFICIAL python-sdk
# preprocessor. Run once per spec release by a maintainer; contributors
# never need Python, because the goldens are committed JSON.
#
# Usage: scripts/make-goldens.sh 2026-04-08
#
# Requires python3 (stdlib only) and git. PYTHONHASHSEED is pinned because
# the python preprocessor builds some lists via set(), whose iteration order
# is hash-seed dependent; without the pin the goldens would not be
# reproducible even from python to python.
set -euo pipefail
cd "$(dirname "$0")/.."

VERSION="${1:?usage: make-goldens.sh <spec-version>}"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

git clone -b "release/$VERSION" --depth 1 \
  https://github.com/Universal-Commerce-Protocol/ucp "$WORK/ucp"
git clone --depth 1 \
  https://github.com/Universal-Commerce-Protocol/python-sdk "$WORK/python-sdk"

echo "python-sdk: $(git -C "$WORK/python-sdk" rev-parse --short HEAD)"

# The official preprocessor rewrites the schema tree in place and writes the
# generated *_request.json variants alongside it.
(
  cd "$WORK/python-sdk"
  PYTHONHASHSEED=0 python3 preprocess_schemas.py "$WORK/ucp/source/schemas" >/dev/null
)

# Re-encode python's output through our own canonical encoder so the goldens
# differ from Go's output only where the CONTENT differs — never because of
# indentation, key order, or number formatting.
rm -rf "goldens/$VERSION"
go run ./cmd/ucpgen canonicalize \
  -schemas "$WORK/ucp/source/schemas" \
  -out-schemas "goldens/$VERSION"

echo "Goldens written to goldens/$VERSION ($(find "goldens/$VERSION" -name '*.json' | wc -l | tr -d ' ') files) — review and commit."
