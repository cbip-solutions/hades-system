#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
set -euo pipefail

if [[ $# -lt 1 ]]; then
  echo "usage: $0 DIST_DIR [VERSION]" >&2
  exit 2
fi

DIST_DIR="$1"
VERSION="${2:-}"

if [[ ! -d "$DIST_DIR" ]]; then
  echo "verify_release_checksums.sh: --dist=$DIST_DIR not a directory" >&2
  exit 2
fi

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd -- "$SCRIPT_DIR/../.." && pwd)"

GOLDEN_PATH="${GOLDEN_PATH:-$REPO_ROOT/scripts/release-gates/release-checksums.golden.json}"
REQUIRE_ALL="${REQUIRE_ALL:-1}"
BIN_DIR="${BIN_DIR:-$REPO_ROOT/bin}"
BIN_CHECK="${BIN_CHECK:-0}"

BIN="$BIN_DIR/verify-release-checksums"
if [[ ! -x "$BIN" ]]; then
  echo "verify_release_checksums.sh: building $BIN (was not present)"
  (cd "$REPO_ROOT" && go build -o "$BIN" ./cmd/verify-release-checksums)
fi

ARGS=(--dist "$DIST_DIR")

if [[ "$REQUIRE_ALL" == "1" ]]; then
  ARGS+=(--require-all-platforms)
fi

if [[ -n "$VERSION" && -f "$GOLDEN_PATH" ]]; then
  ARGS+=(--golden "$GOLDEN_PATH" --version "$VERSION")
fi

if [[ "$BIN_CHECK" == "1" ]]; then
  ARGS+=(--bin-check)
fi

echo "verify_release_checksums.sh: invoking $BIN ${ARGS[*]}"
exec "$BIN" "${ARGS[@]}"
