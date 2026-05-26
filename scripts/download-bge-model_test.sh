#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
set -euo pipefail

[[ "${BGE_DOWNLOAD_SMOKE:-0}" == "1" ]] || { echo "SKIP: set BGE_DOWNLOAD_SMOKE=1"; exit 0; }

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
target_script="$script_dir/download-bge-model.sh"

[[ -x "$target_script" ]] || {
    echo "FAIL: $target_script not executable"
    exit 1
}

tmpdir="$(mktemp -d)"
trap 'rm -rf "$tmpdir"' EXIT

ZEN_BGE_MODEL_DIR="$tmpdir/models" \
ZEN_BGE_DRY_RUN=1 \
    "$target_script" >"$tmpdir/run1.log" 2>&1

[[ -d "$tmpdir/models" ]] || {
    echo "FAIL: directory $tmpdir/models not created on first dry-run"
    cat "$tmpdir/run1.log"
    exit 1
}

grep -q "bge-reranker-v2-m3" "$tmpdir/run1.log" || {
    echo "FAIL: first dry-run did not echo the model URL or filename"
    cat "$tmpdir/run1.log"
    exit 1
}

touch "$tmpdir/models/bge-reranker-v2-m3.onnx" "$tmpdir/models/tokenizer.json"
expected_sha="$(printf '' | shasum -a 256 2>/dev/null | awk '{print $1}' || \
    printf '' | sha256sum | awk '{print $1}')"
echo "$expected_sha" > "$tmpdir/models/bge-reranker-v2-m3.onnx.expected-sha"

ZEN_BGE_MODEL_DIR="$tmpdir/models" \
ZEN_BGE_DRY_RUN=1 \
    "$target_script" >"$tmpdir/run2.log" 2>&1

grep -q "already present" "$tmpdir/run2.log" || {
    echo "FAIL: second invocation did not report 'already present' (not idempotent)"
    cat "$tmpdir/run2.log"
    exit 1
}

rm -rf "$tmpdir/models"
mkdir -p "$tmpdir/models"
touch "$tmpdir/models/bge-reranker-v2-m3.onnx" "$tmpdir/models/tokenizer.json"
ZEN_BGE_MODEL_DIR="$tmpdir/models" \
ZEN_BGE_DRY_RUN=1 \
    "$target_script" >"$tmpdir/run3.log" 2>&1

[[ -f "$tmpdir/models/bge-reranker-v2-m3.onnx.expected-sha" ]] || {
    echo "FAIL: first run did not record .expected-sha companion file"
    cat "$tmpdir/run3.log"
    exit 1
}

echo "OK"
