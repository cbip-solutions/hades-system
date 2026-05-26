# SPDX-License-Identifier: MIT
"""Tests for plugin/zen-swarm/hooks/llm_handlers.py — pre_llm_call hook."""

from __future__ import annotations

import json
import os
from typing import Any
from unittest.mock import patch

import httpx
import pytest

                                                                    
                                                          
from hermes_plugins.hades.hooks.llm_handlers import (
    _set_client_for_testing,
    pre_llm_call,
)


@pytest.fixture(autouse=True)
def isolate_client():
    """Reset the module-level daemon client between tests + force
    ZEN_HOOK_DRY_RUN so the Phase H' event-poster subprocess is
    short-circuited (rc=0, no real subprocess).
    """
    with patch.dict(os.environ, {"ZEN_HOOK_DRY_RUN": "1"}):
        yield
    _set_client_for_testing(None)


@pytest.fixture
def install_mock_client():
    """Install an httpx.MockTransport for testing the Unix-socket HTTP
    client the extended pre_llm_call body uses.
    """

    def _install(transport: httpx.MockTransport) -> httpx.Client:
        client = httpx.Client(transport=transport, base_url="http://localhost")
        _set_client_for_testing(client)
        return client

    return _install


                                                                        
                                                                  
                                                                        


def test_pre_llm_call_returns_none_when_no_messages():
    """No messages list → callback returns None (nothing to augment)."""
    result = pre_llm_call(model="m", session_id="sess-1")
    assert result is None


def test_pre_llm_call_returns_none_with_empty_messages():
    """Empty messages list → callback returns None."""
    result = pre_llm_call(messages=[], model="m")
    assert result is None


def test_pre_llm_call_returns_none_with_only_system_message():
    """No user message → callback returns None (nothing to augment)."""
    result = pre_llm_call(
        messages=[{"role": "system", "content": "you are helpful"}],
        model="m",
    )
    assert result is None


                                                                        
                                                                        


def test_pre_llm_call_returns_context_on_200(install_mock_client):
    captured: dict[str, object] = {}

    def handler(request: httpx.Request) -> httpx.Response:
        captured["body"] = json.loads(request.content)
        captured["url"] = str(request.url)
        return httpx.Response(
            200,
            json={
                "static_context": "## Repo context\nMergeEngine merges N candidates.",
                "volatile_context": "Recent commit 4eac544 touched MergeEngine.",
                "citations": [
                    {
                        "id": "c1",
                        "source_tool": "mcp_zen-swarm_caronte_query",
                        "confidence": 0.94,
                        "snippet": "func MergeEngine(...) {...}",
                    }
                ],
                "doctrine": "default",
                "max_kg_tokens": 10000,
            },
        )

    install_mock_client(httpx.MockTransport(handler))
    messages = [{"role": "user", "content": "refactor MergeEngine"}]
    result = pre_llm_call(
        messages=messages,
        model="claude-sonnet-4-6",
        session_id="sess-1",
        project_id="zen-swarm",
    )

    body = captured["body"]
    assert body["session_id"] == "sess-1"
    assert body["project"] == "zen-swarm"
    assert body["prompt_hash"]                        
    assert "refactor" in body["prompt"]
    assert "/v1/augment" in captured["url"]

                                                                      
                                     
    assert isinstance(result, dict)
    assert "context" in result
    assert "MergeEngine merges N candidates" in result["context"]
    assert "Recent commit 4eac544" in result["context"]


def test_pre_llm_call_returns_none_on_204(install_mock_client):
    """inv-zen-170: capa-firewall doctrine returns 204 → callback returns None."""

    def handler(request: httpx.Request) -> httpx.Response:
        return httpx.Response(204)

    install_mock_client(httpx.MockTransport(handler))
    messages = [{"role": "user", "content": "secret-thing"}]
    result = pre_llm_call(
        messages=messages, model="m", project_id="capa-firewall-project"
    )
    assert result is None


def test_pre_llm_call_returns_none_on_500(install_mock_client):
    """Daemon errors → callback fails open; Hermes proceeds unaugmented."""

    def handler(request: httpx.Request) -> httpx.Response:
        return httpx.Response(500, json={"error": "doctrine-read-fail"})

    install_mock_client(httpx.MockTransport(handler))
    result = pre_llm_call(messages=[{"role": "user", "content": "anything"}], model="m")
    assert result is None


def test_pre_llm_call_returns_none_on_network_error(install_mock_client):
    """Daemon unreachable → callback fails open."""

    def handler(request: httpx.Request) -> httpx.Response:
        raise httpx.ConnectError("daemon unreachable")

    install_mock_client(httpx.MockTransport(handler))
    result = pre_llm_call(messages=[{"role": "user", "content": "anything"}], model="m")
    assert result is None


def test_pre_llm_call_returns_none_on_empty_envelope(install_mock_client):
    """Phase B daemon returns empty envelope; callback must not emit empty context."""

    def handler(request: httpx.Request) -> httpx.Response:
        return httpx.Response(
            200,
            json={
                "static_context": "",
                "volatile_context": "",
                "citations": [],
                "doctrine": "default",
                "max_kg_tokens": 10000,
            },
        )

    install_mock_client(httpx.MockTransport(handler))
    result = pre_llm_call(messages=[{"role": "user", "content": "hi"}], model="m")
                                                                          
    assert result is None


def test_pre_llm_call_returns_context_volatile_only(install_mock_client):
    def handler(request: httpx.Request) -> httpx.Response:
        return httpx.Response(
            200,
            json={
                "static_context": "",
                "volatile_context": "Recent commit log: a1b2c3d added X.",
                "citations": [],
                "doctrine": "default",
                "max_kg_tokens": 10000,
            },
        )

    install_mock_client(httpx.MockTransport(handler))
    result = pre_llm_call(
        messages=[{"role": "user", "content": "what changed?"}], model="m"
    )
    assert isinstance(result, dict)
    assert "Recent commit log" in result["context"]


def test_pre_llm_call_returns_context_static_only(install_mock_client):
    def handler(request: httpx.Request) -> httpx.Response:
        return httpx.Response(
            200,
            json={
                "static_context": "## System context\nProject is zen-swarm.",
                "volatile_context": "",
                "citations": [],
                "doctrine": "default",
                "max_kg_tokens": 10000,
            },
        )

    install_mock_client(httpx.MockTransport(handler))
    result = pre_llm_call(
        messages=[{"role": "user", "content": "what's this?"}], model="m"
    )
    assert isinstance(result, dict)
    assert "Project is zen-swarm" in result["context"]


def test_pre_llm_call_prompt_hash_is_sha256_hex(install_mock_client):
    captured: dict[str, str] = {}

    def handler(request: httpx.Request) -> httpx.Response:
        body = json.loads(request.content)
        captured["prompt_hash"] = body["prompt_hash"]
        return httpx.Response(204)

    install_mock_client(httpx.MockTransport(handler))
    pre_llm_call(
        messages=[{"role": "user", "content": "deterministic prompt"}], model="m"
    )
    h = captured["prompt_hash"]
    assert len(h) == 64               
    int(h, 16)                     


def test_pre_llm_call_returns_none_on_malformed_json(install_mock_client):
    def handler(request: httpx.Request) -> httpx.Response:
        return httpx.Response(200, content=b"not-json{{")

    install_mock_client(httpx.MockTransport(handler))
    result = pre_llm_call(messages=[{"role": "user", "content": "hi"}], model="m")
    assert result is None


def test_pre_llm_call_handles_list_content_blocks(install_mock_client):
    """User messages with content as list (text + image blocks) — must
    extract just the text portions for the prompt + prompt_hash.
    """
    captured: dict[str, object] = {}

    def handler(request: httpx.Request) -> httpx.Response:
        captured["body"] = json.loads(request.content)
        return httpx.Response(204)

    install_mock_client(httpx.MockTransport(handler))
    pre_llm_call(
        messages=[
            {
                "role": "user",
                "content": [
                    {"type": "text", "text": "describe this"},
                    {"type": "image", "source": {"type": "base64", "data": "..."}},
                ],
            }
        ],
        model="m",
    )
    body = captured["body"]
    assert "describe this" in body["prompt"]
                                                
    assert "base64" not in body["prompt"]


def test_pre_llm_call_handles_string_content_blocks_in_list(install_mock_client):
    """Some clients use [{"type": "text", "text": "..."}] format; others
    pass plain strings inside the list. Both must work.
    """
    captured: dict[str, object] = {}

    def handler(request: httpx.Request) -> httpx.Response:
        captured["body"] = json.loads(request.content)
        return httpx.Response(204)

    install_mock_client(httpx.MockTransport(handler))
    pre_llm_call(
        messages=[{"role": "user", "content": ["one", "two"]}],
        model="m",
    )
    body = captured["body"]
    assert "one" in body["prompt"]
    assert "two" in body["prompt"]


def test_pre_llm_call_sends_conversation_id_to_daemon(install_mock_client):
    """Cross-language wire-format symmetry (Phase B reviewer I1): the
    Python plugin MUST emit ``conversation_id`` in the JSON envelope so
    the Go-side ``AugmentRequest`` decoder populates the field. Phase C
    consumes ConversationID for 5-lane RRF thread grouping; a silent
    drop here yields null retrieval scopes.
    """
    captured: dict[str, Any] = {}

    def handler(request: httpx.Request) -> httpx.Response:
        captured["body"] = json.loads(request.content)
        return httpx.Response(204)

    install_mock_client(httpx.MockTransport(handler))
    pre_llm_call(
        messages=[{"role": "user", "content": "follow-up"}],
        model="m",
        session_id="sess-X",
        conversation_id="conv-Y",
        project_id="zen-swarm",
    )
    body = captured["body"]
    assert "conversation_id" in body, (
        "envelope missing conversation_id key — Go-side AugmentRequest "
        "decoder would receive null (Phase C RRF retrieval would lose "
        "thread scoping)."
    )
    assert body["conversation_id"] == "conv-Y"


def test_pre_llm_call_empty_conversation_id_still_sent(install_mock_client):
    """When the operator's Hermes session is conversationless (first
    turn, or hook invoked outside a thread context), the Python plugin
    sends ``conversation_id: ""`` rather than omitting the key. Stable
    envelope shape lets the Go decoder distinguish "not provided" from
    "explicitly empty" through field presence even though both yield
    the zero value in Go.
    """
    captured: dict[str, Any] = {}

    def handler(request: httpx.Request) -> httpx.Response:
        captured["body"] = json.loads(request.content)
        return httpx.Response(204)

    install_mock_client(httpx.MockTransport(handler))
    pre_llm_call(
        messages=[{"role": "user", "content": "first turn"}],
        model="m",
        session_id="sess-only",
        conversation_id="",                  
    )
    body = captured["body"]
    assert "conversation_id" in body
    assert body["conversation_id"] == ""


def test_pre_llm_call_uses_last_user_message_for_prompt(install_mock_client):
    """When the conversation has multiple user messages, the LAST one is
    the prompt being augmented (most recent operator query).
    """
    captured: dict[str, object] = {}

    def handler(request: httpx.Request) -> httpx.Response:
        captured["body"] = json.loads(request.content)
        return httpx.Response(204)

    install_mock_client(httpx.MockTransport(handler))
    pre_llm_call(
        messages=[
            {"role": "user", "content": "first turn"},
            {"role": "assistant", "content": "first reply"},
            {"role": "user", "content": "follow-up question"},
        ],
        model="m",
    )
    body = captured["body"]
    assert "follow-up question" in body["prompt"]
    assert "first turn" not in body["prompt"]                             


def test_pre_llm_call_forwards_json_safe_kwargs_to_event_payload():
    """Phase H' baseline behaviour: extra **kwargs primitives flow into the
    audit event payload via invoke_event_poster. Verify the forward-path
    iterates over kwargs.
    """
    captured: dict[str, Any] = {}

    def _fake_poster(event_name, payload):
        captured["event"] = event_name
        captured["payload"] = payload
        return 0

    from hermes_plugins.hades.hooks import llm_handlers as _llm_mod

    original = _llm_mod.invoke_event_poster
    _llm_mod.invoke_event_poster = _fake_poster
    try:
        pre_llm_call(
            messages=None,
            model="m",
            task_id="task-42",
            retry_count=3,
            is_resume=True,
        )
    finally:
        _llm_mod.invoke_event_poster = original
    assert captured["event"] == "pre_llm_call"
    assert captured["payload"]["task_id"] == "task-42"
    assert captured["payload"]["retry_count"] == 3
    assert captured["payload"]["is_resume"] is True


def test_get_client_lazy_init_creates_unix_socket_client(monkeypatch):
    """Lazy init: when no test client is installed, _get_client constructs
    an httpx.Client bound to the Unix-socket path from $ZEN_DAEMON_SOCKET.
    The construction itself does NOT connect (UDS connect is deferred to
    first request) so we exercise the init without requiring a daemon.
    """
    from hermes_plugins.hades.hooks import llm_handlers as _llm_mod

                                  
    _set_client_for_testing(None)
    monkeypatch.setenv("ZEN_DAEMON_SOCKET", "/tmp/test-zen-daemon.sock")
    client = _llm_mod._get_client()
    assert isinstance(client, httpx.Client)
                                                                           
    assert _llm_mod._get_client() is client
                                                    
    _set_client_for_testing(None)


def test_pre_llm_call_extract_text_handles_unknown_content_type(install_mock_client):
    """If a user message's content is neither str nor list (e.g. None),
    _extract_text returns empty string; prompt_hash for empty string
    still computes deterministically.
    """
    captured: dict[str, object] = {}

    def handler(request: httpx.Request) -> httpx.Response:
        captured["body"] = json.loads(request.content)
        return httpx.Response(204)

    install_mock_client(httpx.MockTransport(handler))
    pre_llm_call(
        messages=[{"role": "user", "content": None}],                         
        model="m",
    )
    body = captured["body"]
                              
    empty_sha = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
    assert body["prompt_hash"] == empty_sha
    assert body["prompt"] == ""
