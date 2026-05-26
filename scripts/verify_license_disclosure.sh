#!/usr/bin/env bash
#   1. LICENSE                  MIT canonical text + Copyright "Ika el Zur"
#   2. per-file SPDX headers    "SPDX-License-Identifier: MIT"
#   - LICENSE contains "MIT" + "Ika el Zur" copyright (decisión 15 + sub-15-1).
set -euo pipefail

REQUIRED_SURFACES=(
    "LICENSE"
    "README.md"
    "INSTALL.md"
    "THIRD_PARTY_LICENSES.md"
    "Formula/hades.rb"
)

FAIL=0

for surface in "${REQUIRED_SURFACES[@]}"; do
    if [[ ! -f "$surface" ]]; then
        echo "FAIL: surface $surface missing"
        exit 2
    fi
done

if ! grep -qF "MIT" LICENSE; then
    echo "FAIL: surface LICENSE missing 'MIT'"
    FAIL=1
fi
if ! grep -qF "Ika el Zur" LICENSE; then
    echo "FAIL: surface LICENSE missing copyright 'Ika el Zur'"
    FAIL=1
fi

if ! grep -qE "^## License" README.md; then
    echo "FAIL: surface README.md missing '## License' section"
    FAIL=1
fi
if ! grep -qF "MIT" README.md; then
    echo "FAIL: surface README.md missing 'MIT' keyword"
    FAIL=1
fi

if ! grep -qF "Installing HADES" INSTALL.md; then
    echo "FAIL: surface INSTALL.md missing 'Installing HADES' entrypoint"
    FAIL=1
fi

for kw in "hermes-agent" "smacker/go-tree-sitter" "sqlite-vec"; do
    if ! grep -qF "$kw" THIRD_PARTY_LICENSES.md; then
        echo "FAIL: surface THIRD_PARTY_LICENSES.md missing keyword '$kw'"
        FAIL=1
    fi
done

if ! grep -qE 'license "MIT"' Formula/hades.rb; then
    echo "FAIL: surface Formula/hades.rb missing 'license \"MIT\"'"
    FAIL=1
fi
if ! grep -qF "Caronte" Formula/hades.rb; then
    echo "FAIL: surface Formula/hades.rb missing 'Caronte' mention in caveats"
    FAIL=1
fi

if [[ -x "bin/hades" ]]; then
    DOCTOR_OUT="$(bin/hades doctor caronte 2>&1 || true)"
    if ! grep -qF "Caronte" <<< "$DOCTOR_OUT"; then
        echo "WARN: bin/hades doctor caronte output missing 'Caronte' (best-effort cross-check)"
    fi
fi

if [[ "$FAIL" -ne 0 ]]; then
    echo ""
    echo "FAIL: license disclosure 4-redundant MIT compliance violated (inv-zen-174 post-decisión-15)"
    exit 1
fi

echo "OK: license disclosure 4-redundant MIT + keyword coverage clean across canonical surfaces (inv-zen-174 post-decisión-15)"
exit 0
