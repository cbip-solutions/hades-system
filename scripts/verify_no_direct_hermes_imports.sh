#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Copyright 2026 Ika el Zur

set -euo pipefail

REPO_ROOT="${REPO_ROOT:-$(git rev-parse --show-toplevel 2>/dev/null || pwd)}"
cd "$REPO_ROOT"

for d in internal cmd tests; do
    if [[ ! -d "$d" ]]; then
        echo "ERROR: required directory '$d' not found in $REPO_ROOT" >&2
        exit 2
    fi
done


violations=$(
    grep -rEn \
        --include='*.go' \
        -e '^[[:space:]]*import[[:space:]]+"hermes_cli"' \
        -e '^[[:space:]]*import[[:space:]]+"hermes_agent"' \
        -e '^[[:space:]]+"hermes_cli"' \
        -e '^[[:space:]]+"hermes_agent"' \
        internal/ cmd/ tests/ \
        2>/dev/null \
    | grep -v '^internal/hermes/boundary/' \
    || true
)

if [[ -n "$violations" ]]; then
    echo "FAIL: direct hermes_cli/hermes_agent imports detected outside internal/hermes/boundary/:"
    # shellcheck disable=SC2001 # multi-line input; sed is the clearest tool here
    echo "$violations" | sed 's/^/  - /'
    echo ""
    echo "Per decisión 7-b (H-12 boundary consolidation), all Hermes-touching code must"
    echo "route through internal/hermes/boundary/. Refactor the violating files to use the"
    echo "boundary.Surface interface (see internal/hermes/boundary/doc.go for the"
    echo "consolidation rationale + ADR-0117 for the architectural decision)."
    exit 1
fi

echo "PASS: no direct hermes_cli/hermes_agent imports outside internal/hermes/boundary/"
exit 0
