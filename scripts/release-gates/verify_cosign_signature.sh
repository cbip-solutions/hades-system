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
  echo "verify_cosign_signature.sh: artifact missing: ${ARTIFACT}" >&2
  exit 1
fi

SIG="${ARTIFACT}.sig"
CERT="${ARTIFACT}.pem"

if [[ ! -f "${SIG}" ]]; then
  echo "verify_cosign_signature.sh: signature missing: ${SIG}" >&2
  exit 1
fi

if [[ ! -f "${CERT}" ]]; then
  echo "verify_cosign_signature.sh: certificate missing: ${CERT}" >&2
  exit 1
fi

if ! command -v cosign &>/dev/null; then
  echo "verify_cosign_signature.sh: cosign not found in PATH (https://docs.sigstore.dev/cosign/installation/)" >&2
  exit 1
fi

if cosign verify-blob \
    --certificate-identity-regexp "https://github.com/${OWNER}/${REPO}/.github/workflows/release.yml@.*" \
    --certificate-oidc-issuer https://token.actions.githubusercontent.com \
    --signature "${SIG}" \
    --certificate "${CERT}" \
    "${ARTIFACT}" 2>&1; then
  echo "verify_cosign_signature.sh: OK: ${ARTIFACT}"
  exit 0
else
  echo "verify_cosign_signature.sh: FAILED: ${ARTIFACT}; inv-zen-296 VIOLATED" >&2
  exit 1
fi
