#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
set -euo pipefail

ANSWERS=$(cat)
NAME=$(echo "$ANSWERS" | python3 -c 'import sys,json; print(json.load(sys.stdin)["ProjectName"])')

if ! [[ "$NAME" =~ ^[a-z][a-z0-9-]{0,63}$ ]]; then
  echo "error: project name '$NAME' invalid; expected ^[a-z][a-z0-9-]{0,63}$" >&2
  exit 1
fi
