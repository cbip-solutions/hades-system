#!/usr/bin/env bash
# SPDX-License-Identifier: MIT

set -euo pipefail

WRAPPER="$(dirname "$0")/verify-no-bypass-references.sh"
if [ ! -x "$WRAPPER" ]; then
    echo "FAIL: wrapper not executable: $WRAPPER" >&2
    exit 1
fi

FAILED=0
ASSERT_EQ() {
    local label="$1" got="$2" want="$3"
    if [ "$got" != "$want" ]; then
        echo "FAIL: $label — got=$got want=$want" >&2
        FAILED=$((FAILED + 1))
    else
        echo "OK: $label"
    fi
}

TMP1="$(mktemp -d)"
trap 'rm -rf "$TMP1"' EXIT
mkdir -p "$TMP1/tests/compliance"
echo 'package compliance' > "$TMP1/tests/compliance/clean_test.go"
mkdir -p "$TMP1/internal/providers"
cat > "$TMP1/internal/providers/sidecar_backend.go" <<'EOF'
package providers
type SidecarBackend struct{} // bypass-sidecar sanctioned
EOF
out_t1="$(bash "$WRAPPER" --root="$TMP1" 2>&1 || true)"
if echo "$out_t1" | grep -q "verify-no-bypass-references OK"; then
    echo "OK: test 1 — clean fixture exits 0 with OK banner"
else
    echo "FAIL: test 1 — clean fixture should report OK" >&2
    echo "got: $out_t1" >&2
    FAILED=$((FAILED + 1))
fi

TMP2="$(mktemp -d)"
mkdir -p "$TMP2/tests/newsurface"
cat > "$TMP2/tests/newsurface/dirty_test.go" <<'EOF'
package newsurface
var _ = "anthropic-bypass"
EOF
if bash "$WRAPPER" --root="$TMP2" >/dev/null 2>&1; then
    echo "FAIL: test 2 — dirty tests/ fixture should exit non-zero" >&2
    FAILED=$((FAILED + 1))
else
    echo "OK: test 2 — dirty tests/ fixture exits non-zero"
fi
rm -rf "$TMP2"

TMP3="$(mktemp -d)"
mkdir -p "$TMP3/configs"
echo "anthropic-bypass = true" > "$TMP3/configs/random.toml.example"
if bash "$WRAPPER" --root="$TMP3" >/dev/null 2>&1; then
    echo "FAIL: test 3 — dirty configs/ fixture should exit non-zero" >&2
    FAILED=$((FAILED + 1))
else
    echo "OK: test 3 — dirty configs/ fixture exits non-zero"
fi
rm -rf "$TMP3"

TMP4="$(mktemp -d)"
mkdir -p "$TMP4/internal/store/migrations"
echo "CREATE TABLE bypass_tokens (id INTEGER);" > "$TMP4/internal/store/migrations/099_bypass.sql"
if bash "$WRAPPER" --root="$TMP4" >/dev/null 2>&1; then
    echo "FAIL: test 4 — dirty SQL migration should exit non-zero" >&2
    FAILED=$((FAILED + 1))
else
    echo "OK: test 4 — dirty SQL migration exits non-zero"
fi
rm -rf "$TMP4"

if bash "$WRAPPER" --help >/dev/null 2>&1; then
    echo "OK: test 5 — --help exits 0"
else
    echo "FAIL: test 5 — --help should exit 0" >&2
    FAILED=$((FAILED + 1))
fi

if [ "$FAILED" -gt 0 ]; then
    echo ""
    echo "FAIL: $FAILED test(s) failed" >&2
    exit 1
fi
echo ""
echo "OK: all bash wrapper tests passed"
