#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
set -euo pipefail

if [[ $# -lt 2 ]]; then
  echo "usage: $0 ARTIFACT OWNER [REPO]" >&2
  exit 2
fi

ARTIFACT="$1"
OWNER="$2"
REPO="${3:-hades-system}"

if [[ ! -f "${ARTIFACT}" ]]; then
  echo "verify_sigstore_attestation.sh: artifact missing: ${ARTIFACT}" >&2
  exit 1
fi

if ! command -v gh &>/dev/null; then
  echo "verify_sigstore_attestation.sh: gh CLI not found in PATH (https://cli.github.com/)" >&2
  exit 1
fi

if gh attestation verify "${ARTIFACT}" --repo "${OWNER}/${REPO}" 2>&1; then
  echo "verify_sigstore_attestation.sh: OK: ${ARTIFACT}"
  exit 0
else
  echo "verify_sigstore_attestation.sh: FAILED: ${ARTIFACT}; inv-zen-296 VIOLATED" >&2
  exit 1
fi
