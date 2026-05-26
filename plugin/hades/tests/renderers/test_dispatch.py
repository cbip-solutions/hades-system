# SPDX-License-Identifier: MIT
"""RendererRegistry dispatch + doctrine-aware enable/disable tests.

The registry is the orchestration boundary between the daemon `/v1/augment`
JSON response (parsed into `AugmentationResult`) and the platform-specific
renderer pipeline. Doctrine-aware filter is the privacy boundary integration
(capa-firewall disables voice/telegram/slack/web third-party leak surfaces).
"""

from __future__ import annotations

from unittest.mock import patch

import pytest
from hermes_plugins.hades.renderers import Renderer, RendererRegistry
from hermes_plugins.hades.renderers.types import (
    AugmentationResult,
    Platform,
    RenderResult,
)


class StubRenderer(Renderer):
    """Test double satisfying Renderer ABC.

    Renderers consume an `AugmentationResult` wrapper (so they have access
    to doctrine + project_id context for cross-project filtering), iterate
    over the per-citation `Envelope` rows internally.
    """

    PLATFORM = Platform.INK

    def render(self, result: AugmentationResult) -> RenderResult:
        return RenderResult(
            platform=self.PLATFORM,
            output=f"stub-render-{len(result.citations)}",
        )


class FailingRenderer(Renderer):
    """Test double that always panics — exercises fallback path."""

    PLATFORM = Platform.SLACK

    def render(self, result: AugmentationResult) -> RenderResult:
        raise RuntimeError("intentional renderer failure")


                                                                             
                              
                                                                             


def test_registry_register_and_dispatch(
    sample_augmentation_result: AugmentationResult,
) -> None:
    """Registry routes AugmentationResult to the registered renderer matching platform."""
    reg = RendererRegistry()
    reg.register(StubRenderer())
    result = reg.dispatch(sample_augmentation_result, Platform.INK)
    assert result.platform == Platform.INK
    assert result.output == "stub-render-2"


def test_registry_register_duplicates_replaces() -> None:
    """Re-registering same platform replaces previous renderer (last-wins)."""
    reg = RendererRegistry()
    r1 = StubRenderer()
    r2 = StubRenderer()
    reg.register(r1)
    reg.register(r2)
                                                                    
    assert reg._renderers[Platform.INK] is r2  # noqa: SLF001


def test_registry_unknown_platform_falls_back_to_markdown(
    sample_augmentation_result: AugmentationResult,
) -> None:
    """No renderer registered for the platform → markdown_fallback path."""
    reg = RendererRegistry()
                                
    result = reg.dispatch(sample_augmentation_result, Platform.INK)
    assert result.platform == Platform.MARKDOWN_FALLBACK
                                                                      
    assert isinstance(result.output, str)
    assert result.output


def test_registry_renderer_failure_falls_back_to_markdown(
    sample_augmentation_result: AugmentationResult,
) -> None:
    """If registered renderer panics → catch → emit markdown_fallback."""
    reg = RendererRegistry()
    reg.register(FailingRenderer())
    result = reg.dispatch(sample_augmentation_result, Platform.SLACK)
    assert result.platform == Platform.MARKDOWN_FALLBACK
    assert result.output                      


def test_registry_empty_citations_markdown_fallback(
    empty_augmentation_result: AugmentationResult,
) -> None:
    """Empty citations + no renderer → fallback emits '(no citations)' note."""
    reg = RendererRegistry()
    result = reg.dispatch(empty_augmentation_result, Platform.INK)
    assert result.platform == Platform.MARKDOWN_FALLBACK
    assert "no citations" in result.output.lower()


                                                                             
                                        
                                                                             


def test_is_enabled_max_scope_allows_all() -> None:
    reg = RendererRegistry()
    for platform in [
        Platform.INK,
        Platform.TELEGRAM,
        Platform.SLACK,
        Platform.EMAIL,
        Platform.VOICE,
        Platform.WEB,
        Platform.MARKDOWN_FALLBACK,
    ]:
        assert reg.is_enabled(platform, "max-scope"), platform.value


def test_is_enabled_default_enables_all_six_platforms() -> None:
    """Default doctrine enables all 6 platform renderers (including voice)."""
    reg = RendererRegistry()
    for platform in [
        Platform.INK,
        Platform.TELEGRAM,
        Platform.SLACK,
        Platform.EMAIL,
        Platform.VOICE,
        Platform.WEB,
    ]:
        assert reg.is_enabled(platform, "default"), platform.value


def test_is_enabled_capa_firewall_disables_third_party_surfaces() -> None:
    """capa-firewall disables voice/telegram/slack/web (privacy default).

    Only local TUI (Ink), operator-controlled email, and the markdown
    fallback remain enabled in capa-firewall.
    """
    reg = RendererRegistry()
                               
    assert not reg.is_enabled(Platform.VOICE, "capa-firewall")
    assert not reg.is_enabled(Platform.TELEGRAM, "capa-firewall")
    assert not reg.is_enabled(Platform.SLACK, "capa-firewall")
    assert not reg.is_enabled(Platform.WEB, "capa-firewall")
                              
    assert reg.is_enabled(Platform.INK, "capa-firewall")
    assert reg.is_enabled(Platform.EMAIL, "capa-firewall")
    assert reg.is_enabled(Platform.MARKDOWN_FALLBACK, "capa-firewall")


def test_is_enabled_unknown_doctrine_returns_false() -> None:
    """Unknown doctrine string returns False for every platform (defensive)."""
    reg = RendererRegistry()
    assert not reg.is_enabled(Platform.INK, "unknown-doctrine")


def test_dispatch_disabled_falls_back_to_markdown(
    sample_augmentation_result: AugmentationResult,
) -> None:
    """When renderer disabled by doctrine → markdown_fallback (no info loss)."""
    reg = RendererRegistry()
    reg.register(StubRenderer())                           

                                                   
    result_capa = AugmentationResult(
        request_id=sample_augmentation_result.request_id,
        session_id=sample_augmentation_result.session_id,
        doctrine="capa-firewall",
        project_id=sample_augmentation_result.project_id,
        citations=sample_augmentation_result.citations,
        emitted_at=sample_augmentation_result.emitted_at,
        kg_token_count=sample_augmentation_result.kg_token_count,
        cache_key_hash=sample_augmentation_result.cache_key_hash,
        audit_event_id=sample_augmentation_result.audit_event_id,
        static_context=sample_augmentation_result.static_context,
        volatile_context=sample_augmentation_result.volatile_context,
    )
                                                         
    result = reg.dispatch(result_capa, Platform.VOICE)
    assert result.platform == Platform.MARKDOWN_FALLBACK


                                                                             
                       
                                                                             


def test_renderer_abc_cannot_instantiate() -> None:
    """Renderer ABC must not be instantiable directly."""
    with pytest.raises(TypeError):
        Renderer()  # type: ignore[abstract]


def test_renderer_subclass_must_set_platform() -> None:
    """Subclass without PLATFORM attribute should raise on class creation."""
    with pytest.raises(TypeError, match="PLATFORM"):

        class BadRenderer(Renderer):
            def render(self, result: AugmentationResult) -> RenderResult:
                return RenderResult(platform=Platform.INK, output="x")


def test_renderer_audit_anchor_default_network_failure_returns_empty() -> None:
    """Default audit_anchor: when network call fails → returns empty string + logs warning.

    inv-zen-166: audit anchoring is a side-channel; failure here is
    non-fatal and must not abort rendering. The renderer continues with
    audit_event_ids[] entries that reflect the original envelope's
    audit_event_id (not the anchor RPC return value).
    """
    from datetime import datetime, timezone

    from hermes_plugins.hades.renderers.types import (
        CitationSource,
        CitationType,
        Envelope,
        RetrievalLane,
    )

    citation = Envelope(
        id="c-anchortest1",
        type=CitationType.KG_NODE,
        source=CitationSource.GITNEXUS_QUERY,
        retrieval_lane=RetrievalLane.SEMANTIC,
        audit_event_id="evt-anchor-test",
        confidence=0.9,
        rrf_score=0.01,
        rrf_rank=0,
        project_id="p",
        payload="payload",
    )
                                                                          
                                                                         
    _wrapper = AugmentationResult(
        request_id="r",
        session_id="s",
        doctrine="default",
        project_id="p",
        citations=[citation],
        emitted_at=datetime.now(timezone.utc),
        kg_token_count=0,
        cache_key_hash="sha256:0",
        audit_event_id="evt-aug",
    )
    assert _wrapper.citations[0] is citation
    r = StubRenderer()
    with patch("httpx.Client") as mock_client_cls:
        instance = mock_client_cls.return_value.__enter__.return_value
        instance.post.side_effect = Exception("network down")
        out = r.audit_anchor(
            citation,
            doctrine="default",
            audit_endpoint="http://invalid:9/v1/audit/emit",
        )
    assert out == ""


                                                                             
                                                                           
                                                                             


def test_renderer_default_audit_endpoint_uses_canonical_daemon_port_4471() -> None:
    """The Renderer's default audit_endpoint targets the canonical daemon port.

    Source-of-truth: cmd/zen-mcp-budget/main.go:15 +
    cmd/zen-mcp-audit/main.go:16 document the canonical TCP URL as
    ``http://localhost:4471``. Production zen-swarm-ctld either listens on
    this TCP address (when ``--http 127.0.0.1:4471`` is passed) or on the
    UDS path; the renderers' audit-anchor side-channel uses the TCP form.

    Regression guard: pre-I-1 default was ``http://localhost:7345``
    (never a real daemon port); pre-C-1 path was ``/v1/audit/anchor``
    (never a registered daemon route). The canonical wire shape is pinned
    by ``test_audit_emit_canonical_parity.py``; this test only asserts
    the URL constants stay aligned with the cmd/zen-mcp-* doc contract.
    """
    from hermes_plugins.hades.renderers import (
        DEFAULT_AUDIT_ENDPOINT,
        DEFAULT_DAEMON_URL,
    )

    assert DEFAULT_DAEMON_URL == "http://localhost:4471"
    assert DEFAULT_AUDIT_ENDPOINT == "http://localhost:4471/v1/audit/emit"


def test_renderer_audit_anchor_uses_instance_endpoint_when_no_override() -> None:
    """``Renderer.audit_anchor`` (no explicit endpoint arg) hits ``self._audit_endpoint``.

    The instance-attribute default is set by ``__init__``: a renderer
    constructed without a ``daemon_url`` keyword sees
    ``DEFAULT_AUDIT_ENDPOINT`` (the canonical 4471 form); one constructed
    with ``daemon_url="http://10.0.0.42:8080"`` sees a derived endpoint
    pointing at that host. This is what ``register_default_renderers``
    threads through when zen-side wiring passes a non-default daemon URL.
    """
    from hermes_plugins.hades.renderers import DEFAULT_AUDIT_ENDPOINT
    from hermes_plugins.hades.renderers.types import (
        CitationSource,
        CitationType,
        Envelope,
        RetrievalLane,
    )

    citation = Envelope(
        id="c-defaultep",
        type=CitationType.KG_NODE,
        source=CitationSource.GITNEXUS_QUERY,
        retrieval_lane=RetrievalLane.SEMANTIC,
        audit_event_id="evt-defaultep",
        confidence=0.5,
        rrf_score=0.0,
        rrf_rank=0,
        project_id="p",
        payload="payload",
    )
                                  
    r = StubRenderer()
    assert r._audit_endpoint == DEFAULT_AUDIT_ENDPOINT  # noqa: SLF001

    with patch("httpx.Client") as mock_client_cls:
        instance = mock_client_cls.return_value.__enter__.return_value
        resp = instance.post.return_value
        resp.json.return_value = {
            "id": "evt-emitted-default",
            "accepted": True,
            "emitted_at": 1715856000,
        }
        resp.raise_for_status.return_value = None
        out = r.audit_anchor(citation, doctrine="default")
                                                    
        assert instance.post.call_args.args[0] == DEFAULT_AUDIT_ENDPOINT
    assert out == "evt-emitted-default"

                                                      
    r2 = StubRenderer(daemon_url="http://10.0.0.42:8080")
    assert r2._audit_endpoint == "http://10.0.0.42:8080/v1/audit/emit"  # noqa: SLF001
    with patch("httpx.Client") as mock_client_cls:
        instance = mock_client_cls.return_value.__enter__.return_value
        resp = instance.post.return_value
        resp.json.return_value = {
            "id": "evt-emitted-override",
            "accepted": True,
            "emitted_at": 1715856001,
        }
        resp.raise_for_status.return_value = None
        out2 = r2.audit_anchor(citation, doctrine="max-scope")
        assert instance.post.call_args.args[0] == "http://10.0.0.42:8080/v1/audit/emit"
    assert out2 == "evt-emitted-override"


def test_register_default_renderers_threads_daemon_url_through_concrete_renderers() -> (
    None
):
    """``register_default_renderers(reg, daemon_url=...)`` propagates URL to all 6 renderers.

    Validates the end-to-end wiring contract: a plugin loading
    ``register(ctx)`` may pass the operator-configured daemon URL into
    ``register_default_renderers`` once, and every concrete renderer
    (Ink/Telegram/Slack/Email/Voice/Web) carries the resulting
    ``self._audit_endpoint`` so subsequent ``audit_anchor`` calls hit
    the correct host.
    """
    from hermes_plugins.hades.renderers import (
        register_default_renderers,
    )

    reg = RendererRegistry()
    register_default_renderers(reg, daemon_url="http://daemon.test.internal:9090")
    for platform, renderer in reg._renderers.items():  # noqa: SLF001
        assert (
            renderer._audit_endpoint  # noqa: SLF001
            == "http://daemon.test.internal:9090/v1/audit/emit"
        ), f"{platform.value}: daemon_url not threaded through"


def test_no_legacy_7345_references_in_renderers_module() -> None:
    """Regression guard against re-introduction of the pre-fix-cycle port.

    Pre-fix-cycle, the default audit_endpoint was
    ``http://localhost:7345/v1/audit/anchor``. Production daemon listens on
    4471 (see cmd/zen-mcp-{budget,audit}/main.go). This test prevents the
    7345 port from sneaking back in via a copy-paste or a stale doc.
    """
    import inspect

    from hermes_plugins.hades.renderers import (
        Renderer as RendererClass,
    )

    src = inspect.getsource(inspect.getmodule(RendererClass))
    assert "7345" not in src, (
        "Legacy 7345 port resurfaced in plugin/zen-swarm/renderers/__init__.py; "
        "the canonical daemon TCP port is 4471 — see "
        "cmd/zen-mcp-budget/main.go:15 + cmd/zen-mcp-audit/main.go:16."
    )


                                                                             
                                                                        
                                                                             


def test_register_default_renderers_callable_exists() -> None:
    """register_default_renderers must be exported by the renderers package."""
    from hermes_plugins.hades.renderers import register_default_renderers

    assert callable(register_default_renderers)
