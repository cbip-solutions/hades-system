#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
set -euo pipefail
if ! command -v python3 >/dev/null 2>&1; then
  echo "error: python3 not on PATH (Python 3.11+ required)" >&2
  exit 1
fi
