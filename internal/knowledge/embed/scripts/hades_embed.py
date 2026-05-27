#!/usr/bin/env python3
# SPDX-License-Identifier: MIT
"""hades-system embedding subprocess (Mac MPS path).

Used by the Go MPSEmbedder via JSON stdin/stdout protocol. Reads one JSON
object per line; emits one JSON response per line. Subprocess persists
across calls (avoids model reload overhead).

Protocol:
  Input:  {"text": "..."}
  Output: {"embedding": [float, ...], "dimensions": 384}
  Error:  {"error": "message"}

Requires: Python 3.10+; sentence-transformers; torch with MPS support.
Install: pip install sentence-transformers torch

invariant: this script does NOT make web calls at runtime. The
sentence-transformers model is downloaded on first import (cached at
~/.cache/torch/sentence_transformers/) -- first-call cold start downloads
weights, subsequent calls are local-only. Operator pre-warms via
`hades knowledge rebuild --project <id>` post-install.
"""
import json
import sys


def main() -> None:
    # Lazy import to keep startup error messages clean
    try:
        from sentence_transformers import SentenceTransformer  # type: ignore[import]
    except ImportError as e:
        json.dump({"error": f"sentence-transformers not installed: {e}"}, sys.stdout)
        sys.stdout.write("\n")
        sys.stdout.flush()
        return

    try:
        # Use MPS device on Apple Silicon; CPU fallback inside torch
        model = SentenceTransformer("sentence-transformers/all-mpnet-base-v2", device="mps")
    except Exception as e:
        json.dump({"error": f"model load failed: {e}"}, sys.stdout)
        sys.stdout.write("\n")
        sys.stdout.flush()
        return

    for line in sys.stdin:
        line = line.strip()
        if not line:
            continue
        try:
            req = json.loads(line)
            text = req.get("text", "")
            emb = model.encode(text, normalize_embeddings=True).tolist()
            json.dump({"embedding": emb, "dimensions": len(emb)}, sys.stdout)
            sys.stdout.write("\n")
            sys.stdout.flush()
        except Exception as e:  # noqa: BLE001 -- per-call resilience
            json.dump({"error": str(e)}, sys.stdout)
            sys.stdout.write("\n")
            sys.stdout.flush()


if __name__ == "__main__":
    main()
