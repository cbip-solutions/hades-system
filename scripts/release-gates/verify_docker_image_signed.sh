#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
set -euo pipefail

if [[ $# -lt 1 ]]; then
  echo "usage: $0 IMAGE_REFERENCE" >&2
  echo "  e.g. $0 ghcr.io/hades-system/hades-system:v1.0.0" >&2
  exit 2
fi

IMAGE_REF="$1"

if ! command -v cosign >/dev/null 2>&1; then
  echo "verify_docker_image_signed.sh: cosign CLI not in PATH" >&2
  echo "  install: brew install cosign  OR  https://docs.sigstore.dev/cosign/installation/" >&2
  exit 1
fi

CERT_IDENTITY_REGEXP="${COSIGN_CERT_IDENTITY_REGEXP:-https://github.com/hades-system/hades-system/\\.github/workflows/release\\.yml@.*}"
OIDC_ISSUER="${COSIGN_OIDC_ISSUER:-https://token.actions.githubusercontent.com}"

DIGEST_REF=""
if DIGEST_REF=$(cosign triangulate "${IMAGE_REF}" 2>/dev/null); then
  : # triangulate succeeded; use as-is
fi
if [[ -z "${DIGEST_REF}" ]]; then
  if command -v docker >/dev/null 2>&1 && command -v jq >/dev/null 2>&1; then
    RAW=$(docker manifest inspect --verbose "${IMAGE_REF}" 2>/dev/null || true)
    if [[ -n "${RAW}" ]]; then
      DIGEST=$(echo "${RAW}" | jq -r '.[0].Descriptor.digest // .Descriptor.digest // empty' 2>/dev/null || true)
      if [[ -n "${DIGEST}" ]]; then
        DIGEST_REF="${IMAGE_REF%:*}@${DIGEST}"
      fi
    fi
  fi
fi
if [[ -z "${DIGEST_REF}" ]]; then
  DIGEST_REF="${IMAGE_REF}"
fi

echo "verify_docker_image_signed.sh: verifying ${DIGEST_REF}"
echo "  certificate-identity-regexp: ${CERT_IDENTITY_REGEXP}"
echo "  oidc-issuer: ${OIDC_ISSUER}"

if ! cosign verify \
      --certificate-identity-regexp "${CERT_IDENTITY_REGEXP}" \
      --certificate-oidc-issuer "${OIDC_ISSUER}" \
      "${DIGEST_REF}" >/dev/null 2>&1; then
  echo "verify_docker_image_signed.sh: cosign signature INVALID for ${DIGEST_REF}; inv-zen-298 VIOLATED" >&2
  echo "  re-run with: cosign verify --certificate-identity-regexp '${CERT_IDENTITY_REGEXP}' --certificate-oidc-issuer '${OIDC_ISSUER}' '${DIGEST_REF}'" >&2
  exit 1
fi
echo "verify_docker_image_signed.sh: cosign signature OK: ${DIGEST_REF}"

if command -v gh >/dev/null 2>&1; then
  if gh attestation verify "oci://${DIGEST_REF}" --owner hades-system --repo hades-system >/dev/null 2>&1; then
    echo "verify_docker_image_signed.sh: SLSA L2 attestation OK: ${DIGEST_REF}"
  else
    echo "verify_docker_image_signed.sh: SLSA L2 attestation INVALID for ${DIGEST_REF}; inv-zen-298 VIOLATED" >&2
    exit 1
  fi
else
  echo "verify_docker_image_signed.sh: gh CLI not in PATH; skipping SLSA L2 attestation check"
fi

echo "verify_docker_image_signed.sh: PASS (inv-zen-298)"
exit 0
