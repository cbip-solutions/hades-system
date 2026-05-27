# SPDX-License-Identifier: MIT
# plugin/hades-system/hooks/llm_handlers.py
"""Hermes pre_llm_call callback for hades-system (release track' baseline + the release design"""

from __future__ import annotations

import contextlib
import hashlib
import json
import logging
import os
from typing import Any

import httpx

from ._common import invoke_event_poster

_log = logging.getLogger("hades-system.hooks.pre_llm_call")

# Module-level daemon client; lazy-initialised on first use; replaceable by
# tests via _set_client_for_testing.
_DAEMON_CLIENT: httpx.Client | None = None


def _get_client() -> httpx.Client:
    """Return the module-level daemon client (Unix socket), creating it on
    first use. Replaced by tests via _set_client_for_testing.
    """
    global _DAEMON_CLIENT
    if _DAEMON_CLIENT is None:
        socket_path = os.environ.get("HADES_DAEMON_SOCKET", "/tmp/hades-system.sock")
        unix_transport = httpx.HTTPTransport(uds=socket_path)
        _DAEMON_CLIENT = httpx.Client(
            transport=unix_transport,
            base_url="http://localhost",
            timeout=httpx.Timeout(connect=2.0, read=5.0, write=2.0, pool=2.0),
        )
    return _DAEMON_CLIENT


def _set_client_for_testing(client: httpx.Client | None) -> None:
    """Test helper — set or clear the module-level daemon client. Pass None
    to restore the default Unix-socket client on next call.
    """
    global _DAEMON_CLIENT
    if _DAEMON_CLIENT is not None and client is not _DAEMON_CLIENT:
        with contextlib.suppress(Exception):  # pragma: no cover - defensive
            _DAEMON_CLIENT.close()
    _DAEMON_CLIENT = client


def pre_llm_call(
    session_id: str = "",
    cwd: str = "",
    messages: list[dict[str, Any]] | None = None,
    model: str = "",
    project_id: str = "",
    conversation_id: str = "",
    **kwargs: Any,
) -> dict[str, Any] | None:
    """Hermes pre_llm_call hook callback — emits event + augments.

    Args (per Hermes pre_llm_call hook signature):
        session_id: current Hermes session ID
        cwd: current working directory
        messages: list of message dicts in the current turn
        model: requested LLM model name
        project_id: hades-system project alias (from .hades-system.toml or session
            context); empty falls back to "default"
        conversation_id: Hermes conversation correlation key
        **kwargs: forward-compatible extras (e.g. task_id, doctrine)

    Return contract per hermes_cli/plugins.py:1097-1107 (spike §5):
        - None: Hermes proceeds without augmentation context.
        - {"context": "<text>"}: Hermes prepends <text> as the user-message
          context prefix (system prompt is left untouched to preserve the
          Anthropic prompt-cache prefix across turns).
    """
    # ------------------------------------------------------------------
    # release track' baseline: emit event (audit-visible) FIRST so we record
    # the call even if the augmentation path 204s or fails.
    # ------------------------------------------------------------------
    payload: dict[str, Any] = {
        "session_id": session_id,
        "cwd": cwd,
        "messages_count": len(messages) if isinstance(messages, list) else 0,
        "hook_event_name": "pre_llm_call",
        "model": model,
        "project": project_id,
        "conversation_id": conversation_id,
    }
    for k, v in kwargs.items():
        if isinstance(v, (str, int, float, bool)) and v is not None:
            payload[k] = v
    _ = invoke_event_poster("pre_llm_call", payload)

    # ------------------------------------------------------------------
    # ------------------------------------------------------------------
    if not messages:
        return None
    last_user = _last_user_message(messages)
    if last_user is None:
        return None
    prompt_text = _extract_text(last_user.get("content"))
    prompt_hash = hashlib.sha256(prompt_text.encode("utf-8")).hexdigest()
    envelope = {
        "session_id": session_id or "",
        "conversation_id": conversation_id or "",
        "project": project_id or "",
        "prompt": prompt_text,
        "prompt_hash": prompt_hash,
        "mode": "interactive",
    }
    try:
        response = _get_client().post(
            "/v1/augment",
            content=json.dumps(envelope),
            headers={"Content-Type": "application/json"},
        )
    except httpx.HTTPError as exc:
        _log.debug("daemon /v1/augment unreachable; proceeding unaugmented: %s", exc)
        return None

    if response.status_code == 204:
        # inv-hades-170: doctrine veto. Operator-visible: Hermes chats normally.
        return None
    if response.status_code != 200:
        _log.debug(
            "daemon /v1/augment returned %d; proceeding unaugmented: %s",
            response.status_code,
            response.text[:200],
        )
        return None
    try:
        body = response.json()
    except json.JSONDecodeError as exc:
        _log.debug(
            "daemon /v1/augment returned malformed JSON; proceeding unaugmented: %s",
            exc,
        )
        return None

    static_context = (body.get("static_context") or "").strip()
    volatile_context = (body.get("volatile_context") or "").strip()
    if not static_context and not volatile_context:
        return None

    # Assemble into the single string Hermes' {"context": ...} contract
    # expects. Static portion (cache-eligible per Anthropic prompt cache)
    # goes first; volatile portion second (per-query specifics).
    parts: list[str] = []
    if static_context:
        parts.append(static_context)
    if volatile_context:
        parts.append(volatile_context)
    return {"context": "\n\n".join(parts)}


# ----------------------------------------------------------------------
# Helpers
# ----------------------------------------------------------------------


def _last_user_message(
    messages: list[dict[str, Any]],
) -> dict[str, Any] | None:
    """Return the last user message in the list, or None if absent."""
    for msg in reversed(messages):
        if msg.get("role") == "user":
            return msg
    return None


def _extract_text(content: Any) -> str:
    """Best-effort text extraction from Hermes message content.

    Hermes content is either a string or a list of content blocks (tool
    results, images, text). For augmentation we only need the text.
    """
    if isinstance(content, str):
        return content
    if isinstance(content, list):
        parts: list[str] = []
        for block in content:
            if isinstance(block, dict) and block.get("type") == "text":
                parts.append(str(block.get("text", "")))
            elif isinstance(block, str):
                parts.append(block)
        return "\n".join(parts)
    return ""
