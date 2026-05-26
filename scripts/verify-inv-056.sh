#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# inv-zen-056 (persist-before-upstream): the bypass module MUST persist

set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
IDEM="$ROOT/private-tier1-module/idempotency.go"
DISP="$ROOT/private-tier1-module/dispatch.go"
EXIT=0

if [ ! -f "$IDEM" ] || [ ! -f "$DISP" ]; then
  echo "verify-inv-056: bypass module not found; skipping (pre-Phase H)"
  exit 0
fi

if ! grep -qE '^func persistBeforeUpstream\(\)' "$IDEM"; then
  echo "ERROR (inv-zen-056): persistBeforeUpstream sentinel missing from $IDEM"
  echo "  The compile-half of inv-zen-056 anchors that the runtime"
  echo "  ordering contract has a named symbol to test for. Restore it."
  EXIT=1
fi

if ! grep -qE '^var _ = persistBeforeUpstream' "$IDEM"; then
  echo "ERROR (inv-zen-056): 'var _ = persistBeforeUpstream' anchor missing from $IDEM"
  echo "  Without the anchor, dead-code elimination may drop the sentinel."
  EXIT=1
fi

mp_line=$(grep -nE 'idempotency\.MarkPending\(|i\.MarkPending\(' "$DISP" \
  | grep -vE '^[0-9]+:[[:space:]]*//' \
  | head -n1 | cut -d: -f1 || true)
do_line=$(grep -nE 'c\.httpClient\.Do\(|httpClient\.Do\(' "$DISP" \
  | grep -vE '^[0-9]+:[[:space:]]*//' \
  | head -n1 | cut -d: -f1 || true)

if [ -z "$mp_line" ]; then
  echo "ERROR (inv-zen-056): no idempotency.MarkPending call found in $DISP"
  echo "  Forward must persist the 'pending' row before the upstream call."
  EXIT=1
elif [ -z "$do_line" ]; then
  echo "ERROR (inv-zen-056): no httpClient.Do call found in $DISP"
  echo "  Cannot statically verify ordering without an upstream call site."
  EXIT=1
elif [ "$mp_line" -ge "$do_line" ]; then
  echo "ERROR (inv-zen-056): MarkPending (line $mp_line) is not before httpClient.Do (line $do_line) in $DISP"
  echo "  Reorder so the 'pending' row is durable before the upstream call leaves the daemon."
  EXIT=1
fi

if [ $EXIT -eq 0 ]; then
  echo "verify-inv-056: OK (sentinel present, MarkPending precedes httpClient.Do at line $mp_line < $do_line)"
fi
exit $EXIT
