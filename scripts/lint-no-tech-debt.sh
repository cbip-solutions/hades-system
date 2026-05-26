#!/usr/bin/env bash
# SPDX-License-Identifier: MIT

set -euo pipefail

QUIET=0
for arg in "$@"; do
  case "$arg" in
    --quiet) QUIET=1 ;;
    --help) echo "Usage: $0 [--quiet]"; exit 0 ;;
    *) echo "ERROR: unknown flag $arg" >&2; exit 2 ;;
  esac
done

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"

[[ "$QUIET" -eq 0 ]] && echo "[lint-no-tech-debt] running from $REPO_ROOT"

if ! command -v ast-grep >/dev/null 2>&1; then
  echo "ERROR: ast-grep not installed; install via: brew install ast-grep" >&2
  exit 2
fi
ZEN_LINT_BIN="${ZEN_DOCTRINE_LINT_BIN:-$(go env GOPATH)/bin/zen-doctrine-lint}"
if [[ ! -x "$ZEN_LINT_BIN" ]]; then
  echo "ERROR: zen-doctrine-lint not installed; build via: go install ./cmd/zen-doctrine-lint" >&2
  exit 2
fi

FINDINGS=0
COMBINED_OUTPUT=""

for rule_file in lints/*.yaml; do
  rule_name=$(basename "$rule_file" .yaml)
  [[ "$QUIET" -eq 0 ]] && echo "[lint-no-tech-debt] ast-grep: $rule_file"
  RULE_OUT=$(ast-grep scan --rule "$rule_file" . 2>&1 || true)
  if echo "$RULE_OUT" | grep -qE "^(error|warning|info)\["; then
    COMBINED_OUTPUT+="[ast-grep:$rule_name]"$'\n'"$RULE_OUT"$'\n'
    FINDINGS=$((FINDINGS+1))
  fi
done

[[ "$QUIET" -eq 0 ]] && echo "[lint-no-tech-debt] zen-doctrine-lint: nostub + nostore + conventional_commit"
SCOPE_PATHS=(
  "./internal/doctrine/..."
  "./cmd/zen-doctrine-lint/..."
)
LINT_OUT=$("$ZEN_LINT_BIN" \
  -conventional_commit.depth=20 \
  "${SCOPE_PATHS[@]}" 2>&1 || true)
if echo "$LINT_OUT" | grep -qE "(nostub-|nostore-|cc-)(panic|errnotimpl|todo|empty-method|forbidden|bad-type|missing-scope|bad-scope|bad-subject|trailing-dot|claude-attribution)"; then
  COMBINED_OUTPUT+="[zen-doctrine-lint:all]"$'\n'"$LINT_OUT"$'\n'
  FINDINGS=$((FINDINGS+1))
fi

if [[ "$FINDINGS" -gt 0 ]]; then
  echo "[lint-no-tech-debt] FINDINGS:" >&2
  echo "$COMBINED_OUTPUT" >&2
  exit 1
fi

NUM_RULES=$(ls lints/*.yaml 2>/dev/null | wc -l | tr -d ' ')
[[ "$QUIET" -eq 0 ]] && echo "[lint-no-tech-debt] PASS — zero findings across $NUM_RULES ast-grep rules + 3 analyzers"
exit 0
