#!/usr/bin/env bats

# Plan 15 Phase A Task A-2 — verify_hermes.sh probe tests
#
# Amendment §2.2 D-2: pre-release gate verifying Hermes binary present,
# version pinned, plugin loaded, MCP server reachable.

setup() {
    PROJECT_ROOT="$(cd "${BATS_TEST_DIRNAME}/../.." && pwd)"
    cd "${PROJECT_ROOT}"
    # Stub hermes binary on PATH for deterministic tests.
    TMPBIN="$(mktemp -d)"
    cat > "${TMPBIN}/hermes" <<'HERMES'
#!/usr/bin/env bash
case "${1:-}" in
    --version) echo "hermes 0.14.0" ;;
    plugins)   echo "hades" ;;
    mcp)       printf '%s\n' zen-mcp-research zen-mcp-budget zen-mcp-audit zen-mcp-sshexec ;;
    --probe-mcp) exit 0 ;;
    *) exit 0 ;;
esac
HERMES
    chmod +x "${TMPBIN}/hermes"
    export PATH="${TMPBIN}:${PATH}"
    # Stub config.yaml.
    export HERMES_CONFIG_DIR="$(mktemp -d)/.hermes"
    mkdir -p "${HERMES_CONFIG_DIR}"
    cat > "${HERMES_CONFIG_DIR}/config.yaml" <<'CFG'
mcp_servers:
  zen-mcp-research:
    command: zen-mcp-research
    args: []
  zen-mcp-budget:
    command: zen-mcp-budget
    args: []
  zen-mcp-audit:
    command: zen-mcp-audit
    args: []
  zen-mcp-sshexec:
    command: zen-mcp-sshexec
    args: []
plugins:
  enabled:
    - hades
CFG
    export HERMES_CONFIG_OVERRIDE="${HERMES_CONFIG_DIR}/config.yaml"
}

teardown() {
    rm -rf "${TMPBIN}" "${HERMES_CONFIG_DIR}"
}

@test ".hermes-version file present" {
    [ -f .hermes-version ]
}

@test ".hermes-version contains semver" {
    run grep -E '^[0-9]+\.[0-9]+\.[0-9]+$' .hermes-version
    [ "$status" -eq 0 ]
}

@test "scripts/verify_hermes.sh present + executable" {
    [ -x "scripts/verify_hermes.sh" ]
}

@test "verify_hermes.sh PASS when binary + version + hades plugin + MCP + config present" {
    run bash scripts/verify_hermes.sh
    [ "$status" -eq 0 ]
    [[ "$output" == *"OK: hermes"* ]]
}

@test "verify_hermes.sh parses real Hermes Agent version banner" {
    cat > "${TMPBIN}/hermes" <<'HERMES'
#!/usr/bin/env bash
case "${1:-}" in
    --version) echo "Hermes Agent v0.14.0 (2026.5.16)" ;;
    plugins)   echo "│ hades │ enabled │ 0.12.0 │ formerly zen-swarm │ user │" ;;
    mcp)       printf '%s\n' "zen-mcp-research   ✓ enabled" "zen-mcp-budget     ✓ enabled" "zen-mcp-audit      ✓ enabled" "zen-mcp-sshexec    ✓ enabled" ;;
    *) exit 0 ;;
esac
HERMES
    chmod +x "${TMPBIN}/hermes"
    run bash scripts/verify_hermes.sh
    [ "$status" -eq 0 ]
    [[ "$output" == *"OK: hermes v0.14.0"* ]]
}

@test "verify_hermes.sh FAIL when binary missing" {
    # Scrub hermes from PATH while keeping coreutils (bash/rm/grep) findable.
    # Deviation from plan-file Step 1 verbatim: `export PATH=""` cannot work
    # because bats `run` resolves the command via PATH and `bash` itself
    # disappears (exit 127 before the script executes). /usr/bin:/bin keeps
    # coreutils present and excludes /opt/homebrew/bin where hermes lives on
    # macOS dev workstations, exercising the gate's exit-1 path as intended.
    export PATH="/usr/bin:/bin"
    run bash scripts/verify_hermes.sh
    [ "$status" -ne 0 ]
    [[ "$output" == *"hermes binary not found"* ]]
}

@test "verify_hermes.sh FAIL when version mismatched" {
    cat > "${TMPBIN}/hermes" <<'HERMES'
#!/usr/bin/env bash
case "${1:-}" in
    --version) echo "hermes 0.99.99" ;;
    *) exit 0 ;;
esac
HERMES
    chmod +x "${TMPBIN}/hermes"
    run bash scripts/verify_hermes.sh
    [ "$status" -ne 0 ]
    [[ "$output" == *"version mismatch"* ]]
}

@test "verify_hermes.sh FAIL when plugin not discovered" {
    cat > "${TMPBIN}/hermes" <<'HERMES'
#!/usr/bin/env bash
case "${1:-}" in
    --version) echo "hermes 0.14.0" ;;
    plugins)   echo "other-plugin formerly zen-swarm" ;;
    *) exit 0 ;;
esac
HERMES
    chmod +x "${TMPBIN}/hermes"
    run bash scripts/verify_hermes.sh
    [ "$status" -ne 0 ]
    [[ "$output" == *"plugin hades not discovered"* ]]
}

@test "verify_hermes.sh FAIL when required MCP missing" {
    cat > "${TMPBIN}/hermes" <<'HERMES'
#!/usr/bin/env bash
case "${1:-}" in
    --version) echo "hermes 0.14.0" ;;
    plugins)   echo "hades" ;;
    mcp)       printf '%s\n' zen-mcp-research zen-mcp-budget zen-mcp-audit ;;
    *) exit 0 ;;
esac
HERMES
    chmod +x "${TMPBIN}/hermes"
    run bash scripts/verify_hermes.sh
    [ "$status" -ne 0 ]
    [[ "$output" == *"missing required mcp_servers: zen-mcp-sshexec"* ]]
}

@test "verify_hermes.sh FAIL when hades plugin is not enabled in config.yaml" {
    cat > "${HERMES_CONFIG_DIR}/config.yaml" <<'CFG'
mcp_servers:
  zen-mcp-research:
    command: zen-mcp-research
plugins:
  enabled: []
CFG
    run bash scripts/verify_hermes.sh
    [ "$status" -ne 0 ]
    [[ "$output" == *"HADES plugin enablement"* ]]
}
