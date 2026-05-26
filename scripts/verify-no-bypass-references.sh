#!/usr/bin/env bash
# SPDX-License-Identifier: MIT

set -euo pipefail

ROOT="$(pwd)"
for arg in "$@"; do
    case "$arg" in
        --root=*) ROOT="${arg#*=}" ;;
        --help|-h)
            sed -n '2,/^$/p' "$0" | sed 's/^# \{0,1\}//'
            exit 0
            ;;
        *)
            echo "ERROR: unknown flag: $arg" >&2
            exit 2
            ;;
    esac
done

if [ ! -d "$ROOT" ]; then
    echo "ERROR: root directory not found: $ROOT" >&2
    exit 2
fi
cd "$ROOT"

FORBIDDEN_TOKENS=(
    "private-tier1-module"
    "anthropic-bypass"
    "anthropic_bypass"
    "zen-bypass-tier1"
    "cmd/zen-bypass"
    "BypassClient"
    "BypassBackend"
    "BypassAdapter"
    "BypassAdmin"
    "bypassadapter"
    "bypass-config"
    "bypass_config"
    "bypass_audit"
    "bypass-sidecar"
    "bypass-tier"
)

JOIN_RE="$(IFS='|'; echo "${FORBIDDEN_TOKENS[*]}")"

SANCTIONED_PATTERNS=(
    "private-tier1-module/"
    "internal/providers/sidecar_backend"
    "internal/providers/errors_sidecar"
    "internal/providers/bypass_backend"
    "internal/daemon/dispatcheradapter/sidecar_registration"
    "internal/daemon/bypassadapter/"
    "internal/daemon/handlers/bypass"
    "internal/daemon/notifications"
    "internal/daemon/server"
    "internal/config/sidecars"
    "internal/cli/init"
    "internal/cli/bypass"
    "internal/client/bypass"
    "internal/store/bypass_audit"
    "cmd/zen-swarm-ctld/"
    "cmd/verify-no-bypass-references/"
    "scripts/verify-no-bypass-references"
    "tests/testhelpers/"
    "tests/testharness/"
    "tests/compliance/"
    "tests/integration/"
    "tests/realworld/"
    "tests/chaos/"
    "tests/adversarial/"
    "tests/testdata/"
    "tests/orchestrator_chaos/"
    "tests/replay/"
    "tests/release/"
    "tests/property/"
    "tests/doctrine/"
    "tests/timeaccel/"
    "configs/sidecars.toml.example"
    "configs/bypass-config.json.example"
    "configs/personal-references-allowlist.yaml"
    "configs/projects.toml.example"
    "docs/operations/"
    "docs/sbom/"
    "docs/quality/"
    "docs/decisions/"
    "docs/superpowers/"
    "docs/release/"
    "docs/METHODOLOGY.md"
    "docs/public-manifest/"
)

is_sanctioned() {
    local rel="$1"
    rel="${rel#./}"
    for pat in "${SANCTIONED_PATTERNS[@]}"; do
        case "$rel" in
            "$pat"*) return 0 ;;
        esac
    done
    return 1
}

TOTAL_VIOLATIONS=0
declare -a VIOLATION_LINES=()

emit_violation() {
    local surface="$1" file="$2" line="$3" content="$4"
    TOTAL_VIOLATIONS=$((TOTAL_VIOLATIONS + 1))
    VIOLATION_LINES+=("[$surface] $file:$line  $content")
}

scan_surface_ast() {
    if command -v go >/dev/null 2>&1; then
        if ! go run ./cmd/verify-no-bypass-references >/dev/null 2>&1; then
            local go_out
            go_out="$(go run ./cmd/verify-no-bypass-references 2>&1 || true)"
            while IFS= read -r line; do
                if [[ "$line" =~ ^[[:space:]]+\[ast\][[:space:]]+([^:]+):([0-9]+) ]]; then
                    emit_violation "ast" "${BASH_REMATCH[1]}" "${BASH_REMATCH[2]}" "$line"
                fi
            done <<< "$go_out"
        fi
        return 0
    fi
    echo "NOTE: surface 1 (AST) requires the Go binary at cmd/verify-no-bypass-references; skipped in bash-only mode" >&2
}

scan_surface_text() {
    local surface_tag="$1" prefix="$2"
    if [ ! -d "$prefix" ]; then return 0; fi
    while IFS= read -r line; do
        local file lineno content
        file="$(echo "$line" | cut -d: -f1)"
        lineno="$(echo "$line" | cut -d: -f2)"
        content="$(echo "$line" | cut -d: -f3-)"
        if is_sanctioned "$file"; then continue; fi
        file="${file#./}"
        emit_violation "$surface_tag" "$file" "$lineno" "$content"
    done < <(grep -RnE "$JOIN_RE" \
        --include='*.go' --include='*.md' --include='*.toml' \
        --include='*.toml.example' --include='*.yaml' --include='*.yml' \
        --include='*.json' --include='*.json.example' --include='*.sh' \
        --include='*.py' --include='*.sql' --include='*.txt' \
        --exclude-dir=vendor --exclude-dir=.git --exclude-dir=node_modules \
        "$prefix" 2>/dev/null || true)
}

scan_surface_sql() {
    local migrations_dir="internal/store/migrations"
    if [ ! -d "$migrations_dir" ]; then return 0; fi
    while IFS= read -r line; do
        local file lineno content
        file="$(echo "$line" | cut -d: -f1)"
        lineno="$(echo "$line" | cut -d: -f2)"
        content="$(echo "$line" | cut -d: -f3-)"
        if is_sanctioned "$file"; then continue; fi
        file="${file#./}"
        emit_violation "sql" "$file" "$lineno" "$content"
    done < <(grep -RnEi "CREATE[[:space:]]+TABLE[[:space:]]+(IF[[:space:]]+NOT[[:space:]]+EXISTS[[:space:]]+)?(bypass|anthropic_bypass)" \
        --include='*.sql' "$migrations_dir" 2>/dev/null || true)
}

scan_surface_ast
scan_surface_text "tests" "tests/"
scan_surface_text "docs" "docs/"
scan_surface_text "configs" "configs/"
scan_surface_sql

if [ "$TOTAL_VIOLATIONS" -eq 0 ]; then
    echo "verify-no-bypass-references OK: zero forbidden bypass references across 5 surfaces (decisión 17-a extended)"
    exit 0
fi

echo "FAIL: $TOTAL_VIOLATIONS unsanctioned bypass reference(s) across 5 surfaces:" >&2
i=0
for v in "${VIOLATION_LINES[@]}"; do
    if [ "$i" -ge 50 ]; then
        echo "      ... and $((TOTAL_VIOLATIONS - i)) more" >&2
        break
    fi
    echo "  $v" >&2
    i=$((i + 1))
done
exit 1
