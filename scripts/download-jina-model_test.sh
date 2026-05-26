#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
#   6. shellcheck clean
set -euo pipefail

FAIL=0
pass() { echo "PASS: $1"; }
fail() { echo "FAIL: $1"; FAIL=1; }

SCRIPT="$(cd "$(dirname "$0")" && pwd)/download-jina-model.sh"

[ -x "$SCRIPT" ] && pass "download-jina-model.sh is executable" || fail "download-jina-model.sh not executable"

HELP_OUT="$(bash "$SCRIPT" --help 2>&1)" || { fail "--help non-zero exit"; }
echo "$HELP_OUT" | grep -qi "usage\|Usage" && pass "--help prints usage" || fail "--help missing usage line"
echo "$HELP_OUT" | grep -q "download-jina-model" && pass "--help mentions script name" || fail "--help missing script name"

TMPHOME="$(mktemp -d)"
DRY_OUT="$(HOME="$TMPHOME" XDG_DATA_HOME= bash "$SCRIPT" --dry-run 2>&1)" || { fail "--dry-run non-zero exit"; }
if echo "$DRY_OUT" | grep -q "$TMPHOME/.local/share/zen-swarm/models/jina-code"; then
  pass "default dest uses ~/.local/share when XDG_DATA_HOME empty"
else
  fail "default dest fallback wrong: $DRY_OUT"
fi
DRY_OUT_XDG="$(HOME="$TMPHOME" XDG_DATA_HOME="$TMPHOME/xdg" bash "$SCRIPT" --dry-run 2>&1)" || { fail "--dry-run XDG non-zero exit"; }
if echo "$DRY_OUT_XDG" | grep -q "$TMPHOME/xdg/zen-swarm/models/jina-code"; then
  pass "default dest uses XDG_DATA_HOME when set"
else
  fail "default dest XDG wrong: $DRY_OUT_XDG"
fi
rm -rf "$TMPHOME"

TMPDIR="$(mktemp -d)"
echo "not-the-real-model-bytes" > "$TMPDIR/model.onnx"
echo "0000000000000000000000000000000000000000000000000000000000000000" > "$TMPDIR/expected-sha"
set +e
SHA_OUT="$(bash "$SCRIPT" --dest "$TMPDIR" --verify-only 2>&1)"
SHA_RC=$?
set -e
if [ "$SHA_RC" -ne 0 ]; then
  pass "SHA mismatch exits non-zero"
else
  fail "SHA mismatch did not exit non-zero (output: $SHA_OUT)"
fi
echo "$SHA_OUT" | grep -qi "mismatch\|expected" && pass "SHA mismatch error message mentions mismatch" || fail "SHA mismatch error too vague"
rm -rf "$TMPDIR"

TMPDIR="$(mktemp -d)"
echo "fake-model-bytes-for-test" > "$TMPDIR/model.onnx"
ACTUAL_SHA="$(shasum -a 256 "$TMPDIR/model.onnx" | awk '{print $1}')"
echo "$ACTUAL_SHA" > "$TMPDIR/expected-sha"
set +e
IDEMP_OUT="$(bash "$SCRIPT" --dest "$TMPDIR" --verify-only 2>&1)"
IDEMP_RC=$?
set -e
if [ "$IDEMP_RC" -eq 0 ]; then
  pass "matching SHA verify-only exits 0 (idempotent)"
else
  fail "matching SHA verify-only failed: $IDEMP_OUT (rc=$IDEMP_RC)"
fi
echo "$IDEMP_OUT" | grep -qi "valid\|already\|present\|ok" && pass "idempotent message" || fail "idempotent message missing"
rm -rf "$TMPDIR"

TMPDIR="$(mktemp -d)"
echo "fake-model-bytes-pin-test" > "$TMPDIR/model.onnx"
[ ! -f "$TMPDIR/expected-sha" ] || fail "test setup: expected-sha should not exist initially"
set +e
PIN_OUT="$(bash "$SCRIPT" --dest "$TMPDIR" --pin-sha 2>&1)"
PIN_RC=$?
set -e
if [ "$PIN_RC" -eq 0 ]; then
  pass "--pin-sha exits 0"
else
  fail "--pin-sha non-zero: $PIN_OUT"
fi
[ -f "$TMPDIR/expected-sha" ] && pass "--pin-sha wrote expected-sha" || fail "--pin-sha did not write expected-sha"
WROTE_SHA="$(cat "$TMPDIR/expected-sha")"
EXPECTED_SHA="$(shasum -a 256 "$TMPDIR/model.onnx" | awk '{print $1}')"
[ "$WROTE_SHA" = "$EXPECTED_SHA" ] && pass "pinned SHA matches model" || fail "pinned SHA mismatch ($WROTE_SHA vs $EXPECTED_SHA)"
rm -rf "$TMPDIR"

# Test 6: shellcheck clean (if shellcheck installed)
if command -v shellcheck >/dev/null 2>&1; then
  if shellcheck "$SCRIPT" >/dev/null 2>&1; then
    pass "shellcheck clean"
  else
    SHCHK_OUT="$(shellcheck "$SCRIPT" 2>&1 || true)"
    fail "shellcheck found issues: $SHCHK_OUT"
  fi
else
  echo "SKIP: shellcheck not installed"
fi

if grep -q "jinaai/jina-embeddings-v2-base-code" "$SCRIPT"; then
  pass "canonical HuggingFace URL present"
else
  fail "canonical HuggingFace URL missing"
fi
if grep -q "<PIN_AT_FIRST_RUN>" "$SCRIPT"; then
  fail "PLACEHOLDER token leaked into committed script"
else
  pass "no PLACEHOLDER token in committed script"
fi

if [ "$FAIL" -ne 0 ]; then
  echo ""
  echo "download-jina-model_test.sh: FAIL"
  exit 1
fi
echo ""
echo "download-jina-model_test.sh: all checks passed"
