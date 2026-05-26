#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
set -euo pipefail

cd "$(dirname "$0")/.."
ROOT="$(pwd)"

echo "==> building binaries"
make build >/dev/null

TMPDIR=$(mktemp -d)

cleanup() {
    if [ -n "${DAEMON_PID:-}" ]; then
        kill -TERM "$DAEMON_PID" 2>/dev/null || true
        wait "$DAEMON_PID" 2>/dev/null || true
    fi
    rm -rf "$TMPDIR"
}
trap 'cleanup' EXIT

UDS="$TMPDIR/zen.sock"
DB="$TMPDIR/state.db"

echo "==> starting daemon"
ZEN_BYPASS_DISABLE_KEYCHAIN=1 \
    "$ROOT/bin/zen-swarm-ctld" -uds "$UDS" -db "$DB" >"$TMPDIR/ctld.log" 2>&1 &
DAEMON_PID=$!

for i in {1..50}; do
    if [ -S "$UDS" ]; then break; fi
    sleep 0.1
done
if [ ! -S "$UDS" ]; then
    echo "FAIL: daemon socket did not appear"
    cat "$TMPDIR/ctld.log"
    exit 1
fi

echo "==> /v1/health"
HEALTH=$(curl -sf --unix-socket "$UDS" http://unix/v1/health)
echo "    $HEALTH"
if ! echo "$HEALTH" | grep -q '"status":"ok"'; then
    echo "FAIL: health did not return ok"
    exit 1
fi

echo "==> POST /v1/events (3 events)"
RESP=$(curl -sf --unix-socket "$UDS" \
    -H "Content-Type: application/json" \
    -X POST http://unix/v1/events \
    -d '[
        {"type":"smoke.test.a","project":"smoke","payload_json":"{}"},
        {"type":"smoke.test.b","project":"smoke","payload_json":"{}"},
        {"type":"smoke.test.c","project":"smoke","payload_json":"{}"}
    ]')
echo "    $RESP"
if ! echo "$RESP" | grep -q '"accepted":3'; then
    echo "FAIL: events POST did not return accepted=3"
    exit 1
fi

echo "==> waiting 200ms for batcher flush"
sleep 0.2

echo "==> verifying SQLite has the events"
COUNT=$(sqlite3 "$DB" "SELECT COUNT(*) FROM events WHERE project='smoke'")
echo "    events.count(project=smoke) = $COUNT"
if [ "$COUNT" != "3" ]; then
    echo "FAIL: expected 3 events in SQLite, got $COUNT"
    exit 1
fi

echo "==> 501 stub convention (POST /v1/swarms -> X-Zen-Plan: 5)"
HEADERS=$(curl -si --unix-socket "$UDS" -X POST http://unix/v1/swarms -d '{}')
echo "$HEADERS" | grep -i '^http' | head -1
if ! echo "$HEADERS" | grep -qi '^x-zen-plan: 5'; then
    echo "FAIL: /v1/swarms did not return X-Zen-Plan: 5"
    exit 1
fi

echo "==> zen daemon status"
ZEN_SWARM_CTLD="$ROOT/bin/zen-swarm-ctld" "$ROOT/bin/zen" daemon status --uds "$UDS"

echo "==> zen doctor"
"$ROOT/bin/zen" doctor --uds "$UDS"

echo
echo "OK SMOKE TEST PASSED"
