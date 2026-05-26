#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
set -euo pipefail

if ! command -v golangci-lint >/dev/null 2>&1; then
    echo "FAIL: golangci-lint not installed; verify-godoc-clean is a release gate"
    echo "      install pinned compatible tool:"
    echo "      go install github.com/golangci/golangci-lint/cmd/golangci-lint@v1.64.8"
    exit 127
fi

tmp_config="$(mktemp "${TMPDIR:-/tmp}/zen-godoc-revive.XXXXXX.yml")"
cleanup() {
    rm -f "$tmp_config"
}
trap cleanup EXIT

cat >"$tmp_config" <<'YAML'
linters:
  disable-all: true
  enable:
    - revive
linters-settings:
  revive:
    rules:
      - name: exported
        arguments:
          - disableStutteringCheck
issues:
  max-issues-per-linter: 0
  max-same-issues: 0
YAML

if ! golangci-lint run \
        --config "$tmp_config" \
        --exclude-use-default=false \
        ./internal/... \
        ./cmd/... ; then
    echo ""
    echo "FAIL: exported Go identifiers missing godoc-format comments"
    echo "      Per Plan 15 K-3/K-9: every exported identifier needs"
    echo "      // Name does ... (godoc-format doc comment starting with name)"
    exit 1
fi

echo "OK: all exported Go identifiers have godoc-format doc comments"
exit 0
