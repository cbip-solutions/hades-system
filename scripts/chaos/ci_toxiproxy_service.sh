#!/usr/bin/env bash
# SPDX-License-Identifier: MIT

set -euo pipefail

CONTROL_HOST="${TOXIPROXY_CONTROL_HOST:-127.0.0.1}"
CONTROL_PORT="${TOXIPROXY_CONTROL_PORT:-8474}"
CONFIG_DIR="${HOME}/.config/zen-swarm"
CONFIG_JSON="${CONFIG_DIR}/toxiproxy-dev.json"
PID_FILE="${TOXIPROXY_PID_FILE:-/tmp/zen-toxiproxy-ci.pid}"
TOXIPROXY_VERSION="${TOXIPROXY_VERSION:-v2.11.0}"

install_binary() {
    if command -v toxiproxy-server >/dev/null 2>&1; then
        return 0
    fi
    case "$(uname -s)-$(uname -m)" in
        Linux-x86_64)
            curl -fsSL -o /tmp/toxiproxy \
                "https://github.com/Shopify/toxiproxy/releases/download/${TOXIPROXY_VERSION}/toxiproxy-server-linux-amd64"
            chmod +x /tmp/toxiproxy
            sudo mv /tmp/toxiproxy /usr/local/bin/toxiproxy-server
            ;;
        Linux-aarch64|Linux-arm64)
            curl -fsSL -o /tmp/toxiproxy \
                "https://github.com/Shopify/toxiproxy/releases/download/${TOXIPROXY_VERSION}/toxiproxy-server-linux-arm64"
            chmod +x /tmp/toxiproxy
            sudo mv /tmp/toxiproxy /usr/local/bin/toxiproxy-server
            ;;
        Darwin-*)
            if ! command -v brew >/dev/null 2>&1; then
                echo "ERROR: brew not found on darwin runner" >&2
                exit 2
            fi
            brew install toxiproxy
            ;;
        *)
            echo "ERROR: unsupported platform $(uname -s)-$(uname -m)" >&2
            exit 2
            ;;
    esac
}

emit_port_json() {
    mkdir -p "${CONFIG_DIR}"
    cat > "${CONFIG_JSON}" <<JSON
{
    "control_url": "http://${CONTROL_HOST}:${CONTROL_PORT}",
    "schema_version": "1.0",
    "rendered_by": "scripts/chaos/ci_toxiproxy_service.sh",
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
}

action="${1:-start}"
case "${action}" in
    start)
        install_binary
        toxiproxy-server -host "${CONTROL_HOST}" -port "${CONTROL_PORT}" &
        echo $! > "${PID_FILE}"
        emit_port_json
        attempts=0
        while [[ ${attempts} -lt 30 ]]; do
            if curl -fsS "http://${CONTROL_HOST}:${CONTROL_PORT}/version" >/dev/null 2>&1; then
                echo "[ci_toxiproxy_service] daemon up at ${CONTROL_HOST}:${CONTROL_PORT}"
                exit 0
            fi
            attempts=$((attempts + 1))
            sleep 1
        done
        echo "ERROR: toxiproxy daemon failed to come up within 30s" >&2
        exit 3
        ;;
    stop)
        if [[ -f "${PID_FILE}" ]]; then
            pid="$(cat "${PID_FILE}")"
            kill -TERM "${pid}" 2>/dev/null || true
            sleep 1
            kill -KILL "${pid}" 2>/dev/null || true
            rm -f "${PID_FILE}" "${CONFIG_JSON}"
        fi
        ;;
    probe)
        curl -fsS "http://${CONTROL_HOST}:${CONTROL_PORT}/version"
        ;;
    *)
        echo "Usage: $0 [start|stop|probe]" >&2
        exit 64
        ;;
esac
