#!/usr/bin/env python3
# SPDX-License-Identifier: MIT
"""
zen_jina_embed.py —   JinaCodeEmbeddings subprocess.

Hosts jinaai/jina-code-embeddings-1.5b via sentence-transformers on M4 MPS
(falls back to CPU on non-Apple platforms). Reads JSON-line requests from
stdin, writes JSON-line responses to stdout. Mirrors the  G
zen_embed.py protocol used by internal/knowledge/embed/mps.go.

Protocol:
    Request:  {"texts": ["one or many texts"], "shape": "both"|"bin"|"fp32"}
    Response: {"bins_b64": ["..."], "fp32s": [[...]], "error": ""}
    On error: {"bins_b64": [], "fp32s": [], "error": "message"}

Shim mode (ZEN_JINA_SHIM=1):
    Skip model loading entirely. Return deterministic outputs derived from
    sha256(text) so unit tests are reproducible without GPU or model weights.

    Shim fp32 (1536 floats):   smooth pseudo-distribution derived from
                               sha256(text) bytes; values in [-1, 1].
    Shim binary (32 bytes):    quantize_binary_256(fp32[:256]) — cross-shape
                               invariant matches the real-model path so Go
                               unit tests can assert it.

The cross-shape invariant matters: EmbedBoth must produce a binary that
matches the binary derived from quantizing the first 256 dims of fp32.
Shim mode enforces this so embedder_test.go can assert the contract.

invariant: this script never imports network libraries. Reads stdin /
writes stdout only. Operator's daemon owns network egress F.
"""

import sys
import os
import json
import hashlib
import math
import base64
import traceback


SHIM_MODE = os.environ.get("ZEN_JINA_SHIM", "") == "1"
                                                                       
# unit tests on the Go side. Test-only — production callers MUST leave unset.
        
                                                                                 
                                                                                  
                                                                               
                                                                                  
                                                                              
                                                                                        
                                                                         
MALFORM_MODE = os.environ.get("ZEN_JINA_MALFORM", "")
MODEL_NAME = "jinaai/jina-code-embeddings-1.5b"
DIM_FULL = 1536
DIM_BIN = 256


def quantize_binary_256(fp32_first256):
    """Quantize 256 floats to 32 bytes via sign-bit.

    Bit i (MSB-first within each byte) = 1 if fp32[i] >= 0 else 0.
    Returns bytes of length 32.

    Wire format MUST match Go side's quantizeBinary256 and sqlite-vec's
    BIT[256] virtual-table convention.
    """
    if len(fp32_first256) != DIM_BIN:
        raise ValueError(
            f"quantize_binary_256 requires 256 floats, got {len(fp32_first256)}"
        )
    out = bytearray(DIM_BIN // 8)
    for i, v in enumerate(fp32_first256):
        if v >= 0:
            out[i >> 3] |= 1 << (7 - (i & 7))
    return bytes(out)


def shim_fp32(text):
    """Deterministic 1536-d fp32 vector derived from sha256(text)."""
    h = hashlib.sha256(text.encode("utf-8")).digest()            
    out = []
    for i in range(DIM_FULL):
                                                                     
        byte_a = h[i % 32]
        byte_b = h[(i + 7) % 32]
        seed = (byte_a * 256 + byte_b) / 65535.0             
                                                                           
        out.append(math.sin(seed * math.pi * 2 + i * 0.001))
    return out


def shim_embed(texts, shape):
    """Return deterministic outputs without loading the model."""
    bins_b64 = []
    fp32s = []
    for text in texts:
        fp32 = shim_fp32(text)
        if shape in ("fp32", "both"):
            fp32s.append(fp32)
        if shape in ("bin", "both"):
            bin_bytes = quantize_binary_256(fp32[:DIM_BIN])
            bins_b64.append(base64.b64encode(bin_bytes).decode("ascii"))
    return {"bins_b64": bins_b64, "fp32s": fp32s, "error": ""}


def load_model():
    """Load jina-code-embeddings-1.5b with MPS device when available."""
    from sentence_transformers import SentenceTransformer  # type: ignore[import]
    import torch  # type: ignore[import]
    if torch.backends.mps.is_available():
        device = "mps"
    elif torch.cuda.is_available():
        device = "cuda"
    else:
        device = "cpu"
    model = SentenceTransformer(MODEL_NAME, device=device)
    return model, device


def real_embed(model, texts, shape):
    """Encode texts via the loaded model. Slices to 256-d before binary quant."""
    embeddings = model.encode(
        texts, convert_to_numpy=True, normalize_embeddings=False
    )
    bins_b64 = []
    fp32s = []
    for vec in embeddings:
        fp32_full = [float(x) for x in vec[:DIM_FULL]]
        if shape in ("fp32", "both"):
            fp32s.append(fp32_full)
        if shape in ("bin", "both"):
            bin_bytes = quantize_binary_256(fp32_full[:DIM_BIN])
            bins_b64.append(base64.b64encode(bin_bytes).decode("ascii"))
    return {"bins_b64": bins_b64, "fp32s": fp32s, "error": ""}


def main():
                                                                          
    if SHIM_MODE or MALFORM_MODE:
        model = None
        device = "shim" if SHIM_MODE else "malform"
    else:
        try:
            model, device = load_model()
        except Exception as e:  # noqa: BLE001 -- propagate as JSON error
            err = {
                "bins_b64": [],
                "fp32s": [],
                "error": f"model load failed: {e}",
            }
            sys.stdout.write(json.dumps(err) + "\n")
            sys.stdout.flush()
            sys.exit(1)

                                                                          
    sys.stderr.write(
        f"zen_jina_embed.py ready (device={device}, shim={SHIM_MODE})\n"
    )
    sys.stderr.flush()

    for line in sys.stdin:
        line = line.strip()
        if not line:
            continue
                                                                          
        if MALFORM_MODE:
            if MALFORM_MODE == "malformed_json":
                sys.stdout.write("{not valid json\n")
                sys.stdout.flush()
                continue
            if MALFORM_MODE == "wrong_bin_count":
                resp = {"bins_b64": [], "fp32s": [], "error": ""}
            elif MALFORM_MODE == "wrong_fp_count":
                resp = {"bins_b64": [], "fp32s": [], "error": ""}
            elif MALFORM_MODE == "wrong_bin_len":
                                               
                resp = {
                    "bins_b64": [base64.b64encode(b"\x00" * 31).decode("ascii")],
                    "fp32s": [[0.0] * DIM_FULL],
                    "error": "",
                }
            elif MALFORM_MODE == "wrong_fp_len":
                resp = {
                    "bins_b64": [base64.b64encode(b"\x00" * 32).decode("ascii")],
                    "fp32s": [[0.0] * (DIM_FULL - 1)],
                    "error": "",
                }
            elif MALFORM_MODE == "bad_b64":
                resp = {
                    "bins_b64": ["!!!not-valid-base64!!!"],
                    "fp32s": [[0.0] * DIM_FULL],
                    "error": "",
                }
            elif MALFORM_MODE == "subprocess_err":
                resp = {
                    "bins_b64": [],
                    "fp32s": [],
                    "error": "synthetic subprocess error",
                }
            else:
                resp = {
                    "bins_b64": [],
                    "fp32s": [],
                    "error": f"unknown MALFORM_MODE: {MALFORM_MODE!r}",
                }
            sys.stdout.write(json.dumps(resp) + "\n")
            sys.stdout.flush()
            continue
        try:
            req = json.loads(line)
            texts = req.get("texts", [])
            shape = req.get("shape", "both")
            if shape not in ("bin", "fp32", "both"):
                raise ValueError(f"invalid shape: {shape!r}")
            if SHIM_MODE:
                resp = shim_embed(texts, shape)
            else:
                resp = real_embed(model, texts, shape)
        except Exception as e:  # noqa: BLE001 -- per-call resilience
            resp = {
                "bins_b64": [],
                "fp32s": [],
                "error": f"{e}\n{traceback.format_exc()}",
            }
        sys.stdout.write(json.dumps(resp) + "\n")
        sys.stdout.flush()


if __name__ == "__main__":
    main()
