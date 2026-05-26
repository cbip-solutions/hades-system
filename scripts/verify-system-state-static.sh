#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
set -euo pipefail

if [ "$#" -ne 1 ]; then
  echo "usage: $0 <path-to-system-state.toml>" >&2
  exit 2
fi

FILE="$1"

if [ ! -f "$FILE" ]; then
  echo "FAIL: $FILE not found" >&2
  exit 1
fi

if [ ! -r "$FILE" ]; then
  echo "FAIL: $FILE not readable" >&2
  exit 1
fi

lines=$(wc -l <"$FILE" | tr -d '[:space:]')
if [ "${lines:-0}" -lt 30 ]; then
  echo "FAIL: $FILE has only $lines lines (floor 30)" >&2
  exit 1
fi

if ! head -1 "$FILE" | grep -q "system-state.toml"; then
  echo "FAIL: $FILE missing canonical header comment on line 1" >&2
  exit 1
fi

required_sections=(
  '\[zen-swarm\]'
  '\[plans\]'
  '\[invariants\]'
  '\[doctrines\]'
)
for sect in "${required_sections[@]}"; do
  if ! grep -qE "^${sect}" "$FILE"; then
    echo "FAIL: $FILE missing required section [${sect//\\/}]" >&2
    exit 1
  fi
done

if ! grep -qE '^[[:space:]]*version[[:space:]]*=' "$FILE"; then
  echo "FAIL: $FILE missing 'version' key under [zen-swarm]" >&2
  exit 1
fi

echo "OK: $FILE static check ($lines lines, required sections present)"
