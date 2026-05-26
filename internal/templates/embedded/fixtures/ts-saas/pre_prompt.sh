#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
set -euo pipefail
if ! command -v node >/dev/null 2>&1; then
  echo "error: node not on PATH (Node 20+ required)" >&2
  exit 1
fi
