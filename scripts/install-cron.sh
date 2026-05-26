#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
set -euo pipefail

LABEL="com.zen-swarm.docs-cron"
PLIST_DIR="${HOME}/Library/LaunchAgents"
PLIST_PATH="${PLIST_DIR}/${LABEL}.plist"
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PLIST_TEMPLATE="${SCRIPT_DIR}/../cmd/zen-docs-cron/launchd.plist"
LOG_DIR="${HOME}/.local/share/zen-swarm/logs"

CRON_BIN_PATH=""
UNINSTALL=false
TIMEZONE="America/Argentina/Buenos_Aires"  # project standing default per spec §2.9

while [[ $# -gt 0 ]]; do
    case "$1" in
        --uninstall)
            UNINSTALL=true
            shift
            ;;
        --timezone)
            TIMEZONE="${2:-}"
            shift 2
            ;;
        -*)
            echo "error: unknown flag $1" >&2
            echo "usage: $0 [/path/to/zen-docs-cron] [--uninstall] [--timezone <IANA-tz>]" >&2
            exit 1
            ;;
        *)
            CRON_BIN_PATH="$1"
            shift
            ;;
    esac
done

if $UNINSTALL; then
    if [ -f "$PLIST_PATH" ]; then
        launchctl unload "$PLIST_PATH" 2>/dev/null || true
        rm -f "$PLIST_PATH"
        echo "uninstalled: ${LABEL}"
    else
        echo "not installed: ${PLIST_PATH} not found"
    fi
    exit 0
fi


if [ -z "$CRON_BIN_PATH" ]; then
    if command -v zen-docs-cron >/dev/null 2>&1; then
        CRON_BIN_PATH="$(command -v zen-docs-cron)"
    else
        echo "usage: $0 /path/to/zen-docs-cron" >&2
        echo "       (or ensure zen-docs-cron is in \$PATH)" >&2
        exit 1
    fi
fi

if [ ! -x "$CRON_BIN_PATH" ]; then
    echo "error: $CRON_BIN_PATH is not executable" >&2
    exit 1
fi

if [ ! -f "$PLIST_TEMPLATE" ]; then
    echo "error: plist template not found at $PLIST_TEMPLATE" >&2
    echo "       run from the zen-swarm repo root or pass full binary path" >&2
    exit 1
fi

mkdir -p "$PLIST_DIR"
mkdir -p "$LOG_DIR"

sed -e "s|{{CRON_BIN_PATH}}|${CRON_BIN_PATH}|g" \
    -e "s|{{HOME}}|${HOME}|g" \
    -e "s|{{TIMEZONE}}|${TIMEZONE}|g" \
    "$PLIST_TEMPLATE" > "$PLIST_PATH"

launchctl unload "$PLIST_PATH" 2>/dev/null || true
launchctl load -w "$PLIST_PATH"

echo "installed: $PLIST_PATH"
echo "started:   ${LABEL}"
echo
echo "verify:    launchctl list | grep docs-cron"
echo "logs:      ${LOG_DIR}/docs-cron.{out,err}"
echo "uninstall: $0 --uninstall"
