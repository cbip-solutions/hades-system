# SPDX-License-Identifier: MIT
"""Shared pytest fixtures for AFK richness tests."""

from __future__ import annotations

from collections.abc import Callable, Iterator
from typing import Any

import httpx
import pytest


@pytest.fixture
def daemon_url() -> str:
    """Canonical zen-swarm-ctld TCP listen URL."""
    return "http://localhost:4471"


@pytest.fixture
def mock_daemon_responses() -> dict[str, Any]:
    """Canned daemon HTTP responses keyed by URL path.

    The path ``/v1/audit/event/<id>`` returns a full envelope payload
    matching the AFK short-render contract (citation_id + title + top_fields
    + audit_event_id + project_id + cache_state). The path
    ``/v1/knowledge/query`` returns aggregator-style rows for cache
    hydration. The path ``/v1/notifications/inbox`` returns the canned ack.
    """
    return {
        "/v1/audit/event/evt-1234abcd": {
            "envelope": {
                "citation_id": "c-001",
                "title": "cmd/zen/cli/audit_event.go",
                "top_fields": [
                    ["blast_radius", "12 callers"],
                    [
                        "top_callers",
                        "dispatcher.Forward, orchestrator.Plan, hra.Decide",
                    ],
                    ["community", "daemon-bootstrap"],
                ],
                "full_payload": {
                    "callers": [
                        "dispatcher.Forward",
                        "orchestrator.Plan",
                        "hra.Decide",
                    ],
                    "callees": ["audit.AnchorTessera", "store.WriteEvent"],
                    "blast_radius_score": 12,
                    "community_label": "daemon-bootstrap",
                    "tessera_anchor": "0xdeadbeef",
                },
                "audit_event_id": "evt-1234abcd",
                "project_id": "zen-swarm",
                "cache_state": "fresh",
            }
        },
        "/v1/knowledge/query": {
            "results": [
                {
                    "citation_id": "c-001",
                    "query_hash": "abc123",
                    "project_id": "zen-swarm",
                    "envelope_payload": {
                        "title": "cmd/zen/cli/audit_event.go",
                        "top_fields": [["blast_radius", "12 callers"]],
                    },
                    "community_summary": "daemon-bootstrap",
                    "ingested_at_unix_ms": 1746998400000,
                }
            ]
        },
        "/v1/notifications/inbox": {"id": 42, "ack": "queued"},
    }


@pytest.fixture
def mock_daemon(mock_daemon_responses: dict[str, Any]) -> httpx.AsyncClient:
    """Return an httpx.AsyncClient replaying the canned canned responses."""

    def _handler(request: httpx.Request) -> httpx.Response:
        body = mock_daemon_responses.get(request.url.path)
        if body is None:
            return httpx.Response(
                404,
                json={"error": "no canned response", "path": request.url.path},
            )
        return httpx.Response(200, json=body)

    return httpx.AsyncClient(transport=httpx.MockTransport(_handler))


@pytest.fixture
def citation_envelope_factory() -> Callable[..., dict[str, Any]]:
    """Builds canned citation envelopes for short-render tests."""

    def _build(
        citation_id: str = "c-001",
        cache_state: str = "fresh",
        project_id: str = "zen-swarm",
        top_field_count: int = 3,
    ) -> dict[str, Any]:
        all_fields: list[list[str]] = [
            ["blast_radius", "12 callers"],
            ["top_callers", "dispatcher.Forward, orchestrator.Plan, hra.Decide"],
            ["community", "daemon-bootstrap"],
            ["complexity", "high"],                                        
        ]
        return {
            "citation_id": citation_id,
            "title": f"file/{citation_id}.go",
            "top_fields": all_fields[:top_field_count],
            "full_payload": {
                "callers": ["x", "y", "z"],
                "blast_radius_score": 12,
            },
            "audit_event_id": f"evt-{citation_id}",
            "project_id": project_id,
            "cache_state": cache_state,
        }

    return _build


@pytest.fixture
def audit_event_capture() -> Iterator[list[dict[str, Any]]]:
    """Captures audit events emitted during a test (yielded as a list)."""
    captured: list[dict[str, Any]] = []
    yield captured
