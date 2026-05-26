#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
set -euo pipefail

REPO_ROOT="$(git rev-parse --show-toplevel)"
cd "$REPO_ROOT"

if ! command -v goreleaser >/dev/null 2>&1; then
    echo "error: goreleaser not on PATH (brew install goreleaser)" >&2
    exit 1
fi
if ! command -v syft >/dev/null 2>&1; then
    echo "error: syft not on PATH (brew install syft)" >&2
    exit 1
fi

echo "==> Building verifier binaries"
make -s verify-release-artifacts-bin verify-cgo-supplement-bin

echo "==> Cleaning dist/"
rm -rf dist/

echo "==> Running goreleaser snapshot (skip sign + publish + docker)"
goreleaser release \
    --snapshot \
    --clean \
    --skip=sign,publish,docker

echo "==> Verifying release artifacts (fast mode; no network)"
bin/verify-release-artifacts \
    --dir dist \
    --mode fast \
    --check-sbom \
    --check-cgo-supplement \
    --check-attestation=false \
    --check-cosign=false

echo "==> Verifying CGO supplement vs go.mod + vendor/"
if [[ "${SUPPLEMENT_ALLOW_MISSING_VENDOR:-0}" == "1" ]]; then
    bin/verify-cgo-supplement --root . --allow-missing-vendor
else
    bin/verify-cgo-supplement --root . --allow-missing-vendor
fi

echo "==> Smoke complete; dist/ artifacts:"
find dist/ -type f | sort
