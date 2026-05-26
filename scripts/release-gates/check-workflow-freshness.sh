#!/usr/bin/env bash
# SPDX-License-Identifier: MIT

set -euo pipefail

CHAOS_MAX_AGE_DAYS="${CHAOS_MAX_AGE_DAYS:-7}"
DOCTRINE_MAX_AGE_DAYS="${DOCTRINE_MAX_AGE_DAYS:-7}"
NIGHTLY_BYPASS_MAX_AGE_DAYS="${NIGHTLY_BYPASS_MAX_AGE_DAYS:-14}"

REPO=""

usage() {
    grep '^#' "$0" | sed 's/^# \{0,1\}//'
    exit 0
}

while [[ $# -gt 0 ]]; do
    case "$1" in
        --repo)
            REPO="$2"
            shift 2
            ;;
        -h|--help)
            usage
            ;;
        *)
            echo "ERROR: unknown flag $1" >&2
            echo "Use --help for usage." >&2
            exit 1
            ;;
    esac
done

command -v gh >/dev/null 2>&1 || {
    echo "ERROR: gh CLI not installed (https://cli.github.com)" >&2
    exit 1
}
command -v jq >/dev/null 2>&1 || {
    echo "ERROR: jq not installed (brew install jq / apt install jq)" >&2
    exit 1
}

if [[ -z "$REPO" ]]; then
    if [[ -n "${GITHUB_REPOSITORY:-}" ]]; then
        REPO="$GITHUB_REPOSITORY"
    elif git rev-parse --is-inside-work-tree >/dev/null 2>&1; then
        REPO="$(git remote get-url origin 2>/dev/null | sed -E 's|.*github\.com[:/]([^/]+/[^/.]+)(\.git)?$|\1|' || true)"
    fi
    if [[ -z "$REPO" ]]; then
        REPO="cbip-solutions/hades-system"
        echo "WARN: repo unresolvable; falling back to default: ${REPO}" >&2
    fi
fi

echo "Checking cross-workflow freshness for ${REPO}..."
echo "Thresholds: chaos.yml ≤${CHAOS_MAX_AGE_DAYS}d | doctrine-pre-release.yml ≤${DOCTRINE_MAX_AGE_DAYS}d | nightly-bypass-probe.yml ≤${NIGHTLY_BYPASS_MAX_AGE_DAYS}d"
echo

NOW_EPOCH="$(date -u +%s)"
EXIT_CODE=0

parse_iso8601() {
    local ts="$1"
    if date -u -d "$ts" +%s 2>/dev/null; then
        return 0
    fi
    if date -u -j -f "%Y-%m-%dT%H:%M:%SZ" "$ts" +%s 2>/dev/null; then
        return 0
    fi
    return 1
}

check_workflow() {
    local workflow_file="$1"
    local max_age_days="$2"
    local max_age_seconds=$((max_age_days * 24 * 60 * 60))

    local response
    if ! response="$(gh api "/repos/${REPO}/actions/workflows/${workflow_file}/runs?status=success&per_page=1" 2>&1)"; then
        echo "ERROR: ${workflow_file}: gh api query failed: ${response}" >&2
        EXIT_CODE=1
        return
    fi

    local total_count
    total_count="$(echo "$response" | jq -r '.total_count // 0')"
    if [[ "$total_count" == "0" ]]; then
        echo "FAIL: ${workflow_file}: no successful runs found ever" >&2
        if [[ $EXIT_CODE -lt 3 ]]; then
            EXIT_CODE=3
        fi
        return
    fi

    local last_success_ts
    last_success_ts="$(echo "$response" | jq -r '.workflow_runs[0].updated_at')"
    if [[ -z "$last_success_ts" || "$last_success_ts" == "null" ]]; then
        echo "FAIL: ${workflow_file}: latest successful run has no updated_at timestamp" >&2
        if [[ $EXIT_CODE -lt 3 ]]; then
            EXIT_CODE=3
        fi
        return
    fi

    local last_epoch
    if ! last_epoch="$(parse_iso8601 "$last_success_ts")"; then
        echo "FAIL: ${workflow_file}: invalid timestamp \"${last_success_ts}\"" >&2
        EXIT_CODE=1
        return
    fi

    local age_seconds=$((NOW_EPOCH - last_epoch))
    local age_days=$((age_seconds / 86400))

    if [[ $age_seconds -gt $max_age_seconds ]]; then
        echo "FAIL: ${workflow_file}: last success ${age_days}d ago (>${max_age_days}d threshold; ts=${last_success_ts})" >&2
        if [[ $EXIT_CODE -lt 2 ]]; then
            EXIT_CODE=2
        fi
    else
        echo "OK:   ${workflow_file}: last success ${age_days}d ago (≤${max_age_days}d; ts=${last_success_ts})"
    fi
}

check_workflow "chaos.yml" "$CHAOS_MAX_AGE_DAYS"
check_workflow "doctrine-pre-release.yml" "$DOCTRINE_MAX_AGE_DAYS"
check_workflow "nightly-bypass-probe.yml" "$NIGHTLY_BYPASS_MAX_AGE_DAYS"

echo

case $EXIT_CODE in
    0)
        echo "SUCCESS: all 3 cross-workflows are fresh."
        ;;
    1)
        echo "ERROR: prerequisite missing or gh api failure." >&2
        ;;
    2)
        echo "ERROR: one or more workflows are stale. Re-run via:" >&2
        echo "  gh workflow run chaos --ref main" >&2
        echo "  gh workflow run doctrine-pre-release --ref main" >&2
        echo "  gh workflow run nightly-bypass-probe --ref main" >&2
        ;;
    3)
        echo "ERROR: one or more workflows have no successful runs ever (first run pending)." >&2
        ;;
esac

exit $EXIT_CODE
