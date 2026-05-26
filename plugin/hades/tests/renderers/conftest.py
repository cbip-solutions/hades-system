# SPDX-License-Identifier: MIT
"""Shared pytest fixtures for renderer tests."""

from __future__ import annotations

import json
from datetime import datetime, timezone
from typing import Any

import pytest
from hermes_plugins.hades.renderers.types import (
    AugmentationResult,
    CitationSource,
    CitationType,
    Envelope,
    RetrievalLane,
)


@pytest.fixture
def sample_envelope_caronte() -> Envelope:
    """Single caronte-source per-citation envelope, frozen-time deterministic.

    Plan 19: source renamed gitnexus_query → caronte_query (Plan 11 §3.1 tool
    name `mcp_zen-swarm_caronte_query`). Wire value ``caronte_query`` matches Go
    ``SourceCaronteQuery``.
    """
    return Envelope(
        id="c-1234abcd0123",
        type=CitationType.KG_NODE,
        source=CitationSource.CARONTE_QUERY,
        retrieval_lane=RetrievalLane.SEMANTIC,
        audit_event_id="evt-1234abcd",
        confidence=0.94,
        rrf_score=0.0162,
        rrf_rank=0,
        project_id="zen-swarm",
        payload=(
            "MergeEngine.SelectWinner: selects winner from N candidates via "
            "test_pass + reviewer_agreement + blast_radius "
            "(internal/orchestrator/merge/winner.go:142)"
        ),
    )


@pytest.fixture
def sample_envelope_gitnexus(sample_envelope_caronte: Envelope) -> Envelope:
    """Backward-compat alias for sample_envelope_caronte.

    Pre-Plan-19 tests referenced this fixture by the old name; keeping the
    alias avoids a cascade rename across all renderer tests. The fixture now
    returns a CARONTE_QUERY envelope (the canonical post-Plan-19 source).
    """
    return sample_envelope_caronte


@pytest.fixture
def sample_envelope_aggregator() -> Envelope:
    """Plan 9 D aggregator-source per-citation envelope."""
    return Envelope(
        id="c-5678efgh4567",
        type=CitationType.FILE_SLICE,
        source=CitationSource.AGGREGATOR_FTS,
        retrieval_lane=RetrievalLane.LEXICAL,
        audit_event_id="evt-5678efgh",
        confidence=0.78,
        rrf_score=0.0148,
        rrf_rank=1,
        project_id="zen-swarm",
        payload=(
            "WorkforceQueue priority enum amendment 0042 added Priority.URGENT "
            "slot (internal/workforce/queue.go:18)"
        ),
    )


@pytest.fixture
def sample_envelope(sample_envelope_caronte: Envelope) -> Envelope:
    """Single representative per-citation envelope for renderer-input tests.

    Backwards-compatible name for older tests that need a single citation.
    Renderers themselves consume `AugmentationResult`; use the wrapper
    fixture below for those tests.
    """
    return sample_envelope_caronte


@pytest.fixture
def sample_augmentation_result(
    sample_envelope_gitnexus: Envelope,
    sample_envelope_aggregator: Envelope,
) -> AugmentationResult:
    """Two-citation augmentation result wrapper, frozen-time deterministic."""
    return AugmentationResult(
        request_id="req-aaaabbbb",
        session_id="sess-ccccdddd",
        doctrine="default",
        project_id="zen-swarm",
        citations=[sample_envelope_gitnexus, sample_envelope_aggregator],
        emitted_at=datetime(2026, 5, 10, 12, 0, 0, tzinfo=timezone.utc),
        kg_token_count=512,
        cache_key_hash="sha256:abcd1234",
        audit_event_id="evt-augmentation-completed-aaaa",
        static_context="project=zen-swarm; doctrine=default",
        volatile_context="callers: dispatcher.go:67",
    )


@pytest.fixture
def sample_augmentation_result_json(
    sample_augmentation_result: AugmentationResult,
) -> str:
    """JSON-wire form (matches Go AugmentationResult MarshalJSON)."""
    return sample_augmentation_result.to_json()


@pytest.fixture
def sample_augmentation_result_dict(
    sample_augmentation_result: AugmentationResult,
) -> dict[str, Any]:
    """Dict form for parametrised tests."""
    return json.loads(sample_augmentation_result.to_json())


@pytest.fixture
def empty_augmentation_result() -> AugmentationResult:
    """Zero-citation augmentation result (edge case: no augmentation occurred)."""
    return AugmentationResult(
        request_id="req-empty",
        session_id="sess-empty",
        doctrine="capa-firewall",
        project_id="zen-swarm",
        citations=[],
        emitted_at=datetime(2026, 5, 10, 12, 0, 0, tzinfo=timezone.utc),
        kg_token_count=0,
        cache_key_hash="sha256:empty",
        audit_event_id="evt-augmentation-skipped-empty",
    )
