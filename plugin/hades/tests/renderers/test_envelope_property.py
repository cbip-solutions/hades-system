# SPDX-License-Identifier: MIT
"""Property-based tests: envelope round-trip + render correctness under hypothesis-generated inputs."""

from __future__ import annotations

import json
from datetime import datetime, timezone
from typing import Any
from unittest.mock import patch

import pytest
from hermes_plugins.hades.renderers import Renderer
from hermes_plugins.hades.renderers.email_citation import EmailCitationRenderer
from hermes_plugins.hades.renderers.ink_citation import InkCitationRenderer
from hermes_plugins.hades.renderers.slack_citation import SlackCitationRenderer
from hermes_plugins.hades.renderers.telegram_citation import (
    TelegramCitationRenderer,
)
from hermes_plugins.hades.renderers.types import (
    AugmentationResult,
    CitationSource,
    CitationType,
    Envelope,
    Platform,
    RetrievalLane,
)
from hermes_plugins.hades.renderers.voice_citation import VoiceCitationRenderer
from hermes_plugins.hades.renderers.web_citation import WebCitationRenderer
from hypothesis import HealthCheck, given, settings
from hypothesis import strategies as st

                                                                             
                       
                                                                             


                                                                             
                                                          
_safe_text = st.text(
    alphabet=st.characters(
        min_codepoint=0x20,
        max_codepoint=0x7E,
        exclude_characters="<>&\"'`",
    ),
    min_size=1,
    max_size=80,
)
_doctrines = st.sampled_from(["max-scope", "default", "capa-firewall"])


def _citation_id_strategy() -> st.SearchStrategy[str]:
    """Generate ``c-XXX..`` citation IDs that pass Phase D Validate format."""
    return st.text(
        alphabet="abcdefghijklmnopqrstuvwxyz0123456789",
        min_size=2,
        max_size=16,
    ).map(lambda s: f"c-{s}")


def _event_id_strategy() -> st.SearchStrategy[str]:
    return st.text(
        alphabet="abcdefghijklmnopqrstuvwxyz0123456789",
        min_size=4,
        max_size=24,
    ).map(lambda s: f"evt-{s}")


@st.composite
def _envelope_strategy(draw: Any) -> Envelope:
    return Envelope(
        id=draw(_citation_id_strategy()),
        type=draw(st.sampled_from(list(CitationType))),
        source=draw(st.sampled_from(list(CitationSource))),
        retrieval_lane=draw(st.sampled_from(list(RetrievalLane))),
        audit_event_id=draw(_event_id_strategy()),
        confidence=draw(
            st.floats(min_value=0.0, max_value=1.0, allow_nan=False, allow_infinity=False)
        ),
        rrf_score=draw(
            st.floats(
                min_value=0.0,
                max_value=0.0164,
                allow_nan=False,
                allow_infinity=False,
            )
        ),
        rrf_rank=draw(st.integers(min_value=-1, max_value=999)),
        project_id=draw(_safe_text),
        payload=draw(_safe_text),
    )


@st.composite
def _augmentation_result_strategy(draw: Any) -> AugmentationResult:
    return AugmentationResult(
        request_id=draw(
            st.text(
                alphabet="abcdefghijklmnopqrstuvwxyz0123456789-",
                min_size=1,
                max_size=20,
            )
        ),
        session_id=draw(
            st.text(
                alphabet="abcdefghijklmnopqrstuvwxyz0123456789-",
                min_size=1,
                max_size=20,
            )
        ),
        doctrine=draw(_doctrines),
        project_id=draw(_safe_text),
        citations=draw(st.lists(_envelope_strategy(), min_size=0, max_size=5)),
        emitted_at=draw(
            st.datetimes(
                min_value=datetime(2020, 1, 1),
                max_value=datetime(2030, 12, 31),
            ).map(lambda d: d.replace(tzinfo=timezone.utc))
        ),
        kg_token_count=draw(st.integers(min_value=0, max_value=100000)),
        cache_key_hash=draw(
            st.text(alphabet="abcdef0123456789", min_size=1, max_size=64).map(
                lambda s: f"sha256:{s}"
            )
        ),
        audit_event_id=draw(_event_id_strategy()),
    )


                                                                             
                                     
                                                                             


@given(envelope=_envelope_strategy())
@settings(max_examples=100, suppress_health_check=[HealthCheck.too_slow])
def test_property_envelope_json_round_trip(envelope: Envelope) -> None:
    """Envelope.from_json(envelope.to_json()) == envelope (per-citation)."""
    raw = envelope.to_json()
    parsed = Envelope.from_json(raw)
    assert parsed == envelope


@given(result_obj=_augmentation_result_strategy())
@settings(max_examples=100, suppress_health_check=[HealthCheck.too_slow])
def test_property_augmentation_result_json_round_trip(
    result_obj: AugmentationResult,
) -> None:
    """AugmentationResult JSON round-trip preserves equality (wrapper level)."""
    raw = result_obj.to_json()
    parsed = AugmentationResult.from_json(raw)
    assert parsed == result_obj


                                                                             
                        
                                                                             


@given(result_obj=_augmentation_result_strategy())
@settings(max_examples=50, suppress_health_check=[HealthCheck.too_slow])
def test_property_render_idempotent_ink(result_obj: AugmentationResult) -> None:
    """Rendering same wrapper twice produces identical output (deterministic)."""
    r = InkCitationRenderer()
    with patch.object(InkCitationRenderer, "audit_anchor", return_value="evt-mocked"):
        result1 = r.render(result_obj)
        result2 = r.render(result_obj)
    assert json.dumps(result1.output, sort_keys=True) == json.dumps(
        result2.output, sort_keys=True
    )


@given(result_obj=_augmentation_result_strategy())
@settings(max_examples=30, suppress_health_check=[HealthCheck.too_slow])
def test_property_augmentation_result_immutable_after_render(
    result_obj: AugmentationResult,
) -> None:
    """Rendering does not mutate the wrapper (frozen-dataclass invariant)."""
    pre_state = result_obj.to_json()
    renderers: list[Renderer] = [
        InkCitationRenderer(),
        TelegramCitationRenderer(),
        SlackCitationRenderer(),
        EmailCitationRenderer(),
        VoiceCitationRenderer(),
        WebCitationRenderer(),
    ]
    with patch.object(Renderer, "audit_anchor", return_value="evt-mocked"):
        for r in renderers:
            r.render(result_obj)
    assert result_obj.to_json() == pre_state


                                                                             
                                                                  
                                                                             


_renderer_classes = [
    InkCitationRenderer,
    TelegramCitationRenderer,
    SlackCitationRenderer,
    EmailCitationRenderer,
    VoiceCitationRenderer,
    WebCitationRenderer,
]


@pytest.mark.parametrize("renderer_cls", _renderer_classes)
@given(result_obj=_augmentation_result_strategy())
@settings(
    max_examples=30,
    suppress_health_check=[
        HealthCheck.too_slow,
        HealthCheck.function_scoped_fixture,
    ],
)
def test_property_audit_event_ids_count_matches_citations(
    renderer_cls: type[Renderer],
    result_obj: AugmentationResult,
) -> None:
    """For ANY hypothesis-generated wrapper: audit_event_ids count == citations count."""
    r = renderer_cls()
    with patch.object(renderer_cls, "audit_anchor", return_value="evt-mocked"):
        result = r.render(result_obj)
    assert len(result.audit_event_ids) == len(result_obj.citations)


@pytest.mark.parametrize("renderer_cls", _renderer_classes)
@given(result_obj=_augmentation_result_strategy())
@settings(
    max_examples=30,
    suppress_health_check=[
        HealthCheck.too_slow,
        HealthCheck.function_scoped_fixture,
    ],
)
def test_property_render_returns_correct_platform(
    renderer_cls: type[Renderer],
    result_obj: AugmentationResult,
) -> None:
    """Every renderer's output.platform matches its declared PLATFORM."""
    r = renderer_cls()
    with patch.object(renderer_cls, "audit_anchor", return_value="evt-mocked"):
        result = r.render(result_obj)
    assert result.platform == r.PLATFORM


                                                                             
                                                                            
                                                                             


def _augmentation_result_with_doctrine_strategy(
    doctrine: str,
) -> st.SearchStrategy[AugmentationResult]:
    """Hypothesis strategy yielding wrappers with a fixed doctrine + 1-5 citations.

    Used by ``test_property_dispatch_capa_firewall_filter`` to exercise
    the doctrine-aware filter end-to-end. Constrains ``min_size=1`` so
    the filter's "no renderer registered" + "doctrine-disabled" paths
    both yield observable markdown_fallback output (the empty-citations
    placeholder is a separate code path).
    """

    @st.composite
    def _strategy(draw: Any) -> AugmentationResult:
        return AugmentationResult(
            request_id=draw(
                st.text(
                    alphabet="abcdefghijklmnopqrstuvwxyz0123456789-",
                    min_size=1,
                    max_size=20,
                )
            ),
            session_id=draw(
                st.text(
                    alphabet="abcdefghijklmnopqrstuvwxyz0123456789-",
                    min_size=1,
                    max_size=20,
                )
            ),
            doctrine=doctrine,
            project_id=draw(_safe_text),
            citations=draw(st.lists(_envelope_strategy(), min_size=1, max_size=5)),
            emitted_at=draw(
                st.datetimes(
                    min_value=datetime(2020, 1, 1),
                    max_value=datetime(2030, 12, 31),
                ).map(lambda d: d.replace(tzinfo=timezone.utc))
            ),
            kg_token_count=draw(st.integers(min_value=0, max_value=100000)),
            cache_key_hash=draw(
                st.text(alphabet="abcdef0123456789", min_size=1, max_size=64).map(
                    lambda s: f"sha256:{s}"
                )
            ),
            audit_event_id=draw(_event_id_strategy()),
        )

    return _strategy()


                                                                           
                                                                 
                                                                   
                                                                        
_CAPA_FIREWALL_ENABLED = {Platform.INK, Platform.EMAIL, Platform.MARKDOWN_FALLBACK}
_CAPA_FIREWALL_DISABLED = {
    Platform.VOICE,
    Platform.TELEGRAM,
    Platform.SLACK,
    Platform.WEB,
}


@given(result_obj=_augmentation_result_with_doctrine_strategy("capa-firewall"))
@settings(
    max_examples=30,
    deadline=None,
    suppress_health_check=[
        HealthCheck.too_slow,
        HealthCheck.function_scoped_fixture,
    ],
)
def test_property_dispatch_capa_firewall_filter(
    result_obj: AugmentationResult,
) -> None:
    """RendererRegistry.dispatch under capa-firewall:
    - Platforms in {VOICE, TELEGRAM, SLACK, WEB} are doctrine-disabled →
      output MUST be the Go-format markdown fallback (per-citation
      footnote starting with ``[^<citation_id>]``).
    - Platforms in {INK, EMAIL} are doctrine-enabled → output MUST NOT
      be the markdown fallback (each renderer emits its own native
      shape).

    Anchors capa-firewall as the strict privacy boundary across the
    entire wrapper space hypothesis can generate. The pre-fix-cycle test
    surface only covered the fixed-sample dispatch path
    (``test_envelope_seven_renderers.py``); this property test exercises
    the doctrine filter for any valid wrapper shape and any platform.
    """
    from hermes_plugins.hades.renderers import (  # noqa: PLC0415
        RendererRegistry,
        register_default_renderers,
    )

    reg = RendererRegistry()
    register_default_renderers(reg)
    with patch.object(Renderer, "audit_anchor", return_value="evt-mocked"):
        for platform in _CAPA_FIREWALL_DISABLED:
            result = reg.dispatch(result_obj, platform)
            assert result.platform == Platform.MARKDOWN_FALLBACK, (
                f"{platform.value}: doctrine-disabled in capa-firewall but "
                f"dispatch returned {result.platform.value}"
            )
                                                                         
                                                                     
                                                                         
                                                                          
                                    
            assert len(result.audit_event_ids) == len(result_obj.citations)
            output = result.output
            assert isinstance(output, str)
                                                                       
                                                               
            first_id = result_obj.citations[0].id
            assert output.startswith(f"[^{first_id}]"), (
                f"{platform.value}: doctrine-disabled output is not in Go-format "
                f"markdown fallback shape (expected leading ``[^{first_id}]``; "
                f"got {output[:80]!r})"
            )

        for platform in _CAPA_FIREWALL_ENABLED - {Platform.MARKDOWN_FALLBACK}:
            result = reg.dispatch(result_obj, platform)
            assert result.platform == platform, (
                f"{platform.value}: doctrine-enabled in capa-firewall but "
                f"dispatch returned {result.platform.value}"
            )

                                                                   
                                                          
                                                                       
                                                         
        fb = reg.dispatch(result_obj, Platform.MARKDOWN_FALLBACK)
        assert fb.platform == Platform.MARKDOWN_FALLBACK


@given(result_obj=_augmentation_result_with_doctrine_strategy("max-scope"))
@settings(
    max_examples=20,
    deadline=None,
    suppress_health_check=[
        HealthCheck.too_slow,
        HealthCheck.function_scoped_fixture,
    ],
)
def test_property_dispatch_max_scope_enables_every_platform(
    result_obj: AugmentationResult,
) -> None:
    """RendererRegistry.dispatch under max-scope: every platform renders
    natively; no doctrine-driven fallback occurs.

    Complement to ``test_property_dispatch_capa_firewall_filter``: the
    matrix has three doctrines; verifying capa-firewall's strict
    filtering without verifying max-scope's permissive routing would
    leave a quiet space where the matrix could regress to
    overly-restrictive defaults.
    """
    from hermes_plugins.hades.renderers import (  # noqa: PLC0415
        RendererRegistry,
        register_default_renderers,
    )

    reg = RendererRegistry()
    register_default_renderers(reg)
    with patch.object(Renderer, "audit_anchor", return_value="evt-mocked"):
        for platform in (
            Platform.INK,
            Platform.TELEGRAM,
            Platform.SLACK,
            Platform.EMAIL,
            Platform.VOICE,
            Platform.WEB,
        ):
            result = reg.dispatch(result_obj, platform)
            assert result.platform == platform, (
                f"{platform.value}: doctrine-enabled in max-scope but dispatch "
                f"returned {result.platform.value}"
            )
