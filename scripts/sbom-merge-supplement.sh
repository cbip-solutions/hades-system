#!/usr/bin/env bash
# SPDX-License-Identifier: MIT

set -euo pipefail

DIST_DIR="${DIST_DIR:-dist}"
SUPPLEMENT="${SUPPLEMENT:-configs/cgo-supplement.cdx.json}"
BINARY="${VERIFY_CGO_SUPPLEMENT_BIN:-bin/verify-cgo-supplement}"

if [[ ! -f "$SUPPLEMENT" ]]; then
    echo "error: CGO supplement file not found: $SUPPLEMENT" >&2
    exit 1
fi

if [[ ! -x "$BINARY" ]]; then
    echo "error: verify-cgo-supplement binary not found or not executable: $BINARY" >&2
    echo "hint: run 'make verify-cgo-supplement-bin' first (target builds bin/verify-cgo-supplement)" >&2
    exit 1
fi

merged_count=0
while IFS= read -r -d '' cdx_path; do
    echo "merging supplement into $cdx_path"
    "$BINARY" --merge --sbom "$cdx_path" --supplement "$SUPPLEMENT"
    merged_count=$((merged_count + 1))
done < <(find "$DIST_DIR" -type f -name '*.cdx.json' -print0)

if [[ $merged_count -eq 0 ]]; then
    echo "error: no .cdx.json artifacts found in $DIST_DIR/ (expected >=1 from goreleaser sboms: block)" >&2
    exit 1
fi

echo "merged supplement into $merged_count cdx.json artifact(s)"
