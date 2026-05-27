# SPDX-License-Identifier: MIT
# Imported by `zen migrate claude-code`.
# Markdown body lives in `recall.md` (raw, no escape).
# This wrapper is a fixed delegate — operator-supplied content NEVER
# enters Python source, eliminating the docstring-escape / RCE class.

from pathlib import Path

_SIDECAR = Path(__file__).parent / "recall.md"

def recall_handler(raw_args: str) -> str | None:
    # The slash command body is the markdown text in the sidecar.
    # Operator extends this handler to rewrite as native Python.
    _ = _SIDECAR.read_text(encoding="utf-8")  # available to operator extension
    return None  # operator extends as needed
