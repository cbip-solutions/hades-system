# SPDX-License-Identifier: MIT
"""Tests for ZenSwarmTransport — the Python half of the cross-language
LLM-dispatch bridge.

inv-zen-164: every Hermes LLM dispatch routes through this class. The
tests assert the HTTP envelope shape, the X-Zen-Transport header, and
the absence of authorisation headers (defence in depth).
"""

from __future__ import annotations

import json

import httpx
import pytest
from hermes_plugins.hades.transports.zen_swarm_transport import (
    HEADER_TRANSPORT_SOURCE,
    TRANSPORT_LABEL,
    CompletedResponse,
    ZenSwarmTransport,
)


@pytest.fixture
def fake_daemon():
    """Return a factory that builds an httpx.MockTransport with a custom handler."""

    def factory(handler):
        return httpx.MockTransport(handler)

    return factory


@pytest.fixture
def canned_response_handler():
    """Default handler that returns a successful ForwardedResponse envelope."""

    def handler(request):
        try:
            body = json.loads(request.content)
        except Exception as exc:  # pragma: no cover — defensive
            return httpx.Response(400, json={"error": f"bad body: {exc}"})
        if "body" not in body:
            return httpx.Response(400, json={"error": "missing body field"})
        return httpx.Response(
            200,
            json={
                "status": 200,
                "body": json.dumps(
                    {
                        "id": "msg_TEST",
                        "model": body.get("model", "claude-sonnet-4-6"),
                        "content": [{"type": "text", "text": "hi from fake daemon"}],
                        "usage": {"input_tokens": 10, "output_tokens": 5},
                    }
                ),
                "headers": {},
                "audit_event_id": "evt-fake-001",
            },
        )

    return handler


def _build_transport(http_transport: httpx.MockTransport) -> ZenSwarmTransport:
    """Construct a ZenSwarmTransport with a custom httpx transport (test seam)."""
    zt = ZenSwarmTransport()
    zt._set_client_for_test(
        httpx.Client(transport=http_transport, base_url="http://localhost")
    )
    return zt


def test_complete_forwards_messages_to_daemon(fake_daemon, canned_response_handler):
    transport = _build_transport(fake_daemon(canned_response_handler))
    try:
        result = transport.complete(
            messages=[{"role": "user", "content": "hi"}],
            model="claude-sonnet-4-6",
            max_tokens=512,
        )
    finally:
        transport.close()
    assert isinstance(result, CompletedResponse)
    assert result.body["id"] == "msg_TEST"
    assert result.body["content"][0]["text"] == "hi from fake daemon"
    assert result.body["usage"]["input_tokens"] == 10


def test_complete_returns_completedresponse_with_audit_event_id(
    fake_daemon, canned_response_handler
):
    """Reviewer I2: ``complete()`` MUST surface the daemon's
    ``audit_event_id`` so Plan 12 citation renderers can deep-link via
    ``zen://audit/<id>``. The previous return shape (just the inner
    JSON body) discarded this metadata silently.
    """
    transport = _build_transport(fake_daemon(canned_response_handler))
    try:
        result = transport.complete(
            messages=[{"role": "user", "content": "hi"}],
            model="claude-sonnet-4-6",
        )
    finally:
        transport.close()
    assert isinstance(result, CompletedResponse)
    assert result.audit_event_id == "evt-fake-001"
    assert result.status == 200


def test_complete_returns_completedresponse_with_headers(fake_daemon):
    """Reviewer I2: ``complete()`` MUST surface the daemon's response
    headers (e.g. ``anthropic-ratelimit-*``, ``request-id``) so callers
    can read rate-limit + provenance metadata. The previous return shape
    discarded them.
    """

    def handler(request: httpx.Request) -> httpx.Response:
        return httpx.Response(
            200,
            json={
                "status": 200,
                "body": json.dumps({"id": "msg_HEADERS"}),
                "headers": {
                    "anthropic-ratelimit-requests-remaining": "42",
                    "request-id": "req-7",
                },
                "audit_event_id": "evt-h",
            },
        )

    transport = _build_transport(fake_daemon(handler))
    try:
        result = transport.complete(
            messages=[{"role": "user", "content": "hi"}], model="m"
        )
    finally:
        transport.close()
    assert isinstance(result, CompletedResponse)
    assert result.headers.get("anthropic-ratelimit-requests-remaining") == "42"
    assert result.headers.get("request-id") == "req-7"
    assert result.audit_event_id == "evt-h"


def test_complete_completedresponse_handles_missing_metadata(fake_daemon):
    """When the daemon omits ``audit_event_id`` (graceful degradation
    per messages_handler.go:236-256), the field defaults to empty
    string rather than blowing up the dataclass.
    """

    def handler(request: httpx.Request) -> httpx.Response:
        return httpx.Response(
            200,
            json={
                "status": 200,
                "body": json.dumps({"id": "msg_NOAUDIT"}),
                "headers": {},
                                         
            },
        )

    transport = _build_transport(fake_daemon(handler))
    try:
        result = transport.complete(
            messages=[{"role": "user", "content": "hi"}], model="m"
        )
    finally:
        transport.close()
    assert isinstance(result, CompletedResponse)
    assert result.audit_event_id == ""
    assert result.headers == {}
    assert result.body["id"] == "msg_NOAUDIT"


def test_complete_completedresponse_defends_against_bad_status_type(fake_daemon):
    """Defence in depth: if the daemon returns a non-int ``status``
    field (corrupted envelope or upstream regression), the transport
    coerces it to 200 rather than blowing up downstream callers that
    expect an int. Same contract applies for status missing entirely.
    """

    def handler(request: httpx.Request) -> httpx.Response:
        return httpx.Response(
            200,
            json={
                                                                        
                "status": "two-hundred",
                "body": json.dumps({"id": "msg_BAD"}),
                "headers": {},
            },
        )

    transport = _build_transport(fake_daemon(handler))
    try:
        result = transport.complete(
            messages=[{"role": "user", "content": "hi"}], model="m"
        )
    finally:
        transport.close()
    assert result.status == 200                    
    assert result.body["id"] == "msg_BAD"


def test_complete_completedresponse_defends_against_bad_headers_type(fake_daemon):
    """Defence in depth: if the daemon returns ``headers`` as a non-dict
    (e.g. a list because of a deserialisation regression), the transport
    coerces to empty dict rather than crashing dict-iterating callers.
    """

    def handler(request: httpx.Request) -> httpx.Response:
        return httpx.Response(
            200,
            json={
                "status": 200,
                "body": json.dumps({"id": "msg_HDR"}),
                                                
                "headers": ["not", "a", "dict"],
                "audit_event_id": "evt-x",
            },
        )

    transport = _build_transport(fake_daemon(handler))
    try:
        result = transport.complete(
            messages=[{"role": "user", "content": "hi"}], model="m"
        )
    finally:
        transport.close()
    assert result.headers == {}
    assert result.audit_event_id == "evt-x"


def test_complete_completedresponse_defends_against_bad_audit_event_id_type(fake_daemon):
    """Defence in depth: if the daemon returns ``audit_event_id`` as a
    non-string (e.g. an int because of a JSON encoder regression), the
    transport coerces to empty string rather than letting the wrong
    type leak into Plan 12 citation renderers that build zen://audit/<id>
    URIs.
    """

    def handler(request: httpx.Request) -> httpx.Response:
        return httpx.Response(
            200,
            json={
                "status": 200,
                "body": json.dumps({"id": "msg_AID"}),
                "headers": {},
                "audit_event_id": 42,                
            },
        )

    transport = _build_transport(fake_daemon(handler))
    try:
        result = transport.complete(
            messages=[{"role": "user", "content": "hi"}], model="m"
        )
    finally:
        transport.close()
    assert result.audit_event_id == ""


def test_complete_completedresponse_is_immutable_dataclass():
    """CompletedResponse fields must be exposed as dataclass attributes
    (status, body, headers, audit_event_id). Asserting the dataclass
    contract guards against accidental refactor to a bare tuple/dict
    that would lose the typed shape.
    """
    import dataclasses

    assert dataclasses.is_dataclass(CompletedResponse)
    field_names = {f.name for f in dataclasses.fields(CompletedResponse)}
    assert field_names == {"status", "body", "headers", "audit_event_id"}


def test_complete_stamps_x_zen_transport_header(fake_daemon):
    captured: dict[str, str] = {}

    def handler(request: httpx.Request) -> httpx.Response:
        captured.update({k: v for k, v in request.headers.items()})
        return httpx.Response(
            200,
            json={
                "status": 200,
                "body": json.dumps({"content": [{"type": "text", "text": "ok"}]}),
                "headers": {},
            },
        )

    transport = _build_transport(fake_daemon(handler))
    try:
        transport.complete(messages=[{"role": "user", "content": "hi"}], model="m")
    finally:
        transport.close()
    assert captured.get("x-zen-transport") == TRANSPORT_LABEL
    assert HEADER_TRANSPORT_SOURCE == "X-Zen-Transport"


def test_complete_strips_authorization_header(fake_daemon):
    captured: dict[str, str] = {}

    def handler(request: httpx.Request) -> httpx.Response:
        captured.update({k.lower(): v for k, v in request.headers.items()})
        return httpx.Response(
            200,
            json={
                "status": 200,
                "body": json.dumps({"content": [{"type": "text", "text": "ok"}]}),
                "headers": {},
            },
        )

    transport = _build_transport(fake_daemon(handler))
    try:
        transport.complete(
            messages=[{"role": "user", "content": "hi"}],
            model="m",
            extra_headers={
                "Authorization": "Bearer NEVER-FORWARD",
                "X-API-KEY": "leak",
                "Cookie": "session=leak",
            },
        )
    finally:
        transport.close()
    assert "authorization" not in captured
    assert "x-api-key" not in captured
    assert "cookie" not in captured


def test_complete_propagates_session_id_into_body(fake_daemon):
    captured: dict[str, object] = {}

    def handler(request: httpx.Request) -> httpx.Response:
        captured["body"] = json.loads(request.content)
        return httpx.Response(
            200,
            json={
                "status": 200,
                "body": json.dumps({"content": [{"type": "text", "text": "ok"}]}),
                "headers": {},
            },
        )

    transport = _build_transport(fake_daemon(handler))
    try:
        transport.complete(
            messages=[{"role": "user", "content": "hi"}],
            model="m",
            session_id="sess-42",
            conversation_id="conv-1",
            idempotency_key="idemp-xyz",
            profile="orchestrator",
            project="zen-swarm",
        )
    finally:
        transport.close()
    body = captured["body"]
    assert body["session_id"] == "sess-42"
    assert body["conversation_id"] == "conv-1"
    assert body["idempotency_key"] == "idemp-xyz"
    assert body["profile"] == "orchestrator"
    assert body["project"] == "zen-swarm"
    assert body["transport_source"] == TRANSPORT_LABEL
                                                                            
    inner = json.loads(body["body"])
    assert inner["model"] == "m"
    assert inner["messages"] == [{"role": "user", "content": "hi"}]


def test_complete_propagates_max_tokens_into_inner_body(fake_daemon):
    captured: dict[str, object] = {}

    def handler(request: httpx.Request) -> httpx.Response:
        captured["body"] = json.loads(request.content)
        return httpx.Response(
            200,
            json={"status": 200, "body": json.dumps({"content": []}), "headers": {}},
        )

    transport = _build_transport(fake_daemon(handler))
    try:
        transport.complete(
            messages=[{"role": "user", "content": "x"}], model="m", max_tokens=4096
        )
    finally:
        transport.close()
    inner = json.loads(captured["body"]["body"])
    assert inner["max_tokens"] == 4096


def test_complete_kwargs_pass_through_into_inner_body(fake_daemon):
    captured: dict[str, object] = {}

    def handler(request: httpx.Request) -> httpx.Response:
        captured["body"] = json.loads(request.content)
        return httpx.Response(
            200,
            json={"status": 200, "body": json.dumps({"content": []}), "headers": {}},
        )

    transport = _build_transport(fake_daemon(handler))
    try:
        transport.complete(
            messages=[{"role": "user", "content": "x"}],
            model="m",
            temperature=0.7,
            system="You are helpful.",
        )
    finally:
        transport.close()
    inner = json.loads(captured["body"]["body"])
    assert inner["temperature"] == 0.7
    assert inner["system"] == "You are helpful."


def test_complete_raises_on_502(fake_daemon):
    def handler(request: httpx.Request) -> httpx.Response:
        return httpx.Response(502, json={"error": "upstream-down"})

    transport = _build_transport(fake_daemon(handler))
    try:
        with pytest.raises(RuntimeError) as excinfo:
            transport.complete(messages=[{"role": "user", "content": "hi"}], model="m")
    finally:
        transport.close()
    assert "502" in str(excinfo.value) or "upstream" in str(excinfo.value).lower()


def test_complete_raises_on_malformed_envelope(fake_daemon):
    def handler(request: httpx.Request) -> httpx.Response:
        return httpx.Response(200, content=b"not-json{{")

    transport = _build_transport(fake_daemon(handler))
    try:
        with pytest.raises(RuntimeError):
            transport.complete(messages=[{"role": "user", "content": "hi"}], model="m")
    finally:
        transport.close()


def test_complete_raises_on_malformed_inner_body(fake_daemon):
    def handler(request: httpx.Request) -> httpx.Response:
                                                                      
        return httpx.Response(
            200,
            json={"status": 200, "body": "not-json{{", "headers": {}},
        )

    transport = _build_transport(fake_daemon(handler))
    try:
        with pytest.raises(RuntimeError):
            transport.complete(messages=[{"role": "user", "content": "hi"}], model="m")
    finally:
        transport.close()


def test_stream_returns_single_chunk_generator(fake_daemon, canned_response_handler):
    transport = _build_transport(fake_daemon(canned_response_handler))
    try:
        chunks = list(
            transport.stream(messages=[{"role": "user", "content": "hi"}], model="m")
        )
    finally:
        transport.close()
    assert len(chunks) == 1
                                                                      
                                            
    assert isinstance(chunks[0], CompletedResponse)
    assert chunks[0].body["content"][0]["text"] == "hi from fake daemon"


def test_close_releases_client():
    transport = ZenSwarmTransport()
    transport.close()
                                                                         
    # MUST be idempotent (no exception).
    transport.close()


def test_default_socket_path_used_without_env(monkeypatch):
    monkeypatch.delenv("ZEN_DAEMON_SOCKET", raising=False)
    transport = ZenSwarmTransport()
    try:
        assert transport._socket_path == "/tmp/zen-swarm.sock"
    finally:
        transport.close()


def test_custom_socket_path_via_env(monkeypatch):
    monkeypatch.setenv("ZEN_DAEMON_SOCKET", "/var/run/zen.sock")
    transport = ZenSwarmTransport()
    try:
        assert transport._socket_path == "/var/run/zen.sock"
    finally:
        transport.close()


def test_explicit_socket_path_argument_wins(monkeypatch):
    monkeypatch.setenv("ZEN_DAEMON_SOCKET", "/should/be/ignored")
    transport = ZenSwarmTransport(socket_path="/explicit/path.sock")
    try:
        assert transport._socket_path == "/explicit/path.sock"
    finally:
        transport.close()


def test_transport_label_matches_handler_constant():
                                                                            
    # MUST match the Go-side discriminator the handler reads.
    assert TRANSPORT_LABEL == "zenswarm"


def test_complete_handles_bytes_body_in_envelope(fake_daemon):
    """The Go-side ForwardedResponse.Body is []byte; if a custom dispatcher
    forwards bytes (rather than a string-form JSON), the Python side must
    decode rather than blow up.
    """

    def handler(request: httpx.Request) -> httpx.Response:
                                                                      
                                                                       
                                                                         
        return httpx.Response(
            200,
            text=json.dumps(
                {
                    "status": 200,
                    "body": json.dumps({"id": "msg_BYTES"}),
                    "headers": {},
                }
            ),
        )

    transport = _build_transport(fake_daemon(handler))
                                                                           
    import hermes_plugins.hades.transports.zen_swarm_transport as zt_mod

    original_json = httpx.Response.json

    def json_with_bytes_body(self):
        resp = original_json(self)
        resp["body"] = resp["body"].encode("utf-8")
        return resp

    try:
        httpx.Response.json = json_with_bytes_body
        result = transport.complete(
            messages=[{"role": "user", "content": "hi"}], model="m"
        )
    finally:
        httpx.Response.json = original_json
        transport.close()
    assert isinstance(result, CompletedResponse)
    assert result.body["id"] == "msg_BYTES"
                                                               
    assert zt_mod is not None


def test_extra_headers_collision_with_zen_headers_preserves_zen_value(fake_daemon):
    """When extra_headers contain a key that would collide with X-Zen-Transport,
    the canonical zen-stamped value wins (defence in depth: a misbehaving
    caller cannot impersonate a different transport source).
    """
    captured: dict[str, str] = {}

    def handler(request: httpx.Request) -> httpx.Response:
        captured.update({k.lower(): v for k, v in request.headers.items()})
        return httpx.Response(
            200,
            json={"status": 200, "body": json.dumps({"content": []}), "headers": {}},
        )

    transport = _build_transport(fake_daemon(handler))
    try:
        transport.complete(
            messages=[{"role": "user", "content": "hi"}],
            model="m",
            extra_headers={
                "X-Zen-Transport": "evil-impersonation",
                "X-Anthropic-Beta": "tools-2025-05",
            },
        )
    finally:
        transport.close()
                           
    assert captured["x-zen-transport"] == TRANSPORT_LABEL
                                                 
    assert captured.get("x-anthropic-beta") == "tools-2025-05"


def test_extra_headers_none_is_safe(fake_daemon, canned_response_handler):
    """extra_headers=None must not blow up the sanitiser."""
    transport = _build_transport(fake_daemon(canned_response_handler))
    try:
        result = transport.complete(
            messages=[{"role": "user", "content": "hi"}], model="m", extra_headers=None
        )
    finally:
        transport.close()
    assert isinstance(result, CompletedResponse)
    assert "id" in result.body


def test_provider_transport_alias_dropped():
    """The documentary ``ProviderTransport`` alias was MISLEADING after the
    2026-05-15 deep audit confirmed Hermes DOES expose a real
    ``ProviderTransport`` ABC at ``agent/transports/base.py:16`` (format
    adapter — NOT HTTP-transport substitution).

    Keeping the alias risked code review confusing ``ZenSwarmTransport``
    for a subclass of the real ABC. The alias is dropped; the class is
    reachable as ``ZenSwarmTransport`` directly. inv-zen-164 compliance
    greps target ``class ZenSwarmTransport`` (the canonical name); no
    production code depends on the alias.
    """
    from hermes_plugins.hades.transports import zen_swarm_transport as mod

    assert not hasattr(mod, "ProviderTransport"), (
        "ProviderTransport alias must be dropped from zen_swarm_transport.py "
        "(see Phase B extension audit amendment 2026-05-15)"
    )


def test_envelope_kwargs_leaking_into_kwargs_are_dropped_from_inner_body(fake_daemon):
    """Defensive: if a caller dynamically passes an envelope kwarg via **,
    it MUST NOT leak into the inner Anthropic body. Production callers
    use the explicit kwargs (session_id=...); this guards against
    refactor errors.
    """
    captured: dict[str, object] = {}

    def handler(request: httpx.Request) -> httpx.Response:
        captured["body"] = json.loads(request.content)
        return httpx.Response(
            200,
            json={"status": 200, "body": json.dumps({"content": []}), "headers": {}},
        )

    transport = _build_transport(fake_daemon(handler))
    try:
                                                                        
                                                                      
                                                                           
                                                                                    
                                                                         
                                                                         
                
        leaky_kwargs = {"profile": "leaky-profile-from-kwargs"}
                                                                          
                                                          
        env = transport._build_envelope(
            messages=[{"role": "user", "content": "x"}],
            model="m",
            session_id=None,
            conversation_id=None,
            idempotency_key=None,
            profile=None,
            project=None,
            max_tokens=None,
            kwargs=leaky_kwargs,
        )
        inner = json.loads(env["body"])
                                                                  
                                                                
        assert "profile" not in inner
    finally:
        transport.close()
