#!/usr/bin/env bash
# SPDX-License-Identifier: MIT

set -euo pipefail

SCAN_DIR="${1:-internal/workforce/worker/}"

if [[ ! -d "$SCAN_DIR" ]]; then
  echo "scan-no-worktreepool: directory $SCAN_DIR does not exist (skipping)"
  exit 0
fi

violations=$(
  find "$SCAN_DIR" -name '*.go' -print0 \
    | xargs -0 grep -nHE '\b(WorktreePool|AllocateWorktree)\b' 2>/dev/null \
    | awk -F: '
      {
        line = $0
        idx = index(line, ":")
        idx = index(substr(line, idx+1), ":")
        content = substr(line, index(line, $3))
        sub(/\/\/.*$/, "", content)
        gsub(/"[^"]*"/, "\"\"", content)
        if (match(content, /\b(WorktreePool|AllocateWorktree)\b/)) {
          print $1 ":" $2 ":" content
        }
      }
    ' \
    || true
)

if [[ -n "$violations" ]]; then
  echo "inv-zen-087 violation: WorktreePool/AllocateWorktree identifier reference forbidden in $SCAN_DIR" >&2
  echo "$violations" >&2
  exit 1
fi
echo "inv-zen-087: no WorktreePool/AllocateWorktree identifier references in $SCAN_DIR — Plan 4 boundary OK"
