#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
set -euo pipefail
if ! command -v go >/dev/null 2>&1; then
  echo "error: go toolchain not on PATH (Go 1.25+ required)" >&2
  exit 1
fi
