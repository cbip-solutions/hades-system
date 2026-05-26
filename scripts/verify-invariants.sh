#!/usr/bin/env bash
# SPDX-License-Identifier: MIT

set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
TARGET="$ROOT/private-tier1-module"
EXIT=0

if [ ! -d "$TARGET" ]; then
  echo "verify-invariants: $TARGET does not exist; skipping (Phase B runs before Phase C)"
  exit 0
fi

RAW_FIELD_RE='^[[:space:]]+([A-Z][A-Za-z0-9]*)?(Token|Credential|OAuth|ApiKey|APIKey|Bearer|Authorization|Secret|Password|Key|RefreshToken|AccessToken|ClientSecret|ClientID)[A-Za-z0-9]*[[:space:]]+\*?string([[:space:]`]|$)'
SAFE_MARKER='// safe: not-a-credential'
matches="$(grep -RnE "$RAW_FIELD_RE" "$TARGET" --include='*.go' --exclude='*_test.go' 2>/dev/null || true)"
if [ -n "$matches" ]; then
  filtered="$(echo "$matches" | grep -vF "$SAFE_MARKER" || true)"
  if [ -n "$filtered" ]; then
    echo "$filtered"
    echo ""
    echo "ERROR (inv-zen-051): credential-named fields must be redact.Secret, not raw string / *string."
    echo "Migrate the fields above. Use internal/redact.Secret instead of string."
    echo "(Or, if the field is a non-credential despite the keyword match, append"
    echo " '$SAFE_MARKER — <reason>' to the field's line.)"
    EXIT=1
  fi
fi

CLIENT_NO_TRANSPORT_RE='http\.Client\{[^}]*\}'
set +e
matches="$(grep -REn "$CLIENT_NO_TRANSPORT_RE" "$TARGET" --include='*.go' --exclude='*_test.go' 2>/dev/null)"
rc=$?
set -e
if [ "$rc" -gt 1 ]; then
  echo "verify-invariants: grep failed with exit $rc while scanning $TARGET" >&2
  exit "$rc"
fi
if [ -n "$matches" ]; then
  set +e
  filtered="$(echo "$matches" | grep -v 'Transport')"
  rc=$?
  set -e
  if [ "$rc" -gt 1 ]; then
    echo "verify-invariants: grep filter failed with exit $rc" >&2
    exit "$rc"
  fi
  if [ -n "$filtered" ]; then
    echo ""
    echo "ERROR (inv-zen-052): http.Client constructed without explicit Transport field."
    echo "Wrap with redact.NewRedactingTransport(inner, secret) and assign to Transport:"
    echo "$filtered"
    EXIT=1
  fi
fi

go build ./internal/redact/ >/dev/null

if [ $EXIT -eq 0 ]; then
  echo "verify-invariants: OK (inv-zen-051, inv-zen-052)"
fi
exit $EXIT
