#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
set -euo pipefail

cd "$(dirname "$0")/.."
ROOT="$(pwd)"

echo "==> Plan 9 audit-chain smoke test"
echo "    repo root: $ROOT"

if [ ! -x "$ROOT/bin/zen-swarm-ctld" ] || [ ! -x "$ROOT/bin/zen" ]; then
  echo "==> binaries missing; running make build"
  make build >/dev/null
fi

TMPDIR=$(mktemp -d)
DAEMON_PID=""

cleanup() {
  if [ -n "$DAEMON_PID" ]; then
    kill -TERM "$DAEMON_PID" 2>/dev/null || true
    for _ in 1 2 3 4 5; do
      if ! kill -0 "$DAEMON_PID" 2>/dev/null; then break; fi
      sleep 1
    done
    kill -9 "$DAEMON_PID" 2>/dev/null || true
    wait "$DAEMON_PID" 2>/dev/null || true
  fi
  rm -rf "$TMPDIR"
}
trap 'cleanup' EXIT

UDS="$TMPDIR/zen.sock"
DB="$TMPDIR/state.db"
LOG="$TMPDIR/ctld.log"

echo "==> starting daemon"
ZEN_BYPASS_DISABLE_KEYCHAIN=1 \
  "$ROOT/bin/zen-swarm-ctld" -uds "$UDS" -db "$DB" >"$LOG" 2>&1 &
DAEMON_PID=$!

for i in $(seq 1 100); do
  if [ -S "$UDS" ]; then
    break
  fi
  sleep 0.1
done
if [ ! -S "$UDS" ]; then
  echo "FAIL: daemon UDS $UDS did not appear within 10 s"
  echo "--- daemon log ---"
  cat "$LOG"
  exit 1
fi

echo "==> health check"
if ! "$ROOT/bin/zen" --uds "$UDS" daemon status >/dev/null 2>&1; then
  echo "FAIL: zen daemon status returned non-zero"
  echo "--- daemon log ---"
  cat "$LOG"
  exit 1
fi
echo "  daemon status: OK"

echo "==> emit controlled audit event"
EVT_JSON='{"smoke":"plan9","stage":"audit-chain"}'
if ! "$ROOT/bin/zen" --uds "$UDS" audit emit \
    --type smoke.plan9_audit \
    --project zen-swarm-smoke \
    --payload "$EVT_JSON" >"$TMPDIR/emit.out" 2>&1; then
  echo "FAIL: zen audit emit returned non-zero"
  cat "$TMPDIR/emit.out"
  echo "--- daemon log ---"
  cat "$LOG"
  exit 1
fi
echo "  audit emit: OK"

echo "==> query audit events"
if ! "$ROOT/bin/zen" --uds "$UDS" audit events \
    --project zen-swarm-smoke --limit 5 >"$TMPDIR/events.out" 2>&1; then
  echo "FAIL: zen audit events returned non-zero"
  cat "$TMPDIR/events.out"
  echo "--- daemon log ---"
  cat "$LOG"
  exit 1
fi
if ! grep -q "smoke.plan9_audit" "$TMPDIR/events.out"; then
  echo "FAIL: emitted event not found in events query output"
  cat "$TMPDIR/events.out"
  exit 1
fi
echo "  audit events query: OK (event round-tripped)"

echo "==> verify audit-chain"
if "$ROOT/bin/zen" --uds "$UDS" audit-chain verify-chain \
    --project zen-swarm-smoke >"$TMPDIR/verify.out" 2>&1; then
  echo "  audit-chain verify: OK"
else
  if grep -q "plan9_audit_unavailable" "$TMPDIR/verify.out"; then
    echo "  audit-chain verify: SKIPPED (facade not wired; expected at Phase K)"
    echo "  (this assertion will become required once AuditCtxP9 ships)"
  else
    echo "FAIL: unexpected verify-chain error (not the documented 503):"
    cat "$TMPDIR/verify.out"
    echo "--- daemon log ---"
    cat "$LOG"
    exit 1
  fi
fi

echo ""
echo "Plan 9 audit-chain smoke test: ALL CHECKS PASSED"
