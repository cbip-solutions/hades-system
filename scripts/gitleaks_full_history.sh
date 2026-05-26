#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
set -euo pipefail

REPORT_DIR="${ZEN_GITLEAKS_REPORT_DIR:-$HOME/.cache/hades-system}"
mkdir -p "$REPORT_DIR"
REPORT_PATH="$REPORT_DIR/gitleaks-pre-flip.json"
ALL_REFS_REPORT_PATH="$REPORT_DIR/gitleaks-all-refs.json"

if ! command -v gitleaks >/dev/null 2>&1; then
    echo "ERROR: gitleaks not installed." >&2
    echo "       Install via: brew install gitleaks" >&2
    echo "       Or: see https://github.com/gitleaks/gitleaks#installation" >&2
    exit 1
fi

GITLEAKS_VERSION="$(gitleaks version 2>&1 | head -1 || echo unknown)"
echo "gitleaks version: $GITLEAKS_VERSION" >&2

LEAKS_FOUND=0

echo "-> Phase 1: scanning HEAD + working tree..." >&2
set +e
gitleaks detect \
    --source . \
    --report-format json \
    --report-path "$REPORT_PATH" \
    --log-level info \
    --no-banner >/dev/null 2>&1
HEAD_SCAN_EXIT=$?
set -e

case "$HEAD_SCAN_EXIT" in
    0)
        echo "   HEAD scan: clean (0 leaks)" >&2
        ;;
    1)
        LEAKS_FOUND=1
        echo "" >&2
        echo "   LEAKS DETECTED in HEAD / working tree." >&2
        echo "   Report: $REPORT_PATH" >&2
        if command -v jq >/dev/null 2>&1; then
            jq -r '.[] | "     - \(.File):\(.StartLine) [\(.RuleID)]"' "$REPORT_PATH" 2>/dev/null >&2 || true
        else
            head -200 "$REPORT_PATH" >&2 2>/dev/null || true
        fi
        ;;
    *)
        echo "ERROR: gitleaks HEAD scan failed with exit $HEAD_SCAN_EXIT" >&2
        exit 2
        ;;
esac

echo "-> Phase 2: scanning all refs (branches + tags + reflog)..." >&2
set +e
gitleaks detect \
    --source . \
    --log-opts="--all" \
    --report-format json \
    --report-path "$ALL_REFS_REPORT_PATH" \
    --log-level info \
    --no-banner >/dev/null 2>&1
REFS_SCAN_EXIT=$?
set -e

case "$REFS_SCAN_EXIT" in
    0)
        echo "   all-refs scan: clean (0 leaks)" >&2
        ;;
    1)
        LEAKS_FOUND=1
        echo "" >&2
        echo "   LEAKS DETECTED in branch / tag history." >&2
        echo "   Report: $ALL_REFS_REPORT_PATH" >&2
        if command -v jq >/dev/null 2>&1; then
            jq -r '.[] | "     - \(.File):\(.StartLine) [\(.RuleID)] commit=\(.Commit // "unknown")"' "$ALL_REFS_REPORT_PATH" 2>/dev/null >&2 || true
        else
            head -200 "$ALL_REFS_REPORT_PATH" >&2 2>/dev/null || true
        fi
        ;;
    *)
        echo "ERROR: gitleaks all-refs scan failed with exit $REFS_SCAN_EXIT" >&2
        exit 2
        ;;
esac

if [ "$LEAKS_FOUND" -ne 0 ]; then
    echo "" >&2
    echo "NOTE under decisión 1 (Z fresh-repo): a leak in PRIVATE history does" >&2
    echo "      NOT automatically block the C-9 public-flip event. The public" >&2
    echo "      snapshot does not inherit private history; archaeology is" >&2
    echo "      structurally impossible. Operator decides whether to remediate" >&2
    echo "      private history (e.g., 'git filter-repo --replace-text <file>')" >&2
    echo "      per docs/operations/public-repo-flip-playbook.md §1.1." >&2
    echo "      The C-13 build_public_snapshot.sh has its OWN snapshot-tree" >&2
    echo "      gitleaks gate; that gate IS load-bearing for C-9." >&2
    exit 3
fi

PRE_PLAN2_DATE="2026-04-29"
PRE_PLAN2_COUNT="$(git log --before="$PRE_PLAN2_DATE" --oneline 2>/dev/null | wc -l | tr -d ' ')"
echo "" >&2
echo "================================================================" >&2
echo "  gitleaks full-history scan: PASS (zero leaks across all refs)" >&2
echo "  Reports:" >&2
echo "    HEAD:     $REPORT_PATH" >&2
echo "    all-refs: $ALL_REFS_REPORT_PATH" >&2
echo "" >&2
echo "  Pre-Plan-2 commits (before $PRE_PLAN2_DATE; before inv-zen-061" >&2
echo "  pre-commit hook active in v0.2.0): $PRE_PLAN2_COUNT commits." >&2
echo "  Operator SHOULD manually review these per defense-in-depth," >&2
echo "  even though gitleaks reports clean (rule coverage is not exhaustive)." >&2
echo "================================================================" >&2

exit 0
