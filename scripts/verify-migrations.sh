#!/usr/bin/env bash
# SPDX-License-Identifier: MIT

set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
SCHEMA_GO="$ROOT/internal/store/schema.go"
EXIT=0

if [ ! -f "$SCHEMA_GO" ]; then
  echo "verify-migrations: $SCHEMA_GO not found"
  exit 1
fi

SCHEMA_VER=$(grep -E '^[[:space:]]*(const[[:space:]]+)?schemaVersion[[:space:]]*(int)?[[:space:]]*=[[:space:]]*[0-9]+' "$SCHEMA_GO" \
  | head -n1 \
  | sed -E 's|//.*$||' \
  | grep -oE '[0-9]+' \
  | head -n1)

if [ -z "${SCHEMA_VER:-}" ]; then
  echo "FAIL: could not parse schemaVersion constant from $SCHEMA_GO"
  echo "      Expected a line matching: [const ]schemaVersion[ int] = NN"
  exit 1
fi
echo "Parsed schemaVersion: $SCHEMA_VER"

MIG_NUMS_RAW=$(grep -E '^var[[:space:]]+migrationV[0-9]+[[:space:]]+string' "$SCHEMA_GO" \
  | grep -oE 'migrationV[0-9]+' \
  | grep -oE '[0-9]+' \
  | sort -un)

if [ -z "${MIG_NUMS_RAW:-}" ]; then
  echo "FAIL: no migrationV<N> declarations discovered in $SCHEMA_GO"
  echo "      Expected one or more lines matching: var migrationV<N> string"
  exit 1
fi

MIGRATION_NUMS=()
while IFS= read -r n; do
  [ -z "$n" ] && continue
  MIGRATION_NUMS+=( "$n" )
done <<< "$MIG_NUMS_RAW"

echo "Discovered migrationV constants: ${MIGRATION_NUMS[*]}"

EXPECTED=2
HIGHEST=1
for n in "${MIGRATION_NUMS[@]}"; do
  if [ "$n" -ne "$EXPECTED" ]; then
    if [ "$n" -lt "$EXPECTED" ]; then
      echo "FAIL: migrationV$n appears AFTER higher number (sort invariant violated)"
    else
      echo "FAIL: gap detected at migration number $EXPECTED (next found: $n)"
    fi
    EXIT=1
  fi
  HIGHEST="$n"
  EXPECTED=$((n+1))
done

if [ "$SCHEMA_VER" -ne "$HIGHEST" ]; then
  echo "FAIL: schemaVersion=$SCHEMA_VER but highest migrationV<N>=$HIGHEST"
  echo "      Update internal/store/schema.go: const schemaVersion = $HIGHEST"
  EXIT=1
fi

EMBED_PATHS=$(grep -E '^//go:embed[[:space:]]+[^[:space:]]+' "$SCHEMA_GO" \
  | sed -E 's|^//go:embed[[:space:]]+||' \
  | sed -E 's|[[:space:]].*$||')

if [ -n "${EMBED_PATHS:-}" ]; then
  while IFS= read -r p; do
    [ -z "$p" ] && continue
    full="$ROOT/internal/store/$p"
    if [ ! -f "$full" ]; then
      echo "FAIL: //go:embed target $p missing on disk (resolved: $full)"
      EXIT=1
    fi
  done <<< "$EMBED_PATHS"
fi

if [ "$HIGHEST" -ge 24 ]; then
  MIG_DIR="$ROOT/internal/store/migrations"
  SCHEMA_DIR="$ROOT/internal/store/schema"
  for slot in 57 58 60 61; do
    found=0
    for d in "$MIG_DIR" "$SCHEMA_DIR"; do
      [ -d "$d" ] || continue
      if compgen -G "$d/$(printf '%03d' "$slot")_*.sql" > /dev/null; then
        found=1
        break
      fi
    done
    if [ "$found" -eq 0 ]; then
      echo "FAIL: Plan 7 reserved file slot $(printf '%03d' "$slot") missing"
      echo "      Expected at least one .sql file with prefix $(printf '%03d' "$slot")_"
      echo "      under internal/store/migrations/ or internal/store/schema/."
      echo "      Reference: master plan §\"Migration numbering coordination\""
      EXIT=1
    fi
  done
fi

if [ "$EXIT" -eq 0 ]; then
  echo "verify-migrations: OK (schemaVersion=$SCHEMA_VER, v1 inline + ${#MIGRATION_NUMS[@]} named constants contiguous 2..$HIGHEST)"
else
  echo ""
  echo "verify-migrations FAILED — see errors above."
  echo "Reference: docs/superpowers/plans/2026-05-01-plan-7-multiproject-master.md §\"Migration numbering coordination\""
fi

exit "$EXIT"
