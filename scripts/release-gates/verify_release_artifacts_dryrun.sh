#!/usr/bin/env bash
# SPDX-License-Identifier: MIT

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "${REPO_ROOT}"

DIST_DIR="${DIST_DIR:-dist}"
OWNER="${OWNER:-hades-system}"
REPO="${REPO:-hades-system}"
PLATFORM="${PLATFORM:-}"  # optional scope flag (e.g., darwin-arm64)

if [[ ! -d "${DIST_DIR}" ]] || [[ -z "$(ls -A "${DIST_DIR}" 2>/dev/null)" ]]; then
  echo "verify_release_artifacts_dryrun.sh: dist/ empty; running goreleaser snapshot"
  if ! command -v goreleaser >/dev/null 2>&1; then
    echo "ERROR: goreleaser not installed; please install via 'brew install goreleaser/tap/goreleaser' or https://goreleaser.com/install/" >&2
    exit 2
  fi
  goreleaser release --snapshot --clean --skip=publish --skip=docker --skip=sign
fi

BIN_PATH="$(mktemp -d)/verify-release-artifacts"
go build -o "${BIN_PATH}" ./cmd/verify-release-artifacts

VERIFY_ARGS=(
  --dir "${DIST_DIR}"
  --owner "${OWNER}"
  --repo "${REPO}"
  --skip-attestation
  --skip-cosign
)
if [[ -n "${PLATFORM}" ]]; then
  VERIFY_ARGS+=(--platform "${PLATFORM}")
fi

"${BIN_PATH}" "${VERIFY_ARGS[@]}"

echo "verify_release_artifacts_dryrun.sh: dry-run verification PASS"
