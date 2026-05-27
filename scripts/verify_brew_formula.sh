#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
set -euo pipefail

MODE="${1:-baseline}"
TAP_URL="${TAP_URL:-https://raw.githubusercontent.com/cbip-solutions/homebrew-tap/main/Formula/hades.rb}"
RELEASES_BASE="${RELEASES_BASE:-https://github.com/cbip-solutions/hades-system/releases/download}"

case "${MODE}" in
  baseline|"")
    MODE="baseline"
    ;;
  post-release|--post-release)
    MODE="post-release"
    ;;
  -h|--help|help)
    cat <<EOF
usage: $0 [baseline | post-release VERSION]

  baseline       Lint the local Formula mirror (default).
  post-release   Fetch the published Formula and cross-check against the
                 just-released tag's archive sha. Requires VERSION
                 (e.g., v1.0.0).

Env overrides:
  BREW_FORMULA_PATH  Local Formula path (default Formula/hades.rb).
  TAP_URL            Published Formula URL (default canonical hades-system tap).
  RELEASES_BASE      GH Releases download URL prefix.
EOF
    exit 0
    ;;
  *)
    echo "verify_brew_formula.sh: unknown mode: ${MODE}" >&2
    echo "usage: $0 [baseline | post-release VERSION]" >&2
    exit 2
    ;;
esac

if [[ "${MODE}" == "baseline" ]]; then
  FORMULA_PATH="${BREW_FORMULA_PATH:-Formula/hades.rb}"

  if [[ ! -f "$FORMULA_PATH" ]]; then
    echo "FAIL: formula not found at $FORMULA_PATH"
    echo "       set BREW_FORMULA_PATH or run from project root"
    exit 3
  fi

  echo "Linting $FORMULA_PATH ..."

  REQUIRED_KEYWORDS=(
      'depends_on "hermes-agent"'
      'license "MIT"'
      'Caronte'
      'Hermes Agent'
  )

  FAIL=0
  for kw in "${REQUIRED_KEYWORDS[@]}"; do
      if ! grep -qF "$kw" "$FORMULA_PATH"; then
          echo "FAIL: formula missing required content: '$kw'"
          FAIL=1
      fi
  done

  if command -v brew >/dev/null 2>&1; then
      if ! brew style "$FORMULA_PATH" 2>&1; then
          echo "FAIL: brew style reported errors"
          exit 2
      fi
  else
      echo "WARN: brew not installed; skipping brew style (CI macos-latest runner has brew preinstalled)"
  fi

  if [[ "$FAIL" -ne 0 ]]; then
      echo ""
      echo "FAIL: formula lint failed (decisiones 4 + 6 + 15 post-Stage-0 contract violated)"
      exit 1
  fi

  echo "OK: brew formula $FORMULA_PATH clean (hermes-agent dep + MIT license + Caronte + Hermes Agent mentions present)"
  exit 0
fi

VERSION="${2:-}"
if [[ -z "${VERSION}" ]]; then
  echo "verify_brew_formula.sh: post-release mode requires VERSION (e.g. v1.0.0)" >&2
  exit 2
fi

TMPDIR="$(mktemp -d)"
trap 'rm -rf "${TMPDIR}"' EXIT

echo "verify_brew_formula.sh: fetching ${TAP_URL}"
if ! curl -fsSL "${TAP_URL}" -o "${TMPDIR}/hades.rb"; then
  echo "FAIL: could not fetch published Formula from ${TAP_URL}" >&2
  exit 1
fi

FORMULA_VERSION="$(grep '^  version "' "${TMPDIR}/hades.rb" | sed -E 's/.*version "([^"]+)".*/\1/' || true)"
EXPECTED_VERSION="${VERSION#v}"
if [[ "${FORMULA_VERSION}" != "${EXPECTED_VERSION}" ]]; then
  echo "FAIL: version mismatch:"
  echo "  formula=${FORMULA_VERSION}"
  echo "  expected=${EXPECTED_VERSION}" >&2
  exit 1
fi

FORMULA_URL="$(grep '^  url "' "${TMPDIR}/hades.rb" | sed -E 's/.*url "([^"]+)".*/\1/' || true)"
EXPECTED_URL="${RELEASES_BASE}/${VERSION}/zen-swarm-${VERSION}-darwin-arm64.tar.gz"
if [[ "${FORMULA_URL}" != "${EXPECTED_URL}" ]]; then
  echo "FAIL: url mismatch:"
  echo "  formula=${FORMULA_URL}"
  echo "  expected=${EXPECTED_URL}" >&2
  exit 1
fi

FORMULA_SHA="$(grep '^  sha256 "' "${TMPDIR}/hades.rb" | sed -E 's/.*sha256 "([0-9a-f]+)".*/\1/' || true)"
echo "verify_brew_formula.sh: downloading ${FORMULA_URL} for sha256 cross-check"
if ! curl -fsSL "${FORMULA_URL}" -o "${TMPDIR}/archive.tar.gz"; then
  echo "FAIL: could not fetch released archive at ${FORMULA_URL}" >&2
  exit 1
fi
ACTUAL_SHA="$(shasum -a 256 "${TMPDIR}/archive.tar.gz" | awk '{print $1}')"
if [[ "${FORMULA_SHA}" != "${ACTUAL_SHA}" ]]; then
  echo "FAIL: sha256 mismatch:"
  echo "  formula=${FORMULA_SHA}"
  echo "  actual=${ACTUAL_SHA}" >&2
  exit 1
fi

# Assertion 4: caveats sentinel — the published caveats MUST still carry
REQUIRED_CAVEATS=(
    "hermes-agent"
    "zen doctor"
    "zen providers add"
    "MIT"
)
for kw in "${REQUIRED_CAVEATS[@]}"; do
    if ! grep -qF "$kw" "${TMPDIR}/hades.rb"; then
        echo "FAIL: published Formula missing caveats keyword: $kw" >&2
        exit 1
    fi
done

echo "verify_brew_formula.sh: post-release verification PASS for ${VERSION}"
exit 0
