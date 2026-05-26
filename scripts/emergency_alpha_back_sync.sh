#!/usr/bin/env bash
# SPDX-License-Identifier: MIT

set -euo pipefail

REPO_ROOT="$(git rev-parse --show-toplevel 2>/dev/null || pwd)"
cd "$REPO_ROOT"

MODE="verify"
HOURS="${BACK_SYNC_HARD_INVARIANT_HOURS:-24}"
HADES_PUBLIC_ORG="${HADES_PUBLIC_ORG:-cbip-solutions}"
HADES_PUBLIC_REPO="${HADES_PUBLIC_REPO:-hades-system}"

while [ $# -gt 0 ]; do
    case "$1" in
        --verify) MODE="verify" ;;
        --apply) MODE="apply" ;;
        --hours=*) HOURS="${1#*=}" ;;
        --org=*) HADES_PUBLIC_ORG="${1#*=}" ;;
        --repo=*) HADES_PUBLIC_REPO="${1#*=}" ;;
        --help|-h)
            sed -n '2,/^$/p' "$0" | sed 's/^# \{0,1\}//'
            exit 0
            ;;
        *)
            echo "ERROR: unknown flag: $1" >&2
            exit 1
            ;;
    esac
    shift
done

PUBLIC_URL="https://github.com/${HADES_PUBLIC_ORG}/${HADES_PUBLIC_REPO}.git"

case "$MODE" in
    apply)
        echo "Operator back-sync recipe (decisión 14 sub-d):"
        echo ""
        echo "  # In this private repo:"
        echo "  git fetch ${PUBLIC_URL} main:refs/remotes/public/main"
        echo "  git log main..refs/remotes/public/main"
        echo "  # Review each commit; cherry-pick the legitimate hot-fix(es):"
        echo "  git cherry-pick -x <hot-fix-commit-sha>"
        echo ""
        echo "  # After back-sync, the next sync-public.yml run will reconcile."
        exit 0
        ;;
    verify)

        TMP="$(mktemp -d)"
        trap 'rm -rf "$TMP"' EXIT

        if ! git clone --depth 50 --quiet "${PUBLIC_URL}" "$TMP/public" 2>/dev/null; then
            echo "INFO: public repo unreachable (pre-flip, deleted, or wrong URL); invariant vacuously holds: ${PUBLIC_URL}"
            exit 0
        fi


        cd "$TMP/public"
        PUBLIC_HEAD_TIMESTAMP="$(git log -1 --pretty=%ct main 2>/dev/null || echo 0)"
        PUBLIC_HEAD_SUBJECT="$(git log -1 --pretty=%s main 2>/dev/null || echo unknown)"

        if [ "$PUBLIC_HEAD_TIMESTAMP" = "0" ]; then
            echo "INFO: public main has no commits; invariant vacuously holds"
            exit 0
        fi

        NOW="$(date +%s)"
        AGE_HOURS=$(( (NOW - PUBLIC_HEAD_TIMESTAMP) / 3600 ))

        cd "$REPO_ROOT"
        if git log --pretty=%s main -50 2>/dev/null | grep -Fxq "$PUBLIC_HEAD_SUBJECT"; then
            echo "INFO: public HEAD subject found in private last-50 commits; invariant satisfied"
            exit 0
        fi

        if [ "$AGE_HOURS" -gt "$HOURS" ]; then
            echo "VIOLATION: public HEAD commit older than ${HOURS}h SLA (age=${AGE_HOURS}h) and not back-synced to private" >&2
            echo "  subject: $PUBLIC_HEAD_SUBJECT" >&2
            echo "  back-sync via: bash scripts/emergency_alpha_back_sync.sh --apply" >&2
            exit 1
        fi

        echo "INFO: public HEAD age=${AGE_HOURS}h < ${HOURS}h SLA; operator has back-sync window remaining"
        exit 0
        ;;
    *)
        echo "ERROR: unknown mode: $MODE" >&2
        exit 1
        ;;
esac
