#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
set -euo pipefail

FAIL=0

pass() { echo "PASS: $1"; }
fail() { echo "FAIL: $1"; FAIL=1; }

SCRIPT="$(cd "$(dirname "$0")" && pwd)/install-cron.sh"
PLIST_TEMPLATE="$(cd "$(dirname "$0")/../cmd/zen-docs-cron" && pwd)/launchd.plist"

[ -x "$SCRIPT" ] && pass "install-cron.sh is executable" || fail "install-cron.sh not executable"

[ -f "$PLIST_TEMPLATE" ] && pass "launchd.plist template exists" || fail "launchd.plist template missing"

grep -q "com.zen-swarm.docs-cron" "$PLIST_TEMPLATE" && pass "Label key present" || fail "Label key missing"
grep -q "KeepAlive" "$PLIST_TEMPLATE" && pass "KeepAlive present" || fail "KeepAlive missing"
grep -q "RunAtLoad" "$PLIST_TEMPLATE" && pass "RunAtLoad present" || fail "RunAtLoad missing"
grep -q "StandardErrorPath" "$PLIST_TEMPLATE" && pass "StandardErrorPath present" || fail "StandardErrorPath missing"
grep -q "docs-cron.err" "$PLIST_TEMPLATE" && pass "stderr log path correct" || fail "stderr log path wrong"
grep -q "docs-cron" "$PLIST_TEMPLATE" && pass "docs-cron token present" || fail "docs-cron token missing"

USAGE_OUT="$(bash "$SCRIPT" 2>&1 || true)"
echo "$USAGE_OUT" | grep -q "usage\|Usage\|USAGE" && pass "usage on empty args" || fail "no usage on empty args"

NONEXISTENT_OUT="$(bash "$SCRIPT" /nonexistent/zen-docs-cron 2>&1 || true)"
echo "$NONEXISTENT_OUT" | grep -q "not executable\|not found\|error" \
    && pass "rejects non-existent binary" || fail "did not reject non-existent binary"

TMPDIR="$(mktemp -d)"
FAKE_BIN="$TMPDIR/zen-docs-cron"
echo '#!/bin/sh' > "$FAKE_BIN"
chmod +x "$FAKE_BIN"

launchctl() { echo "launchctl $*" >&2; }
export -f launchctl 2>/dev/null || true  # bash only; skip on zsh

cleanup() { rm -rf "$TMPDIR"; }
trap cleanup EXIT

pass "install-cron_test.sh: all checks done"

exit $FAIL
