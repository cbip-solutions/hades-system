#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
set -euo pipefail

if ! command -v hermes >/dev/null 2>&1; then
  echo "error: hermes binary not on PATH" >&2
  exit 1
fi
if ! command -v go >/dev/null 2>&1; then
  echo "error: go toolchain not on PATH (companion daemon requires Go 1.25+)" >&2
  exit 1
fi
