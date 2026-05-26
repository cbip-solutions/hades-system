#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
set -euo pipefail

MODEL_URL="https://huggingface.co/jinaai/jina-embeddings-v2-base-code/resolve/main/onnx/model.onnx"
DEST_DEFAULT="${XDG_DATA_HOME:-$HOME/.local/share}/zen-swarm/models/jina-code"

usage() {
  cat <<'USAGE'
Usage: download-jina-model.sh [--dest <dir>] [--dry-run] [--verify-only]
                              [--pin-sha] [--force] [--help]

Downloads the Jina-code v2 ONNX embedder for Caronte semantic search.

Flags:
  --dest <dir>     Destination directory (default: $XDG_DATA_HOME/zen-swarm/models/jina-code,
                   falling back to ~/.local/share/zen-swarm/models/jina-code).
  --dry-run        Print the resolved URL and destination, then exit 0 without
                   touching the filesystem or network.
  --verify-only    Do NOT download; only verify model.onnx against expected-sha.
                   Exits 3 on mismatch, 5 if model absent.
  --pin-sha        First-run pinning: compute the SHA-256 of the downloaded (or
                   already-present) model and write it to <dest>/expected-sha.
                   Subsequent runs without this flag re-validate against the
                   pinned SHA.
  --force          Re-download even if model.onnx is already present + valid.
  --help, -h       Print this usage.

Examples:
  scripts/download-jina-model.sh --dry-run
  scripts/download-jina-model.sh --pin-sha       # first-run pinning
  scripts/download-jina-model.sh                 # re-validate + no-op if OK
  scripts/download-jina-model.sh --verify-only   # CI sanity probe
USAGE
}

DRY_RUN=0
VERIFY_ONLY=0
PIN_SHA=0
FORCE=0
DEST="$DEST_DEFAULT"

while [ $# -gt 0 ]; do
  case "$1" in
    --help|-h)      usage; exit 0 ;;
    --dest)         shift; DEST="$1"; shift ;;
    --dry-run)      DRY_RUN=1; shift ;;
    --verify-only)  VERIFY_ONLY=1; shift ;;
    --pin-sha)      PIN_SHA=1; shift ;;
    --force)        FORCE=1; shift ;;
    *)              echo "unknown flag: $1" >&2; usage >&2; exit 2 ;;
  esac
done

TARGET="$DEST/model.onnx"
SHA_FILE="$DEST/expected-sha"

if [ "$DRY_RUN" -eq 1 ]; then
  echo "dry-run: would download $MODEL_URL"
  echo "dry-run: destination = $TARGET"
  exit 0
fi

mkdir -p "$DEST"

sha256_of() {
  local f="$1"
  if command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$f" | awk '{print $1}'
  elif command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$f" | awk '{print $1}'
  else
    echo "ERROR: neither shasum nor sha256sum available" >&2
    return 127
  fi
}

if [ "$VERIFY_ONLY" -eq 1 ]; then
  if [ ! -f "$TARGET" ]; then
    echo "verify-only: model absent at $TARGET" >&2
    exit 5
  fi
  if [ ! -f "$SHA_FILE" ]; then
    echo "verify-only: expected-sha absent at $SHA_FILE (run with --pin-sha first)" >&2
    exit 6
  fi
  ACTUAL="$(sha256_of "$TARGET")"
  EXPECTED="$(cat "$SHA_FILE")"
  if [ "$ACTUAL" != "$EXPECTED" ]; then
    echo "ERROR: SHA mismatch at $TARGET" >&2
    echo "  actual:   $ACTUAL" >&2
    echo "  expected: $EXPECTED" >&2
    exit 3
  fi
  echo "verify-only: jina-code model SHA valid: $TARGET"
  exit 0
fi

if [ -f "$TARGET" ] && [ "$FORCE" -ne 1 ]; then
  ACTUAL="$(sha256_of "$TARGET")"
  if [ "$PIN_SHA" -eq 1 ]; then
    echo "$ACTUAL" > "$SHA_FILE"
    echo "pinned SHA-256: $ACTUAL"
    echo "jina-code model present + pinned: $TARGET"
    exit 0
  fi
  if [ -f "$SHA_FILE" ]; then
    EXPECTED="$(cat "$SHA_FILE")"
    if [ "$ACTUAL" = "$EXPECTED" ]; then
      echo "jina-code model already present + valid: $TARGET"
      exit 0
    fi
    echo "WARN: existing model has unexpected SHA (got $ACTUAL want $EXPECTED); removing" >&2
    rm -f "$TARGET"
    exit 3
  fi
  echo "ERROR: model present at $TARGET but $SHA_FILE missing" >&2
  echo "  Run with --pin-sha to pin the current file's SHA, or remove the file." >&2
  exit 6
fi

if ! command -v curl >/dev/null 2>&1; then
  echo "ERROR: curl required but not installed" >&2
  exit 4
fi

echo "downloading $MODEL_URL"
echo "  → $TARGET.partial"
if ! curl --fail --location --show-error --silent --retry 3 \
        --output "$TARGET.partial" "$MODEL_URL"; then
  rm -f "$TARGET.partial"
  echo "ERROR: download failed" >&2
  exit 4
fi

ACTUAL="$(sha256_of "$TARGET.partial")"
if [ "$PIN_SHA" -eq 1 ]; then
  mv "$TARGET.partial" "$TARGET"
  echo "$ACTUAL" > "$SHA_FILE"
  echo "pinned SHA-256: $ACTUAL"
  echo "jina-code model installed + pinned: $TARGET"
  exit 0
fi

if [ ! -f "$SHA_FILE" ]; then
  rm -f "$TARGET.partial"
  echo "ERROR: $SHA_FILE missing; run with --pin-sha to pin the current download" >&2
  exit 6
fi

EXPECTED="$(cat "$SHA_FILE")"
if [ "$ACTUAL" != "$EXPECTED" ]; then
  rm -f "$TARGET.partial"
  echo "ERROR: downloaded SHA mismatch" >&2
  echo "  actual:   $ACTUAL" >&2
  echo "  expected: $EXPECTED" >&2
  exit 3
fi

mv "$TARGET.partial" "$TARGET"
echo "jina-code model installed: $TARGET"
