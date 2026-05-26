#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
set -euo pipefail

if ! command -v hermes >/dev/null 2>&1; then
  echo "error: hermes binary not on PATH" >&2
  echo "       install Hermes >=0.13.0 from https://github.com/hermes-agent/hermes-agent" >&2
  exit 1
fi

VER=$(hermes --version 2>/dev/null | head -1 | awk '{print $NF}' || echo "0.0.0")
MAJ=$(echo "$VER" | cut -d. -f1)
MIN=$(echo "$VER" | cut -d. -f2)
if [ "$MAJ" -lt 1 ] && { [ "$MAJ" -lt 0 ] || [ "$MIN" -lt 13 ]; }; then
  echo "error: hermes $VER < 0.13.0 required" >&2
  exit 1
fi
