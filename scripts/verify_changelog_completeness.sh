#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
#     release tag MUST carry CHANGELOG narrative).
# invariant: every git v* tag MUST have CHANGELOG entry OR allowlist row.

set -euo pipefail

REPO_ROOT="$(git rev-parse --show-toplevel 2>/dev/null || pwd)"
cd "$REPO_ROOT"

CHANGELOG="${CHANGELOG_PATH:-CHANGELOG.md}"
ALLOWLIST="${CHANGELOG_ALLOWLIST_PATH:-configs/changelog-omission-allowlist.yaml}"

if [ ! -f "$CHANGELOG" ]; then
    echo "ERROR: CHANGELOG not found at $CHANGELOG" >&2
    exit 3
fi

ALLOWLIST_TAGS=()
declare -a ALLOWLIST_RATIONALES=()
if [ -f "$ALLOWLIST" ]; then
    current_tag=""
    while IFS= read -r line; do
        if [[ "$line" =~ ^[[:space:]]*-[[:space:]]*tag:[[:space:]]*[\"\']?(v[0-9]+\.[0-9]+\.[0-9]+)[\"\']?[[:space:]]*$ ]]; then
            current_tag="${BASH_REMATCH[1]}"
            ALLOWLIST_TAGS+=("$current_tag")
            ALLOWLIST_RATIONALES+=("")
            ver="${current_tag#v}"
            major="${ver%%.*}"
            if [ "$major" -ge 1 ] 2>/dev/null; then
                echo "ERROR: allowlist contains tag $current_tag >= v1.0.0; flip-aware policy rejects (Plan 15 decisión 8 — every v1.0+ release tag MUST have CHANGELOG narrative in the public repo)" >&2
                exit 2
            fi
            continue
        fi
        if [ -n "$current_tag" ] && [[ "$line" =~ ^[[:space:]]+rationale:[[:space:]]*(.*)$ ]]; then
            raw="${BASH_REMATCH[1]}"
            raw="${raw%\"}"
            raw="${raw#\"}"
            raw="${raw%\'}"
            raw="${raw#\'}"
            raw="${raw%"${raw##*[![:space:]]}"}"
            idx=$((${#ALLOWLIST_RATIONALES[@]} - 1))
            ALLOWLIST_RATIONALES[idx]="$raw"
            current_tag=""
        fi
    done < "$ALLOWLIST"

    # Rationale-is-load-bearing gate: every allowlist entry MUST carry a
    for i in "${!ALLOWLIST_TAGS[@]}"; do
        tag="${ALLOWLIST_TAGS[$i]}"
        rationale="${ALLOWLIST_RATIONALES[$i]}"
        if [ -z "$rationale" ]; then
            echo "ERROR: allowlist entry $tag has empty rationale; the rationale field is load-bearing (Plan 15 decisión 8 — future writers need the audit trail)" >&2
            exit 2
        fi
    done
fi

TAGS=()
while IFS= read -r t; do
    [ -z "$t" ] && continue
    TAGS+=("$t")
done < <(git tag --list 'v*' --sort=-creatordate)

if [ "${#TAGS[@]}" -eq 0 ]; then
    echo "INFO: no v* tags found; CHANGELOG completeness vacuously satisfied"
    exit 0
fi

MISSING=()
for tag in "${TAGS[@]}"; do
    esc_tag="${tag//./\\.}"
    if grep -qE "^## \[${esc_tag}\]" "$CHANGELOG"; then
        continue
    fi
    found_in_allowlist=0
    for at in "${ALLOWLIST_TAGS[@]:-}"; do
        if [ "$at" = "$tag" ]; then
            found_in_allowlist=1
            break
        fi
    done
    if [ "$found_in_allowlist" -eq 0 ]; then
        MISSING+=("$tag")
    fi
done

if [ "${#MISSING[@]}" -gt 0 ]; then
    echo "FAIL: CHANGELOG completeness gate — ${#MISSING[@]} tag(s) lack CHANGELOG entry + allowlist row:" >&2
    for t in "${MISSING[@]}"; do
        echo "  - $t" >&2
    done
    echo "" >&2
    echo "Remediation options:" >&2
    echo "  (a) Add a '## [<tag>]' section to $CHANGELOG with release narrative." >&2
    echo "  (b) Add an allowlist row to $ALLOWLIST with rationale (pre-v1.0 only)." >&2
    exit 1
fi

echo "PASS: CHANGELOG completeness gate (${#TAGS[@]} tag(s); ${#ALLOWLIST_TAGS[@]} allowlisted; inv-zen-276)"
exit 0
