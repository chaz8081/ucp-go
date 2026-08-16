#!/usr/bin/env bash
# Regenerate UCP models from the canonical spec schemas.
#
# Usage:
#   ./generate.sh              regenerate at the pinned spec version
#   ./generate.sh 2026-04-08   regenerate at a specific release
#   ./generate.sh main         regenerate from the spec's main branch
#
# With no argument this reproduces the committed models exactly, which is
# what makes the drift guard in conformance/ meaningful. It used to default
# to the spec's main branch instead: that skipped the goldens comparison
# (stage 2 only ran when a version was passed), and main has since drifted
# past this SDK's pin far enough that the run fails outright. The least
# careful invocation was also the default one.
set -euo pipefail
cd "$(dirname "$0")"

# pinnedRef is the spec ref the committed models were generated from, in
# the form release/<version>@<sha>. MANIFEST.json is the record of what
# was actually emitted, so it — not a constant in this script — is the
# thing to stay in step with.
pinnedRef="$(grep -o '"spec_ref"[[:space:]]*:[[:space:]]*"[^"]*"' MANIFEST.json |
  sed 's/.*"\(.*\)"$/\1/')"
if [ -z "$pinnedRef" ]; then
  echo "ERROR: MANIFEST.json has no spec_ref; cannot tell what version is pinned" >&2
  exit 1
fi
pinnedVersion="${pinnedRef#release/}"
pinnedVersion="${pinnedVersion%@*}"
pinnedSHA="${pinnedRef#*@}"

VERSION="${1:-$pinnedVersion}"
if [ "$VERSION" = "main" ]; then
  BRANCH="main"
  VERSION="" # an unpinned branch has no goldens to compare against
else
  BRANCH="release/$VERSION"
fi

rm -rf .ucp-spec
git clone -b "$BRANCH" --depth 1 \
  https://github.com/Universal-Commerce-Protocol/ucp .ucp-spec
# Abbreviated to match the form MANIFEST.json and every generated header
# already carry. The full 40-char sha rewrites the "// Source:" line of all
# 145 files and breaks TestCommittedModelsMatchGenerator, so a regeneration
# that changed nothing would still show up as a diff.
SPEC_SHA="$(git -C .ucp-spec rev-parse --short="${#pinnedSHA}" HEAD)"
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
if [ -n "$VERSION" ]; then
  if [ ! -d "goldens/$VERSION" ]; then
    echo "ERROR: no goldens/$VERSION to check parity against" >&2
    exit 1
  fi
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
