#!/usr/bin/env bash
# SPDX-License-Identifier: MIT

set -euo pipefail

REPO_ROOT="${REPO_ROOT:-$(git rev-parse --show-toplevel 2>/dev/null || pwd)}"
cd "$REPO_ROOT"

errs=0

fail() {
    echo "FAIL: $*" >&2
    errs=$((errs + 1))
}

ok() {
    echo "OK: $*"
}

warn() {
    echo "WARN: $*" >&2
}

line_count() {
    wc -l <"$1" | tr -d ' '
}

require_file() {
    local path="$1"
    if [ ! -f "$path" ]; then
        fail "$path missing"
        return 1
    fi
    return 0
}

require_text() {
    local path="$1"
    local needle="$2"
    local label="$3"
    grep -qF "$needle" "$path" || fail "$path missing $label: $needle"
}

require_text_i() {
    local path="$1"
    local needle="$2"
    local label="$3"
    grep -qiF "$needle" "$path" || fail "$path missing $label: $needle"
}

require_regex() {
    local path="$1"
    local pattern="$2"
    local label="$3"
    grep -qE "$pattern" "$path" || fail "$path missing $label: $pattern"
}

if require_file LICENSE; then
    require_text LICENSE "MIT License" "MIT header"
    require_text LICENSE "Permission is hereby granted" "MIT grant phrase"
    if grep -qF "Apache License" LICENSE; then
        fail "LICENSE contains Apache License marker; Plan 15 decision 15 requires MIT"
    fi
    ok "LICENSE MIT canonical markers present"
fi

if require_file NOTICE; then
    ok "NOTICE present ($(line_count NOTICE) lines)"
else
    warn "NOTICE absent; optional under MIT, but recommended for inbound dependency attribution"
fi

if require_file README.md; then
    require_text README.md "What Plan 15 adds (v1.0.0" "Plan 15 v1.0 section"
    require_text README.md "hades-system" "public identity"
    require_text README.md "MIT" "MIT license framing"
    lines="$(line_count README.md)"
    if [ "$lines" -lt 600 ]; then
        fail "README.md too short for v1.0 public surface: ${lines} lines"
    else
        ok "README.md v1.0 surface present (${lines} lines)"
    fi
fi

if require_file CHANGELOG.md; then
    require_regex CHANGELOG.md '^## \[v1\.0\.0\] .+Plan 15' "v1.0.0 Plan 15 entry"
    for heading in "### Added" "### Changed" "### Fixed" "### Security"; do
        require_text CHANGELOG.md "$heading" "mandatory changelog section"
    done
    for forbidden in \
        "Anthropic anti-abuse" \
        "metadata.user_id" \
        "fingerprint coexistence" \
        "refresh-on-429" \
        "validator schema drift" \
        "gzip+deflate decompression"; do
        if grep -qF "$forbidden" CHANGELOG.md; then
            fail "CHANGELOG.md leaks bypass-tier internal marker: $forbidden"
        fi
    done
    ok "CHANGELOG.md v1.0 structure and bypass-curation markers checked"
fi

if require_file SECURITY.md; then
    require_text SECURITY.md "security/advisories/new" "GHSA primary channel"
    require_text SECURITY.md "hades-dev@proton.me" "backup contact"
    require_text SECURITY.md "90-day" "90-day disclosure window"
    require_text SECURITY.md "[bypass-tier]" "bypass-tier separate path"
    ok "SECURITY.md GHSA disclosure posture present"
fi

if require_file CONTRIBUTING.md; then
    require_text_i CONTRIBUTING.md "Developer Certificate of Origin" "DCO section"
    require_text CONTRIBUTING.md "git commit -s" "DCO command"
    require_text CONTRIBUTING.md "## Doctrine" "doctrine section"
    require_text_i CONTRIBUTING.md "dual-path triage" "dual-path PR triage"
    require_text CONTRIBUTING.md "post-v1-dev-workflow.md" "post-v1 workflow reference"
    ok "CONTRIBUTING.md DCO + doctrine + dual-path PR triage present"
fi

if require_file CODE_OF_CONDUCT.md; then
    require_text CODE_OF_CONDUCT.md "Contributor Covenant" "Contributor Covenant marker"
    require_text_i CODE_OF_CONDUCT.md "version 2.1" "Contributor Covenant version"
    require_text CODE_OF_CONDUCT.md "hades-dev@proton.me" "enforcement contact"
    if grep -qF "[INSERT CONTACT METHOD]" CODE_OF_CONDUCT.md; then
        fail "CODE_OF_CONDUCT.md still contains placeholder contact text"
    fi
    ok "CODE_OF_CONDUCT.md Contributor Covenant 2.1 markers present"
fi

handoff="docs/operations/handoff-v1.0.md"
if require_file "$handoff"; then
    for marker in \
        "## Stage 1" \
        "## Stage 2" \
        "## Stage 3" \
        "## Stage 4" \
        "Future tag schema" \
        "Cross-references" \
        "Z fresh-repo" \
        "cbip-solutions/hades-system" \
        "GHSA" \
        "Hermes peer"; do
        require_text "$handoff" "$marker" "handoff marker"
    done
    ok "$handoff cutover ritual markers present"
fi

hermes="docs/operations/hermes-compat.md"
if require_file "$hermes"; then
    for marker in "Compatibility matrix" "G2" "G3" "G4" "G5" "Upstream-PR" "fast-follow"; do
        require_text_i "$hermes" "$marker" "Hermes compatibility marker"
    done
    ok "$hermes compatibility matrix present"
fi

post_v1="docs/operations/post-v1-dev-workflow.md"
if require_file "$post_v1"; then
    for marker in "Modelo B" "Modelo 5" "Hybrid" "dual-path" "Modelo β" "Modelo α" "24h back-sync"; do
        require_text "$post_v1" "$marker" "post-v1 workflow marker"
    done
    ok "$post_v1 Modelo B workflow present"
fi

security_disclosure="docs/operations/security-disclosure.md"
if require_file "$security_disclosure"; then
    for marker in "90-day" "Reporter recognition" "[bypass-tier]" "hades-dev@proton.me"; do
        require_text_i "$security_disclosure" "$marker" "security-disclosure marker"
    done
    ok "$security_disclosure disclosure handbook present"
fi

echo ""
if [ "$errs" -gt 0 ]; then
    echo "FAIL: H-full doc bundle has ${errs} problem(s)" >&2
    exit 1
fi

echo "PASS: H-full doc bundle present and structurally valid"
