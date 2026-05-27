# SPDX-License-Identifier: MIT
# plugin/zen-swarm/hooks/_common.py
"""Shared helpers for zen-swarm Hermes hook callbacks."""

from __future__ import annotations

import json
import logging
import os
import subprocess
from pathlib import Path
from typing import Any

logger = logging.getLogger(__name__)

# Path to the Go event-poster binary (built by `make plugin`). Resolved
# relative to the plugin root (parent of this hooks/ directory).
_PLUGIN_ROOT = Path(__file__).resolve().parent.parent
_POSTER_BIN = _PLUGIN_ROOT / "bin" / "zen-event-poster"

# Subprocess timeout for the Go poster invocation (seconds). At 1s, hooks
# never noticeably slow down Hermes tool calls; daemon-side processing is
# async (event submitted to batcher and returned immediately).
_POSTER_TIMEOUT = 1.0


def invoke_event_poster(event_name: str, payload: dict[str, Any]) -> int:
    """Invoke bin/zen-event-poster as a subprocess.

    Args:
        event_name: argv[1] for the binary (e.g. "on_session_start",
            "pre_tool_call.blocked"). Must be a known event per the binary's
            dispatch table.
        payload: dict serialized to JSON on stdin (Hermes hook kwargs +
            zen-derived fields).

    Returns:
        Exit code from the binary (0 = ok, 1 = warn, 2 = block — though
        callbacks DO NOT signal block via exit code; they return the block
        directive dict to Hermes). Returns 0 unconditionally if
        ZEN_HOOK_DRY_RUN=1 is set in env (used by unit tests).

    Never raises. Subprocess errors, timeouts, and missing binaries return 1.
    """
    if os.environ.get("ZEN_HOOK_DRY_RUN"):
        return 0

    if not _POSTER_BIN.exists():
        # Defensive: never crash the user's tool call on missing poster.
        # Log via logger (Hermes captures it via the plugin error path).
        logger.warning(
            "zen-swarm hook: bin/zen-event-poster not built; run 'make plugin' "
            "in zen-swarm root. expected at %s",
            _POSTER_BIN,
        )
        return 1

    try:
        body = json.dumps(payload).encode("utf-8")
    except (TypeError, ValueError) as exc:
        logger.warning("zen-swarm hook: payload JSON encode failed: %s", exc)
        return 1

    try:
        result = subprocess.run(
            [str(_POSTER_BIN), event_name],
            input=body,
            timeout=_POSTER_TIMEOUT,
            check=False,
            capture_output=True,
        )
    except subprocess.TimeoutExpired:
        logger.warning(
            "zen-swarm hook: zen-event-poster timed out after %.1fs",
            _POSTER_TIMEOUT,
        )
        return 1
    except OSError as exc:
        logger.warning("zen-swarm hook: zen-event-poster invocation failed: %s", exc)
        return 1

    if result.stderr:
        # Surface poster's stderr (e.g., daemon unreachable) via logger.
        # Hermes' logging pipeline will route to its operator-visible channel.
        try:
            err_text = result.stderr.decode("utf-8", errors="replace").strip()
        except Exception:
            err_text = repr(result.stderr)
        if err_text:
            logger.warning("zen-swarm hook poster stderr: %s", err_text)

    return result.returncode


def safe_str(value: Any, max_len: int = 200) -> str:
    """Truncate-safe stringify for payload-summary fields."""
    s = str(value) if value is not None else ""
    if len(s) > max_len:
        return s[:max_len] + "..."
    return s


def summarize_args(args: Any, max_chars: int = 200) -> dict[str, Any]:
    """Compress a tool's args dict to a payload-safe summary.

    Strings are truncated to max_chars. Nested dicts/lists become marker
    strings ('[object]' / '[array]') to avoid bloating event payloads with
    full tool inputs.
    """
    if not isinstance(args, dict):
        return {}
    out: dict[str, Any] = {}
    for k, v in args.items():
        if isinstance(v, str):
            out[k] = safe_str(v, max_chars)
        elif isinstance(v, dict):
            out[k] = "[object]"
        elif isinstance(v, list):
            out[k] = "[array]"
        else:
            out[k] = v
    return out
