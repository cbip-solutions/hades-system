#!/usr/bin/env bash
# SPDX-License-Identifier: MIT

set -euo pipefail

DRY_RUN=0
REPO="hades-system/hades-system"
RULESET_FILE=".github/rulesets/main-branch.json"

usage() {
    grep '^#' "$0" | sed 's/^# \{0,1\}//'
    exit 0
}

while [[ $# -gt 0 ]]; do
    case "$1" in
        --dry-run)
            DRY_RUN=1
            shift
            ;;
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

REPO_ROOT="$(git rev-parse --show-toplevel)"
RULESET_PATH="${REPO_ROOT}/${RULESET_FILE}"

if [[ ! -f "$RULESET_PATH" ]]; then
    echo "ERROR: ruleset file not found at ${RULESET_PATH}" >&2
    exit 1
fi

if command -v jq >/dev/null 2>&1; then
    if ! jq empty "$RULESET_PATH" >/dev/null 2>&1; then
        echo "ERROR: ${RULESET_PATH} is not valid JSON (jq parse failed)" >&2
        exit 1
    fi
    RULESET_NAME="$(jq -r '.name' "$RULESET_PATH")"
elif command -v python3 >/dev/null 2>&1; then
    if ! python3 -c "import json,sys; json.load(open(sys.argv[1]))" "$RULESET_PATH" 2>/dev/null; then
        echo "ERROR: ${RULESET_PATH} is not valid JSON (python3 parse failed)" >&2
        exit 1
    fi
    RULESET_NAME="$(python3 -c "import json,sys; print(json.load(open(sys.argv[1]))['name'])" "$RULESET_PATH")"
else
    echo "ERROR: neither jq nor python3 available; cannot validate JSON or extract ruleset name" >&2
    exit 1
fi

echo "Applying ruleset \"${RULESET_NAME}\" to ${REPO}..."

if [[ $DRY_RUN -eq 1 ]]; then
    echo "[DRY-RUN] gh api -X POST /repos/${REPO}/rulesets --input ${RULESET_PATH}"
    echo "[DRY-RUN] Skipping post-apply verification."
    exit 0
fi

command -v jq >/dev/null 2>&1 || {
    echo "ERROR: jq required for live apply (brew install jq / apt install jq)" >&2
    exit 1
}

echo "Checking for existing ruleset with name \"${RULESET_NAME}\"..."
EXISTING_ID="$(gh api "/repos/${REPO}/rulesets" \
    | jq -r --arg name "$RULESET_NAME" '.[] | select(.name == $name) | .id' \
    | head -1)"

if [[ -n "${EXISTING_ID:-}" && "${EXISTING_ID}" != "null" ]]; then
    echo "Existing ruleset id=${EXISTING_ID} found; updating via PUT..."
    if ! gh api -X PUT "/repos/${REPO}/rulesets/${EXISTING_ID}" --input "$RULESET_PATH"; then
        echo "ERROR: gh api -X PUT failed" >&2
        exit 2
    fi
else
    echo "No existing ruleset found; creating via POST..."
    if ! gh api -X POST "/repos/${REPO}/rulesets" --input "$RULESET_PATH"; then
        echo "ERROR: gh api -X POST failed" >&2
        exit 2
    fi
fi

echo "Verifying applied ruleset..."
APPLIED_NAME="$(gh api "/repos/${REPO}/rulesets" \
    | jq -r --arg name "$RULESET_NAME" '.[] | select(.name == $name) | .name' \
    | head -1)"

if [[ "${APPLIED_NAME:-}" != "${RULESET_NAME}" ]]; then
    echo "ERROR: post-apply verification failed; ruleset \"${RULESET_NAME}\" not found in /repos/${REPO}/rulesets" >&2
    exit 3
fi

echo "SUCCESS: ruleset \"${RULESET_NAME}\" applied to ${REPO}."
echo
echo "Required status checks now enforced on ${REPO} default branch:"
gh api "/repos/${REPO}/rulesets" \
    | jq -r --arg name "$RULESET_NAME" '
        .[] | select(.name == $name) | .rules[] |
        select(.type == "required_status_checks") |
        .parameters.required_status_checks[].context
      ' \
    | sed 's/^/  - /'

echo
echo "Verify post-apply posture via:"
echo "  gh api /repos/${REPO}/rulesets"
echo
echo "Rollback procedure (emergency only; see docs/operations/ci-aggregator.md §4):"
echo "  gh api -X DELETE /repos/${REPO}/rulesets/\$RULESET_ID"
