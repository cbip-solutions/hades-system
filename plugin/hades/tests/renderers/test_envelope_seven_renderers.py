# SPDX-License-Identifier: MIT
"""Integration test: AugmentationResult → all 7 renderers (6 platform + markdown fallback)."""

from __future__ import annotations

from unittest.mock import patch

import pytest
from hermes_plugins.hades.renderers import (
    Renderer,
    RendererRegistry,
    register_default_renderers,
)
from hermes_plugins.hades.renderers.types import (
    AugmentationResult,
    Platform,
)


@pytest.fixture(autouse=True)
def _mock_audit_anchor():
    """Avoid network calls in audit_anchor across this test module."""
    with patch.object(Renderer, "audit_anchor", return_value="evt-mocked"):
        yield


@pytest.fixture
def registry_with_defaults() -> RendererRegistry:
    """RendererRegistry pre-populated with all 6 platform renderers."""
    reg = RendererRegistry()
    register_default_renderers(reg)
    return reg


def _override_doctrine(src: AugmentationResult, doctrine: str) -> AugmentationResult:
    """Construct a deep copy with the doctrine overridden."""
    return AugmentationResult(
        request_id=src.request_id,
        session_id=src.session_id,
        doctrine=doctrine,
        project_id=src.project_id,
        citations=src.citations,
        emitted_at=src.emitted_at,
        kg_token_count=src.kg_token_count,
        cache_key_hash=src.cache_key_hash,
        audit_event_id=src.audit_event_id,
        static_context=src.static_context,
        volatile_context=src.volatile_context,
    )


def test_seven_renderers_all_produce_output_for_max_scope_envelope(
    registry_with_defaults: RendererRegistry,
    sample_augmentation_result: AugmentationResult,
) -> None:
    """All 7 platform renderings emit non-empty output under max-scope doctrine."""
    env = _override_doctrine(sample_augmentation_result, "max-scope")

    for platform in [
        Platform.INK,
        Platform.TELEGRAM,
        Platform.SLACK,
        Platform.EMAIL,
        Platform.VOICE,
        Platform.WEB,
        Platform.MARKDOWN_FALLBACK,
    ]:
        result = registry_with_defaults.dispatch(env, platform)
        assert result is not None, f"{platform.value}: dispatch returned None"
        assert result.output, f"{platform.value}: empty output"


def test_capa_firewall_doctrine_applies_renderer_filter_correctly(
    registry_with_defaults: RendererRegistry,
    sample_augmentation_result: AugmentationResult,
) -> None:
    """capa-firewall: only INK + EMAIL + MARKDOWN_FALLBACK enabled.

    Voice/telegram/slack/web disabled → fall back to markdown.
    """
    env_capa = _override_doctrine(sample_augmentation_result, "capa-firewall")

    for blocked_platform in [
        Platform.VOICE,
        Platform.TELEGRAM,
        Platform.SLACK,
        Platform.WEB,
    ]:
        result = registry_with_defaults.dispatch(env_capa, blocked_platform)
        assert result.platform == Platform.MARKDOWN_FALLBACK, (
            f"{blocked_platform.value}: expected markdown_fallback in capa-firewall"
        )

                                                        
    for allowed in [Platform.INK, Platform.EMAIL, Platform.MARKDOWN_FALLBACK]:
        result = registry_with_defaults.dispatch(env_capa, allowed)
        assert result.platform == allowed, (
            f"{allowed.value}: should be enabled in capa-firewall"
        )


def test_augmentation_result_immutability_after_render(
    registry_with_defaults: RendererRegistry,
    sample_augmentation_result: AugmentationResult,
) -> None:
    """Frozen AugmentationResult: rendering must not mutate (would raise)."""
    pre_state = sample_augmentation_result.to_json()
    for platform in [
        Platform.WEB,
        Platform.EMAIL,
        Platform.INK,
        Platform.TELEGRAM,
        Platform.SLACK,
        Platform.VOICE,
        Platform.MARKDOWN_FALLBACK,
    ]:
        registry_with_defaults.dispatch(sample_augmentation_result, platform)
    assert sample_augmentation_result.to_json() == pre_state, (
        "AugmentationResult mutated by renderer pipeline"
    )


def test_register_default_renderers_populates_six_platform_slots(
    registry_with_defaults: RendererRegistry,
) -> None:
    """register_default_renderers populates exactly 6 platform slots."""
    # noqa: SLF001 — internal slot inspection allowed in tests
    expected = {
        Platform.INK,
        Platform.TELEGRAM,
        Platform.SLACK,
        Platform.EMAIL,
        Platform.VOICE,
        Platform.WEB,
    }
    assert set(registry_with_defaults._renderers.keys()) == expected  # noqa: SLF001
                                                                           
    assert Platform.MARKDOWN_FALLBACK not in registry_with_defaults._renderers  # noqa: SLF001


def test_markdown_fallback_output_includes_citation_audit_links(
    registry_with_defaults: RendererRegistry,
    sample_augmentation_result: AugmentationResult,
) -> None:
    """Markdown fallback output includes zen://audit/<event-id> per citation."""
    result = registry_with_defaults.dispatch(
        sample_augmentation_result, Platform.MARKDOWN_FALLBACK
    )
    out = result.output
    assert isinstance(out, str)
    for citation in sample_augmentation_result.citations:
        assert f"zen://audit/{citation.audit_event_id}" in out


def test_markdown_fallback_audit_event_ids_match_citation_ids(
    registry_with_defaults: RendererRegistry,
    sample_augmentation_result: AugmentationResult,
) -> None:
    """Markdown fallback audit_event_ids carry the original  event ids."""
    result = registry_with_defaults.dispatch(
        sample_augmentation_result, Platform.MARKDOWN_FALLBACK
    )
    expected = [c.audit_event_id for c in sample_augmentation_result.citations]
    assert result.audit_event_ids == expected
