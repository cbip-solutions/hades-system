#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
set -euo pipefail

PIN_FILE=".hermes-version"
if [[ ! -f "$PIN_FILE" ]]; then
    echo "FAIL: $PIN_FILE missing (release-gate pin file)"
    exit 6
fi
HERMES_VERSION_PIN="$(tr -d '[:space:]' < "$PIN_FILE")"

has_named_entry() {
    local name="$1"
    local text="$2"
    grep -Eq "^[[:space:]]*([│|][[:space:]]*)?${name}([[:space:]│|]|$)" <<< "$text"
}

HERMES_BINARY="$(command -v hermes 2>/dev/null || true)"
if [[ -z "$HERMES_BINARY" ]]; then
    echo "FAIL: hermes binary not found in PATH"
    echo "      remediate: brew install hermes-agent  # formula name (provides 'hermes' binary)"
    exit 1
fi

ACTUAL_VERSION="$(hermes --version 2>/dev/null | grep -Eo 'v?[0-9]+\.[0-9]+\.[0-9]+' | head -n1 | sed 's/^v//' || true)"
ACTUAL_VERSION="${ACTUAL_VERSION:-unknown}"
if [[ "$ACTUAL_VERSION" != "$HERMES_VERSION_PIN" ]]; then
    echo "FAIL: hermes version mismatch"
    echo "      expected: $HERMES_VERSION_PIN  (from $PIN_FILE)"
    echo "      actual:   $ACTUAL_VERSION"
    echo "      remediate: brew upgrade hermes-agent  OR  bump $PIN_FILE + operator-approved spike re-run"
    exit 2
fi

PLUGIN_LIST="$(hermes plugins list 2>/dev/null || true)"
if has_named_entry "hades" "$PLUGIN_LIST"; then
    PLUGIN_NAME="hades"
elif has_named_entry "zen-swarm" "$PLUGIN_LIST"; then
    PLUGIN_NAME="zen-swarm"
else
    echo "FAIL: hermes plugin hades not discovered"
    echo "      remediate: make plugin-install  # or symlink plugin/hades/ → ~/.hermes/plugins/hades"
    exit 3
fi

MCP_LIST="$(hermes mcp list 2>/dev/null || true)"
REQUIRED_MCP_SERVERS=(zen-mcp-research zen-mcp-budget zen-mcp-audit zen-mcp-sshexec)
MISSING_MCP_SERVERS=()
for server in "${REQUIRED_MCP_SERVERS[@]}"; do
    if ! has_named_entry "$server" "$MCP_LIST"; then
        MISSING_MCP_SERVERS+=("$server")
    fi
done
if [ "${#MISSING_MCP_SERVERS[@]}" -gt 0 ]; then
    echo "FAIL: ~/.hermes/config.yaml missing required mcp_servers: ${MISSING_MCP_SERVERS[*]}"
    echo "      remediate: run /hades:install-mcps in Hermes or merge plugin/hades/hermes-config-snippet.yaml"
    exit 4
fi

CONFIG_PATH="${HERMES_CONFIG_OVERRIDE:-${HOME}/.hermes/config.yaml}"
if [[ ! -f "$CONFIG_PATH" ]]; then
    echo "FAIL: $CONFIG_PATH missing"
    echo "      remediate: zen migrate claude-code  # produces config.yaml"
    exit 5
fi
if ! grep -Eq '(^[[:space:]]*-[[:space:]]*(hades|zen-swarm)[[:space:]]*$|enabled:.*(hades|zen-swarm))' "$CONFIG_PATH"; then
    echo "FAIL: $CONFIG_PATH missing HADES plugin enablement"
    echo "      remediate: add 'hades' under plugins.enabled"
    exit 5
fi

echo "OK: hermes v${ACTUAL_VERSION} (brew formula: hermes-agent) installed + plugin ${PLUGIN_NAME} loaded + required MCPs reachable + HADES config enabled"
exit 0
