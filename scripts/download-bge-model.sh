#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
set -euo pipefail

if ! command -v curl >/dev/null 2>&1; then
    echo "ERROR: curl is required. Install via your package manager." >&2
    exit 4
fi

sha_cmd=""
if command -v sha256sum >/dev/null 2>&1; then
    sha_cmd="sha256sum"
elif command -v shasum >/dev/null 2>&1; then
    sha_cmd="shasum -a 256"
else
    echo "ERROR: neither sha256sum (GNU coreutils) nor shasum (macOS) found." >&2
    echo "       Install one of:" >&2
    echo "         - macOS: comes with the OS; brew install coreutils for sha256sum" >&2
    echo "         - Linux: apt install coreutils  /  dnf install coreutils" >&2
    exit 4
fi

target_dir="${ZEN_BGE_MODEL_DIR:-$HOME/.local/share/zen-swarm/models}"
mkdir -p "$target_dir"

model_url="${ZEN_BGE_MODEL_URL:-https://huggingface.co/BAAI/bge-reranker-v2-m3/resolve/main/onnx/model.onnx}"
tok_url="${ZEN_BGE_TOKENIZER_URL:-https://huggingface.co/BAAI/bge-reranker-v2-m3/resolve/main/tokenizer.json}"

model_file="$target_dir/bge-reranker-v2-m3.onnx"
tok_file="$target_dir/tokenizer.json"
sha_file="$target_dir/bge-reranker-v2-m3.onnx.expected-sha"

dry_run="${ZEN_BGE_DRY_RUN:-0}"

echo "zen-swarm: BGE-reranker-v2-m3 model fetch"
echo "  target dir:    $target_dir"
echo "  model:         $model_url"
echo "  tokenizer:     $tok_url"
echo "  model file:    $model_file"
echo "  tokenizer:     $tok_file"
echo "  dry-run:       $dry_run"

sha256_of() {
    # shellcheck disable=SC2086 # $sha_cmd may contain "shasum -a 256"
    $sha_cmd "$1" | awk '{print $1}'
}

if [[ -f "$model_file" && -f "$tok_file" ]]; then
    expected="${ZEN_BGE_EXPECTED_SHA:-}"
    if [[ -z "$expected" && -f "$sha_file" ]]; then
        expected="$(cat "$sha_file")"
    fi
    if [[ -n "$expected" ]]; then
        actual="$(sha256_of "$model_file")"
        if [[ "$actual" == "$expected" ]]; then
            echo "OK: both files already present (sha verified); skipping fetch."
            exit 0
        else
            echo "ERROR: model file present but SHA mismatch." >&2
            echo "  expected: $expected" >&2
            echo "  actual:   $actual" >&2
            echo "  Refusing to overwrite. Inspect $model_file or delete to re-download." >&2
            exit 3
        fi
    fi
    actual="$(sha256_of "$model_file")"
    echo "$actual" > "$sha_file"
    echo "OK: both files already present; recorded SHA -> $sha_file"
    echo "    Commit-or-pin this value via ZEN_BGE_EXPECTED_SHA for reproducibility."
    exit 0
fi

if [[ "$dry_run" != "1" ]]; then
    if ! curl -fIsS --retry 2 --max-time 30 "$model_url" >/dev/null 2>&1; then
        echo "ERROR: pre-flight HEAD failed for $model_url" >&2
        echo "       HuggingFace may have moved the layout. Override via" >&2
        echo "       ZEN_BGE_MODEL_URL or update this script's default." >&2
        exit 2
    fi
    if ! curl -fIsS --retry 2 --max-time 30 "$tok_url" >/dev/null 2>&1; then
        echo "ERROR: pre-flight HEAD failed for $tok_url" >&2
        echo "       Override via ZEN_BGE_TOKENIZER_URL or update default." >&2
        exit 2
    fi
fi

if [[ "$dry_run" == "1" ]]; then
    echo "  [dry-run] skipping downloads; will record SHA from placeholder files"
else
    if [[ ! -f "$model_file" ]]; then
        echo "downloading model (~1 GB) ..."
        curl -fL --retry 5 --retry-delay 5 --progress-bar \
            -o "$model_file" "$model_url"
    fi
    if [[ ! -f "$tok_file" ]]; then
        echo "downloading tokenizer ..."
        curl -fL --retry 5 --retry-delay 5 --progress-bar \
            -o "$tok_file" "$tok_url"
    fi
fi

actual_sha=""
if [[ -f "$model_file" ]]; then
    actual_sha="$(sha256_of "$model_file")"
fi

expected="${ZEN_BGE_EXPECTED_SHA:-}"
if [[ -z "$expected" && -f "$sha_file" ]]; then
    expected="$(cat "$sha_file")"
fi

if [[ -n "$expected" ]]; then
    if [[ "$actual_sha" != "$expected" ]]; then
        echo "ERROR: SHA-256 mismatch on $model_file" >&2
        echo "  expected: $expected" >&2
        echo "  actual:   $actual_sha" >&2
        echo "  Removing downloaded file; re-run to retry." >&2
        rm -f "$model_file" "$tok_file"
        exit 3
    fi
    echo "OK: SHA verified -> $expected"
else
    echo "$actual_sha" > "$sha_file"
    echo "FIRST RUN: recorded sha=$actual_sha -> $sha_file"
    echo "           Commit-or-pin this value via ZEN_BGE_EXPECTED_SHA for reproducibility."
fi

echo "OK: BGE model + tokenizer ready at $target_dir"
