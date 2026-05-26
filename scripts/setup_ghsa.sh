#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Copyright 2026 Ika el Zur

set -euo pipefail

if [[ $# -lt 1 ]]; then
    echo "Usage: $0 OWNER/REPO" >&2
    exit 2
fi

REPO="$1"

if [[ ! "$REPO" =~ ^[A-Za-z0-9._-]+/[A-Za-z0-9._-]+$ ]]; then
    echo "ERROR: argument must be of form OWNER/REPO; got '$REPO'" >&2
    exit 2
fi

if ! command -v gh >/dev/null 2>&1; then
    echo "ERROR: gh CLI not found on PATH; install via https://cli.github.com/" >&2
    exit 2
fi

echo "Enabling GHSA private vulnerability reporting on $REPO..."

gh api -X PUT "/repos/$REPO/private-vulnerability-reporting" || {
    echo "ERROR: gh api PUT private-vulnerability-reporting failed" >&2
    exit 1
}

enabled=$(gh api "/repos/$REPO/private-vulnerability-reporting" --jq '.enabled' 2>/dev/null || echo "")
if [[ "$enabled" != "true" ]]; then
    echo "FAIL: GHSA private vulnerability reporting not enabled (enabled: '$enabled')" >&2
    exit 1
fi

echo "OK: GHSA private vulnerability reporting enabled on $REPO"
echo "Reporters can submit advisories at: https://github.com/$REPO/security/advisories/new"

if ! gh api "/repos/$REPO/contents/.github/security" --silent 2>/dev/null; then
    echo "HINT: .github/security/ templates not present on $REPO yet —"
    echo "      run 'make sync-public-now' (per docs/operations/post-v1-dev-workflow.md)"
    echo "      to push the advisory + acknowledgment + embargo-policy templates."
fi

exit 0
