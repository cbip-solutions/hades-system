#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
#                                              MUST NEVER ship in production)

set -euo pipefail

TOXIPROXY_BIN="${TOXIPROXY_BIN:-/opt/homebrew/bin/toxiproxy-server}"
CONTROL_PORT="${TOXIPROXY_CONTROL_PORT:-8474}"
LOG_DIR="${TOXIPROXY_LOG_DIR:-${HOME}/Library/Logs/zen-swarm}"

cat <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>com.hades-system.zen-toxiproxy-dev</string>
    <key>ProgramArguments</key>
    <array>
        <string>${TOXIPROXY_BIN}</string>
        <string>-host</string>
        <string>127.0.0.1</string>
        <string>-port</string>
        <string>${CONTROL_PORT}</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
    <key>StandardOutPath</key>
    <string>${LOG_DIR}/toxiproxy.out.log</string>
    <key>StandardErrorPath</key>
    <string>${LOG_DIR}/toxiproxy.err.log</string>
    <key>EnvironmentVariables</key>
    <dict>
        <key>ZEN_TOXIPROXY_MODE</key>
        <string>dev</string>
    </dict>
</dict>
</plist>
PLIST
