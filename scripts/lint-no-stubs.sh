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

[[ "$QUIET" -eq 0 ]] && echo "[lint-no-stubs] running from $REPO_ROOT"

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

[[ "$QUIET" -eq 0 ]] && echo "[lint-no-stubs] ast-grep: lints/no-todo-implement-later.yaml"
AST_GREP_OUT=$(ast-grep scan --rule lints/no-todo-implement-later.yaml . 2>&1 || true)
if echo "$AST_GREP_OUT" | grep -qE "^(error|warning|info)\["; then
  COMBINED_OUTPUT+="[ast-grep:no-todo-implement-later]"$'\n'"$AST_GREP_OUT"$'\n'
  FINDINGS=$((FINDINGS+1))
fi

[[ "$QUIET" -eq 0 ]] && echo "[lint-no-stubs] zen-doctrine-lint nostub"
SCOPE_PATHS=(
  "./internal/doctrine/..."
  "./cmd/zen-doctrine-lint/..."
)
NOSTUB_OUT=$("$ZEN_LINT_BIN" \
  -nostub.released-plan=8 \
  -nostore=false \
  -conventional_commit=false \
  "${SCOPE_PATHS[@]}" 2>&1 || true)
if echo "$NOSTUB_OUT" | grep -qE "nostub-(panic|errnotimpl|todo|empty-method)"; then
  COMBINED_OUTPUT+="[zen-doctrine-lint:nostub]"$'\n'"$NOSTUB_OUT"$'\n'
  FINDINGS=$((FINDINGS+1))
fi

if [[ "$FINDINGS" -gt 0 ]]; then
  echo "[lint-no-stubs] FINDINGS:" >&2
  echo "$COMBINED_OUTPUT" >&2
  exit 1
fi

[[ "$QUIET" -eq 0 ]] && echo "[lint-no-stubs] PASS — zero stub findings"
exit 0
