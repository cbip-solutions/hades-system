#!/usr/bin/env bash
# SPDX-License-Identifier: MIT

set -euo pipefail

QUARANTINE_FILE="${1:-scripts/release-gates/flake-quarantine.txt}"
MAX_AGE_SECONDS=$((14 * 24 * 60 * 60)) # 14 days in seconds

if [[ ! -f "$QUARANTINE_FILE" ]]; then
    echo "ERROR: quarantine file not found: ${QUARANTINE_FILE}" >&2
    exit 1
fi

parse_iso8601() {
    local ts="$1"
    if date -u -d "$ts" +%s 2>/dev/null; then
        return 0
    fi
    if date -u -j -f "%Y-%m-%dT%H:%M:%SZ" "$ts" +%s 2>/dev/null; then
        return 0
    fi
    if date -u -j -f "%Y-%m-%dT%H:%M:%S" "${ts%Z}" +%s 2>/dev/null; then
        return 0
    fi
    return 1
}

NOW_EPOCH=$(date -u +%s)

HEADER_TS=$(grep -E '^# Last review:' "$QUARANTINE_FILE" | head -1 | sed -E 's/^# Last review:[[:space:]]*//')

if [[ -z "$HEADER_TS" ]]; then
    echo "ERROR: ${QUARANTINE_FILE} missing \`# Last review: <ISO8601>\` header" >&2
    exit 1
fi

HEADER_EPOCH=$(parse_iso8601 "$HEADER_TS") || {
    echo "ERROR: \`# Last review\` timestamp \"${HEADER_TS}\" is not valid ISO8601 (RFC3339)" >&2
    exit 1
}

ENTRY_COUNT=$(grep -cvE '^[[:space:]]*(#.*)?$' "$QUARANTINE_FILE" || true)

HEADER_AGE_SECONDS=$((NOW_EPOCH - HEADER_EPOCH))
if [[ $ENTRY_COUNT -gt 0 && $HEADER_AGE_SECONDS -gt $MAX_AGE_SECONDS ]]; then
    HEADER_AGE_DAYS=$((HEADER_AGE_SECONDS / 86400))
    echo "ERROR: \`# Last review\` header is ${HEADER_AGE_DAYS}d old (>14d) with ${ENTRY_COUNT} entries — operator review required" >&2
    exit 1
fi

declare -a SEEN_NAMES=()
EXIT_CODE=0
LINE_NUM=0

while IFS= read -r LINE || [[ -n "$LINE" ]]; do
    LINE_NUM=$((LINE_NUM + 1))
    if [[ -z "$LINE" || "$LINE" =~ ^[[:space:]]*# ]]; then
        continue
    fi

    LINE_TRIMMED=$(echo "$LINE" | sed -E 's/^[[:space:]]+|[[:space:]]+$//g')
    # shellcheck disable=SC2206
    TOKENS=($LINE_TRIMMED)

    if [[ ${#TOKENS[@]} -ne 3 ]]; then
        echo "ERROR: line ${LINE_NUM}: malformed entry (expected 3 tokens; got ${#TOKENS[@]}): ${LINE_TRIMMED}" >&2
        EXIT_CODE=3
        continue
    fi

    TEST_NAME="${TOKENS[0]}"
    ENTRY_TS="${TOKENS[1]}"
    REASON_TAG="${TOKENS[2]}"

    if [[ -z "$TEST_NAME" || -z "$ENTRY_TS" || -z "$REASON_TAG" ]]; then
        echo "ERROR: line ${LINE_NUM}: empty token detected in entry: ${LINE_TRIMMED}" >&2
        EXIT_CODE=3
        continue
    fi

    if ! ENTRY_EPOCH=$(parse_iso8601 "$ENTRY_TS"); then
        echo "ERROR: line ${LINE_NUM}: timestamp \"${ENTRY_TS}\" is not valid ISO8601" >&2
        EXIT_CODE=3
        continue
    fi

    ENTRY_AGE_SECONDS=$((NOW_EPOCH - ENTRY_EPOCH))
    if [[ $ENTRY_AGE_SECONDS -ge $MAX_AGE_SECONDS ]]; then
        ENTRY_AGE_DAYS=$((ENTRY_AGE_SECONDS / 86400))
        echo "ERROR: line ${LINE_NUM}: entry \"${TEST_NAME}\" is ${ENTRY_AGE_DAYS}d old (≥14d) — expired (auto-expire boundary)" >&2
        if [[ $EXIT_CODE -eq 0 ]]; then
            EXIT_CODE=2
        fi
        continue
    fi

    for seen in "${SEEN_NAMES[@]:-}"; do
        if [[ "$seen" == "$TEST_NAME" ]]; then
            echo "ERROR: line ${LINE_NUM}: duplicate test-name \"${TEST_NAME}\"" >&2
            if [[ $EXIT_CODE -eq 0 ]]; then
                EXIT_CODE=4
            fi
            continue 2
        fi
    done
    SEEN_NAMES+=("$TEST_NAME")

done < "$QUARANTINE_FILE"

if [[ $EXIT_CODE -eq 0 ]]; then
    echo "OK: ${QUARANTINE_FILE} valid (${ENTRY_COUNT} entries; header ts=${HEADER_TS})"
fi

exit $EXIT_CODE
