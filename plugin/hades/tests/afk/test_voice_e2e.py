# SPDX-License-Identifier: MIT
"""End-to-end integration test for voice query sync vs async paths."""

from __future__ import annotations

import json
from typing import Any

import httpx
import pytest
from hermes_plugins.hades.afk import (
    AUDIT_VOICE_QUERY_DISPATCHED,
    VoiceFlowMode,
)
from hermes_plugins.hades.afk.audit import (
    AUDIT_EMIT_PATH,
    emit_voice_query_dispatched,
    post_inbox_notification,
)
from hermes_plugins.hades.afk.voice_flow import dispatch_voice_query


@pytest.mark.asyncio
async def test_voice_e2e_sync_path() -> None:
    """Short query → sync inline → audit emit, no inbox notification."""
    captured_audit: list[dict[str, Any]] = []
    captured_inbox: list[dict[str, Any]] = []

    def _handler(request: httpx.Request) -> httpx.Response:
        if request.url.path == AUDIT_EMIT_PATH and request.method == "POST":
            body = json.loads(request.content)
                                                                             
                                                                             
                                                              
            if body["type"] == "afk.voice_query_async_started":
                captured_inbox.append(body)
            else:
                captured_audit.append(body)
            return httpx.Response(202, json={"id": f"e-{len(captured_audit)}"})
        return httpx.Response(404)

    daemon_url = "http://localhost:4471"

    async with httpx.AsyncClient(transport=httpx.MockTransport(_handler)) as client:
                                                                     
                                                                    
                                          
        _audit_emitter = emit_voice_query_dispatched
        _inbox_poster = post_inbox_notification

        flow = await dispatch_voice_query(
            query="blast radius for handler.go",                             
            operator_id="testuser",
            project_id="zen-swarm-sha256",
            explicit_override=None,
            cross_project=False,
            project_count=1,
            daemon_url=daemon_url,
            client=client,
            audit_emitter=_audit_emitter,
            inbox_poster=_inbox_poster,
        )

    assert flow.mode == VoiceFlowMode.SYNC
    assert flow.estimated_latency_ms == 4500
    assert flow.notification_dispatched is False
    assert flow.explicit_override is False

                                         
    assert len(captured_audit) == 1
    assert captured_audit[0]["type"] == AUDIT_VOICE_QUERY_DISPATCHED
    assert captured_audit[0]["payload"]["mode"] == "sync"
    assert captured_audit[0]["payload"]["notification_dispatched"] is False
    assert captured_audit[0]["project_id"] == "zen-swarm-sha256"

                                         
    assert len(captured_inbox) == 0


@pytest.mark.asyncio
async def test_voice_e2e_async_path_with_inbox_notification() -> None:
    """Long query → async dispatch → audit emit +  inbox notification."""
    captured_audit: list[dict[str, Any]] = []
    captured_inbox: list[dict[str, Any]] = []

    def _handler(request: httpx.Request) -> httpx.Response:
        if request.url.path == AUDIT_EMIT_PATH and request.method == "POST":
            body = json.loads(request.content)
            if body["type"] == "afk.voice_query_async_started":
                captured_inbox.append(body)
            else:
                captured_audit.append(body)
            return httpx.Response(202, json={"id": "ok"})
        return httpx.Response(404)

    daemon_url = "http://localhost:4471"

    async with httpx.AsyncClient(transport=httpx.MockTransport(_handler)) as client:
                                                                     
                                                                    
                                          
        _audit_emitter = emit_voice_query_dispatched
        _inbox_poster = post_inbox_notification

                                                                 
                                               
        flow = await dispatch_voice_query(
            query="blast radius community summary across A B C",
            operator_id="testuser",
            project_id="zen-swarm-sha256",
            explicit_override=None,
            cross_project=True,
            project_count=3,
            daemon_url=daemon_url,
            client=client,
            audit_emitter=_audit_emitter,
            inbox_poster=_inbox_poster,
        )

    assert flow.mode == VoiceFlowMode.ASYNC
    assert flow.estimated_latency_ms >= 10_000
    assert flow.notification_dispatched is True

                                                                         
    assert len(captured_audit) == 1
    assert captured_audit[0]["payload"]["mode"] == "async"
    assert captured_audit[0]["payload"]["notification_dispatched"] is True

                                                      
    assert len(captured_inbox) == 1
    inbox_body = captured_inbox[0]
    assert inbox_body["project_id"] == "zen-swarm-sha256"
                                                                    
                                                                     
                                                   
    assert inbox_body["payload"]["severity"] == "info-immediate"
    assert inbox_body["type"] == "afk.voice_query_async_started"
    assert inbox_body["payload"]["voice_query"].startswith("blast radius")
    assert inbox_body["payload"]["expected_completion_ms"] >= 10_000
    assert inbox_body["payload"]["operator_id"] == "testuser"


@pytest.mark.asyncio
async def test_voice_e2e_explicit_sync_override_skips_inbox() -> None:
    """Operator forces ``--sync`` on a long query → SYNC, no inbox notification."""
    captured_audit: list[dict[str, Any]] = []
    captured_inbox: list[dict[str, Any]] = []

    def _handler(request: httpx.Request) -> httpx.Response:
        if request.url.path == AUDIT_EMIT_PATH and request.method == "POST":
            body = json.loads(request.content)
            if body["type"] == "afk.voice_query_async_started":
                captured_inbox.append(body)
            else:
                captured_audit.append(body)
            return httpx.Response(202, json={"id": "ok"})
        return httpx.Response(404)

    daemon_url = "http://localhost:4471"

    async with httpx.AsyncClient(transport=httpx.MockTransport(_handler)) as client:
                                                                     
                                                                    
                                          
        _audit_emitter = emit_voice_query_dispatched
        _inbox_poster = post_inbox_notification

        flow = await dispatch_voice_query(
            query="cross-project federated impact analysis with community summary "
            + "x" * 600,
            operator_id="testuser",
            project_id="zen-swarm-sha256",
            explicit_override=VoiceFlowMode.SYNC,                        
            cross_project=True,
            project_count=5,
            daemon_url=daemon_url,
            client=client,
            audit_emitter=_audit_emitter,
            inbox_poster=_inbox_poster,
        )

    assert flow.mode == VoiceFlowMode.SYNC
    assert flow.explicit_override is True
    assert flow.notification_dispatched is False

                                     
    assert captured_audit[0]["payload"]["mode"] == "sync"
    assert captured_audit[0]["payload"]["explicit_override"] is True

                                                                 
    assert len(captured_inbox) == 0


@pytest.mark.asyncio
async def test_voice_e2e_audit_emit_5xx_raises_runtime_error() -> None:
    """Daemon 5xx surfaces as RuntimeError so caller can re-route on
    audit chain outage."""
    daemon_url = "http://localhost:4471"

    def _handler(request: httpx.Request) -> httpx.Response:
        return httpx.Response(503, json={"error": "chain_offline"})

    async with httpx.AsyncClient(transport=httpx.MockTransport(_handler)) as client:
                                                                     
                                                                    
                                          
        _audit_emitter = emit_voice_query_dispatched
        _inbox_poster = post_inbox_notification

        with pytest.raises(RuntimeError, match="audit chain emit failed"):
            await dispatch_voice_query(
                query="x",
                operator_id="testuser",
                project_id="zen-swarm-sha256",
                explicit_override=None,
                cross_project=False,
                project_count=1,
                daemon_url=daemon_url,
                client=client,
                audit_emitter=_audit_emitter,
                inbox_poster=_inbox_poster,
            )
