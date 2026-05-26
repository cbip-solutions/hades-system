#!/usr/bin/env bash
# SPDX-License-Identifier: MIT

set -e

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
SCRIPT="$ROOT/scripts/verify-migrations.sh"

if [ ! -x "$SCRIPT" ]; then
  echo "FATAL: $SCRIPT not present or not executable"
  exit 1
fi

PASS_COUNT=0
FAIL_COUNT=0

run_case() {
  local name="$1" schema_ver="$2" migv_csv="$3" embed_csv="$4" slot_csv="$5" expect_exit="$6" expect_re="$7"
  local tmp out actual_exit
  tmp=$(mktemp -d)
  mkdir -p "$tmp/internal/store/migrations"
  mkdir -p "$tmp/scripts"

  if [ -z "$embed_csv" ]; then
    embed_csv="$migv_csv"
  fi

  {
    printf 'package store\n\n'
    printf 'const schemaVersion = %s\n\n' "$schema_ver"
    if [ -n "$migv_csv" ]; then
      IFS=',' read -ra MIGS <<< "$migv_csv"
      for n in "${MIGS[@]}"; do
        printf '//go:embed migrations/%03d_test.sql\nvar migrationV%s string\n\n' "$n" "$n"
      done
    fi
  } > "$tmp/internal/store/schema.go"

  if [ -n "$embed_csv" ]; then
    IFS=',' read -ra EMBED_FILES <<< "$embed_csv"
    for n in "${EMBED_FILES[@]}"; do
      : > "$tmp/internal/store/migrations/$(printf '%03d' "$n")_test.sql"
    done
  fi

  if [ -n "$slot_csv" ]; then
    IFS=',' read -ra SLOT_FILES <<< "$slot_csv"
    for n in "${SLOT_FILES[@]}"; do
      : > "$tmp/internal/store/migrations/$(printf '%03d' "$n")_slot.sql"
    done
  fi

  cp "$SCRIPT" "$tmp/scripts/verify-migrations.sh"
  chmod +x "$tmp/scripts/verify-migrations.sh"

  set +e
  out=$(cd "$tmp" && "$tmp/scripts/verify-migrations.sh" 2>&1)
  actual_exit=$?
  set -e

  if [ "$actual_exit" -ne "$expect_exit" ]; then
    echo "FAIL [$name]: expected exit $expect_exit, got $actual_exit"
    echo "--- captured stdout/stderr ---"
    echo "$out"
    echo "--- end captured ---"
    FAIL_COUNT=$((FAIL_COUNT+1))
    rm -rf "$tmp"
    return
  fi
  if [ -n "$expect_re" ] && ! printf '%s\n' "$out" | grep -qE "$expect_re"; then
    echo "FAIL [$name]: expected stdout to match /$expect_re/"
    echo "--- actual ---"
    echo "$out"
    echo "--- end ---"
    FAIL_COUNT=$((FAIL_COUNT+1))
    rm -rf "$tmp"
    return
  fi

  echo "OK   [$name]"
  PASS_COUNT=$((PASS_COUNT+1))
  rm -rf "$tmp"
}

run_case happy-plan7 28 \
  "$(seq -s, 2 28)" \
  "" \
  "57,58,60,61,62,63" \
  0 "verify-migrations: OK"

GAP_CSV=$(seq -s, 2 25)",27,28"
run_case gap-at-26 28 \
  "$GAP_CSV" \
  "" \
  "57,58,60,61,62,63" \
  1 "gap detected at migration number 26"

run_case schema-mismatch 27 \
  "$(seq -s, 2 28)" \
  "" \
  "57,58,60,61,62,63" \
  1 "schemaVersion=27 but highest migrationV<N>=28"

run_case pre-plan7 23 \
  "$(seq -s, 2 23)" \
  "" \
  "" \
  0 "verify-migrations: OK"

DANGLING_EMBED=$(seq -s, 2 27)
run_case embed-dangling 28 \
  "$(seq -s, 2 28)" \
  "$DANGLING_EMBED" \
  "57,58,60,61,62,63" \
  1 "embed target.*028_test.sql.*missing"

run_case slot-057-missing 28 \
  "$(seq -s, 2 28)" \
  "" \
  "58,60,61" \
  1 "Plan 7 reserved file slot 057 missing"

echo ""
echo "Results: $PASS_COUNT passed, $FAIL_COUNT failed"
if [ "$FAIL_COUNT" -gt 0 ]; then
  exit 1
fi
echo "All cases passed."
