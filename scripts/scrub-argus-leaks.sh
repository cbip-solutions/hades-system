#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

DRY_RUN=0
VERBOSE=0
EXTRA_TARGETS=()

while [[ $# -gt 0 ]]; do
    case "$1" in
        --dry-run) DRY_RUN=1; shift ;;
        --verbose) VERBOSE=1; shift ;;
        --extra-target) EXTRA_TARGETS+=("$2"); shift 2 ;;
        --help|-h)
            cat <<'USAGE'
Usage: scrub-argus-leaks.sh [--dry-run] [--verbose] [--extra-target FILE]

Mechanically scrub argus / reference-project references from LEAK-class files.
Preserves LEGIT-PROVENANCE allowlist files (ADR corpus + foundational ADRs).
USAGE
            exit 0 ;;
        *) echo "Unknown flag: $1" >&2; exit 2 ;;
    esac
done

cd "$PROJECT_ROOT"

declare -a LEGIT_FILES=(
    "docs/decisions/0002-six-stage-pipeline-doctrine-weighted.md"
    "docs/decisions/0003-single-multi-tenant-daemon.md"
    "docs/decisions/0004-hierarchical-workforce-doctrine-bounded.md"
    "docs/decisions/0005-hierarchical-review-architecture.md"
    "docs/decisions/0006-research-sota-always-integrated.md"
    "docs/decisions/0007-gitnexus-integration-vendor-mode.md"
    "docs/release/J-1-argus-triage.md"
    "docs/superpowers/plans/2026-05-25-plan-15-phase-J-privacy-identity-scrub.md"
    "docs/superpowers/specs/2026-05-15-zen-swarm-plan-15-release-polish-design.md"
    "configs/personal-references-allowlist.yaml"
    "scripts/scrub-argus-leaks.sh"
    "tests/scripts/scrub_argus_leaks_test.bats"
    "tests/compliance/inv_zen_330_no_personal_refs_test.go"
)

is_legit() {
    local file="$1"
    for legit in "${LEGIT_FILES[@]}"; do
        if [[ "$file" == "$legit" ]]; then
            return 0
        fi
    done
    return 1
}

log() {
    if [[ $VERBOSE -eq 1 ]]; then
        echo "$@"
    fi
}

discover_targets() {
    grep -rl --include="*.go" --include="*.md" --include="*.yaml" \
             --include="*.yml" --include="*.toml" --include="*.json" \
             --include="*.sh" --include="*.py" --include="*.txt" \
             "argus" . 2>/dev/null \
        | sed -e 's|^\./||' \
        | sort -u
}

apply_scrub_to_file() {
    local file="$1"

    if [[ $DRY_RUN -eq 1 ]]; then
        log "SCRUB[dry]: $file"
        return 0
    fi

    log "SCRUB: $file"
    local tmp
    tmp="$(mktemp -t scrub.XXXXXX)"
    cp "$file" "$tmp"

    sed -E \
        -e 's/argus_uru/internal_platform_x/g' \
        -e 's/Argus_uru/Internal_Platform_X/g' \
        -e 's/ARGUS_URU/INTERNAL_PLATFORM_X/g' \
        -e 's/reference-project/internal-platform-x/g' \
        -e 's/Argus-uru/Internal-Platform-X/g' \
        -e 's/ARGUS-URU/INTERNAL_PLATFORM_X/g' \
        "$tmp" > "$file.scrub.tmp"
    mv "$file.scrub.tmp" "$tmp"

    python3 - "$tmp" "$file" <<'PYTHON'
import re, sys
src, dst = sys.argv[1], sys.argv[2]
with open(src, 'r', encoding='utf-8', errors='surrogateescape') as f:
    text = f.read()

# Identifier-class context: [A-Za-z0-9_]
ident = r'[A-Za-z0-9_]'

# Order matters: longer first to avoid double-replacement.
def repl(pattern, replacement, s):
    return re.sub(pattern, replacement, s)

# Standalone case-preserving rules. We use word-boundary in the
# letter/digit/underscore sense (Python \b). Dash-separated compounds
# like "pin-argus-1" or "non-argus" are LEAK and should be scrubbed too.
text = repl(r'\bARGUS\b', 'INTERNAL_PLATFORM_X', text)
text = repl(r'\bArgus\b', 'Internal-Platform-X', text)
# argusDB camel-case compound (rare): preserve camel-case shape.
text = repl(r'\bargusDB\b', 'internalPlatformXDB', text)
text = repl(r'\bargus\b', 'internal-platform-x', text)

with open(dst, 'w', encoding='utf-8', errors='surrogateescape') as f:
    f.write(text)
PYTHON

    rm -f "$tmp"
}

TARGETS=$(discover_targets)
NUM_TOTAL=0
NUM_LEAK=0
NUM_LEGIT=0
NUM_SCRUBBED=0
NUM_UNCHANGED=0

for file in $TARGETS; do
    NUM_TOTAL=$((NUM_TOTAL + 1))

    if is_legit "$file"; then
        NUM_LEGIT=$((NUM_LEGIT + 1))
        log "PRESERVE: $file (LEGIT-PROVENANCE)"
        continue
    fi

    NUM_LEAK=$((NUM_LEAK + 1))
    apply_scrub_to_file "$file"
    if [[ $DRY_RUN -eq 0 ]]; then
        NUM_SCRUBBED=$((NUM_SCRUBBED + 1))
    fi
done

for extra in "${EXTRA_TARGETS[@]:-}"; do
    [[ -z "$extra" ]] && continue
    NUM_TOTAL=$((NUM_TOTAL + 1))
    if is_legit "$extra"; then
        log "PRESERVE: $extra (LEGIT-PROVENANCE, extra)"
    else
        log "SCRUB: $extra (extra)"
        if [[ $DRY_RUN -eq 0 ]]; then
            apply_scrub_to_file "$extra"
            NUM_SCRUBBED=$((NUM_SCRUBBED + 1))
        fi
    fi
done

if [[ $DRY_RUN -eq 1 ]]; then
    echo "DRY-RUN summary: $NUM_TOTAL total, $NUM_LEGIT preserve, $NUM_LEAK would-scrub"
else
    echo "Apply summary: $NUM_TOTAL total, $NUM_LEGIT preserve, $NUM_SCRUBBED scrubbed"
fi

exit 0
