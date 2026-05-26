#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
ALLOWLIST_FILE="$PROJECT_ROOT/configs/personal-references-allowlist.yaml"
PY_BACKEND="$SCRIPT_DIR/verify_no_personal_references_backend.py"

TARGET_DIR="$PROJECT_ROOT"
CHANGED_PATHS_ONLY=0
DEBUG=0

while [[ $# -gt 0 ]]; do
    case "$1" in
        --target-dir) TARGET_DIR="$2"; shift 2 ;;
        --changed-paths-only) CHANGED_PATHS_ONLY=1; shift ;;
        --debug) DEBUG=1; shift ;;
        --help|-h)
            cat <<'USAGE'
Usage: verify_no_personal_references.sh [--target-dir DIR] [--changed-paths-only] [--debug]

Scans for the union of Phase J denylists. Exits 0 if clean; 1 if any leak
remains outside the YAML allowlist.

Denylist patterns:
  - hardcoded operator-identifying macOS/Linux home paths
  - operator OAuth device/account literals
  - device_id shape [a-f0-9]{64}  (except synthetic testdata — see allowlist)
  - Tailscale CGNAT RFC 6598 CGNAT range
  - IPv6 ULA IPv6 ULA range
  - Named operator username literals (repository namespace cascade exempted
    via backend lookahead)

Allowlist file: configs/personal-references-allowlist.yaml
USAGE
            exit 0 ;;
        *) echo "Unknown flag: $1" >&2; exit 2 ;;
    esac
done

if [[ ! -f "$ALLOWLIST_FILE" ]]; then
    echo "ERROR: allowlist file $ALLOWLIST_FILE not found" >&2
    exit 2
fi
if [[ ! -f "$PY_BACKEND" ]]; then
    echo "ERROR: Python backend $PY_BACKEND not found" >&2
    exit 2
fi

cd "$PROJECT_ROOT"

FILE_LIST="$(mktemp -t verify_no_personal_refs.XXXXXX)"
trap "rm -f \"$FILE_LIST\"" EXIT

if [[ $CHANGED_PATHS_ONLY -eq 1 ]]; then
    if [[ -z "${ZEN_SCRUB_CHANGED_PATHS:-}" ]]; then
        echo "ERROR: --changed-paths-only requires ZEN_SCRUB_CHANGED_PATHS env var" >&2
        exit 2
    fi
    printf '%s\n' $ZEN_SCRUB_CHANGED_PATHS > "$FILE_LIST"
else
    find "$TARGET_DIR" \
        -type d \( \
            -name .git -o -name vendor -o -name node_modules \
            -o -name .venv -o -name venv -o -name .tox \
            -o -name .mypy_cache -o -name .pytest_cache -o -name __pycache__ \
        \) -prune -o \
        -type f \( \
            -name '*.go' -o -name '*.md' -o -name '*.yaml' -o -name '*.yml' \
            -o -name '*.toml' -o -name '*.json' -o -name '*.sh' -o -name '*.bash' \
            -o -name '*.bats' -o -name '*.py' -o -name '*.txt' \
            -o -name 'LICENSE' -o -name 'NOTICE' -o -name 'README' \
            -o -name 'Makefile' \
        \) -print > "$FILE_LIST"
fi

python3 "$PY_BACKEND" "$ALLOWLIST_FILE" "$PROJECT_ROOT" "$DEBUG" < "$FILE_LIST"
