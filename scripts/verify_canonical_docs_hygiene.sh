#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$REPO_ROOT"

echo "verify-canonical-docs-hygiene: starting Plan 15 Phase I-8 gate..."

required=(
    "AGENTS.md"
    "llms.txt"
    "INSTALL.md"
    "docs/METHODOLOGY.md"
    "docs/operations/adr.md"
    "docs/operations/autonomy.md"
    "docs/operations/doctrine.md"
    "docs/decisions/_index.json"
)
for f in "${required[@]}"; do
    if [[ ! -f "$f" ]]; then
        echo "ERROR: required canonical doc missing: $f" >&2
        exit 1
    fi
done
echo "  [OK] required docs present (inv-zen-324)"

# Sub-invariant inv-zen-326: gitnexus-ux.md MUST NOT exist.
if [[ -f "docs/operations/gitnexus-ux.md" ]]; then
    echo "ERROR: docs/operations/gitnexus-ux.md MUST NOT exist (decisión 6; Plan 19 Caronte)" >&2
    exit 1
fi
echo "  [OK] gitnexus-ux.md correctly absent (inv-zen-326)"

echo "  [...] running Go compliance test TestInvZen329_CanonicalDocsHygiene"
if ! go test \
    -ldflags="-X github.com/ncruces/go-sqlite3/driver.driverName=sqlite3_ncruces" \
    ./tests/compliance/ \
    -run TestInvZen329_CanonicalDocsHygiene \
    -count=1 \
    -timeout=60s; then
    echo "ERROR: TestInvZen329_CanonicalDocsHygiene failed (see Go test output above)" >&2
    exit 2
fi
echo "  [OK] TestInvZen329_CanonicalDocsHygiene green (inv-zen-325/327/328)"

echo "verify-canonical-docs-hygiene: PASS (inv-zen-329 umbrella green)"
exit 0
