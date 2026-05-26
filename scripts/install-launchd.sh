#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
set -euo pipefail

CTLD_PATH="${1:-}"
if [ -z "$CTLD_PATH" ]; then
    if command -v zen-swarm-ctld >/dev/null 2>&1; then
        CTLD_PATH="$(command -v zen-swarm-ctld)"
    else
        echo "usage: $0 /path/to/zen-swarm-ctld" >&2
        exit 1
    fi
fi

if [ ! -x "$CTLD_PATH" ]; then
    echo "error: $CTLD_PATH is not executable" >&2
    exit 1
fi

LABEL="com.zenswarm.ctld"
PLIST_DIR="${HOME}/Library/LaunchAgents"
PLIST_PATH="${PLIST_DIR}/${LABEL}.plist"
TEMPLATE_PATH="$(cd "$(dirname "$0")/.." && pwd)/configs/launchd.plist.tmpl"

mkdir -p "$PLIST_DIR"
mkdir -p "${HOME}/.local/share/zen-swarm/logs"

sed -e "s|{{CTLD_PATH}}|${CTLD_PATH}|g" \
    -e "s|{{HOME}}|${HOME}|g" \
    "$TEMPLATE_PATH" > "$PLIST_PATH"

launchctl unload "$PLIST_PATH" 2>/dev/null || true
launchctl load -w "$PLIST_PATH"

echo "installed: $PLIST_PATH"
echo "started:   ${LABEL}"
echo
echo "verify:    zen daemon status"
echo "logs:      ~/.local/share/zen-swarm/logs/ctld.{stdout,stderr}.log"
echo "uninstall: launchctl unload \"$PLIST_PATH\" && rm \"$PLIST_PATH\""
