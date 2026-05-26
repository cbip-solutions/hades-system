#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
set -euo pipefail

REPO_ROOT="$(git rev-parse --show-toplevel)"
BIN="$REPO_ROOT/bin/verify-cgo-supplement"

if ! git diff --cached --name-only | grep -qE '^(docs/sbom/cgo-supplement\.cdx\.json|go\.mod|vendor/)'; then
    exit 0
fi

if [[ ! -x "$BIN" ]]; then
    echo "pre-commit: building $BIN ..." >&2
    (cd "$REPO_ROOT" && make -s verify-cgo-supplement-bin)
fi

if ! "$BIN" --root "$REPO_ROOT" --allow-missing-vendor; then
    echo "pre-commit: CGO supplement drift detected. Update configs/cgo-supplement.cdx.json to match go.mod / vendor/ state." >&2
    exit 1
fi
