#!/usr/bin/env bash
# SPDX-License-Identifier: MIT

set -euo pipefail

PLIST_LABEL="com.hades-system.zen-toxiproxy-dev"
PLIST_PATH="${HOME}/Library/LaunchAgents/${PLIST_LABEL}.plist"
CONFIG_DIR="${HOME}/.config/zen-swarm"
CONFIG_JSON="${CONFIG_DIR}/toxiproxy-dev.json"
LOG_DIR="${HOME}/Library/Logs/zen-swarm"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

action="${1:-install}"

probe_binary() {
    if command -v toxiproxy-server >/dev/null 2>&1; then
        command -v toxiproxy-server
        return 0
    fi
    if [[ -x /opt/homebrew/bin/toxiproxy-server ]]; then
        echo /opt/homebrew/bin/toxiproxy-server
        return 0
    fi
    if [[ -x /usr/local/bin/toxiproxy-server ]]; then
        echo /usr/local/bin/toxiproxy-server
        return 0
    fi
    if [[ "$(uname -s)" == "Darwin" ]]; then
        echo "ERROR: toxiproxy-server not found. Install via:" >&2
        echo "  brew install toxiproxy" >&2
    else
        echo "ERROR: toxiproxy-server not found. See https://github.com/Shopify/toxiproxy/releases" >&2
    fi
    exit 2
}

emit_port_json() {
    mkdir -p "${CONFIG_DIR}"
    cat > "${CONFIG_JSON}" <<JSON
{
    "control_url": "http://127.0.0.1:8474",
    "schema_version": "1.0",
    "rendered_by": "scripts/setup_toxiproxy_dev.sh",
    "edges": {
        "hermes_plugin":             { "listen": "127.0.0.1:39001", "upstream_env": "ZEN_TEST_HERMES_UPSTREAM" },
        "ctld":                      { "listen": "127.0.0.1:39002", "upstream_env": "ZEN_TEST_CTLD_UPSTREAM" },
        "providers_anthropic_paygo": { "listen": "127.0.0.1:39003", "upstream_env": "ZEN_TEST_PAYGO_UPSTREAM" },
        "providers_gemini":          { "listen": "127.0.0.1:39004", "upstream_env": "ZEN_TEST_GEMINI_UPSTREAM" },
        "mcp_research":              { "listen": "127.0.0.1:39005", "upstream_env": "ZEN_TEST_MCP_RESEARCH_UPSTREAM" },
        "mcp_budget":                { "listen": "127.0.0.1:39006", "upstream_env": "ZEN_TEST_MCP_BUDGET_UPSTREAM" },
        "mcp_audit":                 { "listen": "127.0.0.1:39007", "upstream_env": "ZEN_TEST_MCP_AUDIT_UPSTREAM" },
        "sidecar_bypass":            { "listen": "127.0.0.1:39008", "upstream_env": "ZEN_TEST_SIDECAR_BYPASS_UPSTREAM" }
    }
}
JSON
    echo "[setup_toxiproxy_dev] wrote ${CONFIG_JSON}"
}

case "${action}" in
    install)
        bin_path="$(probe_binary)"
        mkdir -p "${LOG_DIR}" "$(dirname "${PLIST_PATH}")"
        TOXIPROXY_BIN="${bin_path}" \
            "${SCRIPT_DIR}/chaos/render_toxiproxy_plist.sh" \
            > "${PLIST_PATH}.tmp"
        mv "${PLIST_PATH}.tmp" "${PLIST_PATH}"
        echo "[setup_toxiproxy_dev] installed plist: ${PLIST_PATH}"
        launchctl bootout "gui/$(id -u)/${PLIST_LABEL}" 2>/dev/null || true
        launchctl bootstrap "gui/$(id -u)" "${PLIST_PATH}"
        echo "[setup_toxiproxy_dev] bootstrapped launchd service"
        emit_port_json
        ;;
    --uninstall|uninstall)
        launchctl bootout "gui/$(id -u)/${PLIST_LABEL}" 2>/dev/null || true
        rm -f "${PLIST_PATH}" "${CONFIG_JSON}"
        echo "[setup_toxiproxy_dev] uninstalled"
        ;;
    --print-config|print-config)
        if [[ -f "${CONFIG_JSON}" ]]; then
            cat "${CONFIG_JSON}"
        else
            echo "ERROR: ${CONFIG_JSON} missing; run setup_toxiproxy_dev.sh first" >&2
            exit 1
        fi
        ;;
    *)
        echo "Usage: $0 [install|--uninstall|--print-config]" >&2
        exit 64
        ;;
esac
