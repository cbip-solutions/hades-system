#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
set -euo pipefail

BINARY="${VERIFY_SPIKES_BIN:-bin/verify-spikes}"

if [[ ! -x "$BINARY" ]]; then
    echo "Building $BINARY ..."
    go build -o "$BINARY" ./cmd/verify-spikes
fi

exec "$BINARY" "$@"
