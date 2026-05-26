# SPDX-License-Identifier: MIT
"""Integration: plugin ``__init__.py register(ctx)`` wires the zen-internal renderer registry."""

from __future__ import annotations

import importlib
from datetime import datetime, timezone
from unittest.mock import patch

from hermes_plugins.hades.renderers import (
    Renderer,
    RendererRegistry,
    register_default_renderers,
)
from hermes_plugins.hades.renderers.types import (
    AugmentationResult,
    Platform,
    RenderResult,
)


def test_register_default_renderers_populates_all_six_platforms() -> None:
    """Calling register_default_renderers on empty registry → 6 platform slots filled."""
    reg = RendererRegistry()
    register_default_renderers(reg)
    expected_platforms = {
        Platform.INK,
        Platform.TELEGRAM,
        Platform.SLACK,
        Platform.EMAIL,
        Platform.VOICE,
        Platform.WEB,
    }
    assert set(reg._renderers.keys()) == expected_platforms  # noqa: SLF001


def test_register_default_renderers_registers_concrete_subclasses() -> None:
    """Each registered renderer is a Renderer subclass instance, not the ABC itself."""
    reg = RendererRegistry()
    register_default_renderers(reg)
    for platform, renderer in reg._renderers.items():  # noqa: SLF001
        assert isinstance(renderer, Renderer)
        assert platform == renderer.PLATFORM
        assert type(renderer) is not Renderer               


def test_register_default_renderers_idempotent() -> None:
    """Calling register_default_renderers twice replaces (last-wins per platform)."""
    reg = RendererRegistry()
    register_default_renderers(reg)
    first_ink = reg._renderers[Platform.INK]  # noqa: SLF001
    register_default_renderers(reg)
    second_ink = reg._renderers[Platform.INK]  # noqa: SLF001
                                          
    assert first_ink is not second_ink
                
    assert type(first_ink) is type(second_ink)


def test_zen_internal_registry_dispatch_simulation(
    sample_augmentation_result: AugmentationResult,
) -> None:
    """Simulate Phase H' register(ctx) wiring zen-internal RendererRegistry.

    Sequence (in production):
    1. Hermes loads ``plugin/zen-swarm/__init__.py`` and calls its
       ``register(ctx)``.
    2. ``register(ctx)`` calls ``register_default_renderers(_RENDERER_REGISTRY)``.
    3. zen-side callbacks (e.g., ``hooks/llm_handlers.py pre_llm_call``)
       call ``get_renderer_registry()`` and dispatch envelopes for the
       operator's active platform.

    This test simulates steps 2+3 with mocked audit anchor.
    """
    zen_internal_registry = RendererRegistry()
    register_default_renderers(zen_internal_registry)

    with patch(
        "hermes_plugins.hades.renderers.Renderer.audit_anchor",
        return_value="evt-mocked",
    ):
        result = zen_internal_registry.dispatch(sample_augmentation_result, Platform.INK)
    assert result.platform == Platform.INK
    assert result.output
    assert len(result.audit_event_ids) == len(sample_augmentation_result.citations)


def test_failure_isolation_bad_renderer_does_not_abort_others() -> None:
    """If a concrete renderer raises during render, others remain functional via fallback."""

    class BadRenderer(Renderer):
        PLATFORM = Platform.SLACK

        def render(self, result: AugmentationResult) -> RenderResult:
            raise RuntimeError("intentional fault")

    reg = RendererRegistry()
    register_default_renderers(reg)
    reg.register(BadRenderer())                                           

    wrapper = AugmentationResult(
        request_id="r",
        session_id="s",
        doctrine="default",
        project_id="p",
        citations=[],
        emitted_at=datetime.now(timezone.utc),
        kg_token_count=0,
        cache_key_hash="sha256:0",
        audit_event_id="evt-aug-bad",
    )
                                           
    result = reg.dispatch(wrapper, Platform.SLACK)
    assert result.platform == Platform.MARKDOWN_FALLBACK

                                      
    with patch(
        "hermes_plugins.hades.renderers.Renderer.audit_anchor",
        return_value="evt-mocked",
    ):
        ink_result = reg.dispatch(wrapper, Platform.INK)
    assert ink_result.platform == Platform.INK


def test_register_default_renderers_is_importable_from_plugin_root() -> None:
    """Plugin __init__.py register(ctx) import path resolves cleanly."""
    mod = importlib.import_module("hermes_plugins.hades.renderers")
    fn = mod.register_default_renderers
    assert callable(fn)


def test_get_renderer_registry_accessor_exposed_by_plugin_init() -> None:
    """Plugin's __init__.py exports get_renderer_registry() accessor.

    Zen-side callbacks (hooks/llm_handlers.py, AFK code, slash command
    handlers) consume the registry via this accessor.
    """
    plugin_mod = importlib.import_module("hermes_plugins.hades")
    accessor = getattr(plugin_mod, "get_renderer_registry", None)
    assert accessor is not None, "register(ctx) wiring missing accessor"
    assert callable(accessor)
                                                                        
                                                                      
                                                                        
                              
    registry = accessor()
    assert isinstance(registry, RendererRegistry)


def test_plugin_register_function_populates_module_global_registry() -> None:
    """Calling plugin.register(fake_ctx) populates the module-global registry.

    Simulates the Hermes loader's call into register(ctx); verifies the 6
    default platforms get registered into the module-global registry.
    """
    from collections.abc import Callable
    from dataclasses import dataclass, field
    from typing import Any

    plugin_mod = importlib.import_module("hermes_plugins.hades")

    @dataclass
    class _FakeCtx:
        hooks: list[tuple[str, Callable[..., Any]]] = field(default_factory=list)
        skills: list[tuple[str, Any, str]] = field(default_factory=list)
        commands: list[tuple[str, Any, str]] = field(default_factory=list)

        def register_hook(self, name: str, callback: Callable[..., Any]) -> None:
            self.hooks.append((name, callback))

        def register_skill(self, name: str, path: Any, description: str = "") -> None:
            self.skills.append((name, path, description))

        def register_command(
            self,
            name: str,
            handler: Callable[..., Any],
            description: str = "",
            args_hint: str = "",
        ) -> None:
            self.commands.append((name, handler, description))

    ctx = _FakeCtx()
    plugin_mod.register(ctx)
                                                           
    registry = plugin_mod.get_renderer_registry()
    expected = {
        Platform.INK,
        Platform.TELEGRAM,
        Platform.SLACK,
        Platform.EMAIL,
        Platform.VOICE,
        Platform.WEB,
    }
    assert expected.issubset(set(registry._renderers.keys()))  # noqa: SLF001
                                                                         
                                                                          
                                                                   
    assert len(ctx.hooks) >= 5
                                                         
    assert len(ctx.skills) >= 3
                                                                                    
    assert len(ctx.commands) >= 3


def test_dispatch_metadata_includes_request_session_doctrine(
    sample_augmentation_result: AugmentationResult,
) -> None:
    """End-to-end: AugmentationResult → INK renderer → metadata has wrapper provenance."""
    reg = RendererRegistry()
    register_default_renderers(reg)
    with patch(
        "hermes_plugins.hades.renderers.Renderer.audit_anchor",
        return_value="evt-mocked",
    ):
        result = reg.dispatch(sample_augmentation_result, Platform.INK)
    assert result.metadata["request_id"] == sample_augmentation_result.request_id
    assert result.metadata["session_id"] == sample_augmentation_result.session_id
    assert result.metadata["doctrine"] == sample_augmentation_result.doctrine
