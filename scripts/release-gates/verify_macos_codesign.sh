#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
set -euo pipefail

DIST_DIR="${1:-dist}"

if [[ "$(uname -s)" != "Darwin" ]]; then
  echo "verify_macos_codesign.sh: skipping (not running on Darwin; inv-zen-295 inert here)" >&2
  exit 0
fi

if ! command -v codesign &>/dev/null; then
  echo "verify_macos_codesign.sh: codesign not found in PATH (xcode-select --install)" >&2
  exit 1
fi

FAILURES=0
for bin in "${DIST_DIR}"/zen_darwin_arm64*/zen \
           "${DIST_DIR}"/zen-swarm-ctld_darwin_arm64*/zen-swarm-ctld; do
  if [[ ! -f "${bin}" ]]; then
    echo "verify_macos_codesign.sh: binary missing: ${bin}" >&2
    FAILURES=$((FAILURES+1))
    continue
  fi
  if ! codesign --verify "${bin}" 2>/dev/null; then
    echo "verify_macos_codesign.sh: codesign --verify FAILED: ${bin}" >&2
    FAILURES=$((FAILURES+1))
    continue
  fi
  if ! codesign -dvvv "${bin}" 2>&1 | grep -q "Signature=adhoc"; then
    echo "verify_macos_codesign.sh: expected adhoc signature, found other: ${bin}" >&2
    FAILURES=$((FAILURES+1))
    continue
  fi
  echo "verify_macos_codesign.sh: OK: ${bin}"
done

if [[ ${FAILURES} -gt 0 ]]; then
  echo "verify_macos_codesign.sh: ${FAILURES} verification failure(s); inv-zen-295 VIOLATED" >&2
  exit 1
fi

exit 0
