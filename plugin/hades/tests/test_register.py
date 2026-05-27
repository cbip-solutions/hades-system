# SPDX-License-Identifier: MIT
"""Tests for hades Hermes plugin register(ctx) entry point."""

from __future__ import annotations

import sys
from collections.abc import Callable
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any

                                                                              
PLUGIN_ROOT = Path(__file__).resolve().parent.parent


@dataclass
class FakeManifest:
    name: str = "hades"


@dataclass
class FakeCtx:
    """Records every register_* call for assertion."""

    manifest: FakeManifest = field(default_factory=FakeManifest)
    hooks: list[tuple[str, Callable[..., Any]]] = field(default_factory=list)
    skills: list[tuple[str, Path, str]] = field(default_factory=list)
    commands: list[tuple[str, Callable[..., Any], str, str]] = field(default_factory=list)
    cli_commands: list[tuple[str, str, Any, Any, str]] = field(default_factory=list)
    tools: list[str] = field(default_factory=list)

    def register_hook(self, hook_name: str, callback: Callable[..., Any]) -> None:
        self.hooks.append((hook_name, callback))

    def register_skill(self, name: str, path: Path, description: str = "") -> None:
        self.skills.append((name, path, description))

    def register_command(
        self,
        name: str,
        handler: Callable[..., Any],
        description: str = "",
        args_hint: str = "",
    ) -> None:
        self.commands.append((name, handler, description, args_hint))

    def register_cli_command(
        self, name: str, help: str, setup_fn, handler_fn=None, description: str = ""
    ) -> None:
        self.cli_commands.append((name, help, setup_fn, handler_fn, description))

    def register_tool(self, name: str, **_: Any) -> None:
        self.tools.append(name)


                                           
EXPECTED_HOOKS = {
    "on_session_start",
    "on_session_end",
    "pre_tool_call",
    "post_tool_call",
    "pre_llm_call",
}

EXPECTED_SKILLS = {"hades", "start", "handoff"}

EXPECTED_COMMANDS = {"hades:start", "hades:handoff", "hades:install-mcps"}


def _import_plugin_module():
    """Import the plugin package fresh for each test.

    Mirrors Hermes' own directory-plugin loader at
    ``hermes_cli/plugins.py:1030-1065``: creates a parent namespace package,
    builds a spec with ``submodule_search_locations``, sets ``__package__``
    + ``__path__`` so the plugin's relative imports (``from.hooks...``)
    resolve correctly. Each test gets a fresh module via cache eviction.
    """
    import importlib
    import importlib.util
    import types

    _NS_PARENT = "hermes_plugins"
    module_name = f"{_NS_PARENT}.zen_swarm_under_test"

                                                                               
    for key in [
        k for k in sys.modules if k == module_name or k.startswith(module_name + ".")
    ]:
        del sys.modules[key]

                                                                                
    if _NS_PARENT not in sys.modules:
        ns_pkg = types.ModuleType(_NS_PARENT)
        ns_pkg.__path__ = []
        ns_pkg.__package__ = _NS_PARENT
        sys.modules[_NS_PARENT] = ns_pkg

    spec = importlib.util.spec_from_file_location(
        module_name,
        PLUGIN_ROOT / "__init__.py",
        submodule_search_locations=[str(PLUGIN_ROOT)],
    )
    assert spec is not None and spec.loader is not None
    mod = importlib.util.module_from_spec(spec)
    mod.__package__ = module_name
    mod.__path__ = [str(PLUGIN_ROOT)]  # type: ignore[attr-defined]
    sys.modules[module_name] = mod
    spec.loader.exec_module(mod)
    return mod


def test_register_function_exists():
    mod = _import_plugin_module()
    assert hasattr(mod, "register"), "plugin must export register(ctx)"
    assert callable(mod.register), "register must be callable"


def test_register_wires_expected_hooks():
    mod = _import_plugin_module()
    ctx = FakeCtx()
    mod.register(ctx)
    wired = {name for name, _cb in ctx.hooks}
    missing = EXPECTED_HOOKS - wired
    extra = wired - EXPECTED_HOOKS
    assert not missing, f"missing expected hooks: {sorted(missing)}"
                                                                                    
    if extra:
        print(f"warning: register wired unexpected hooks {sorted(extra)}; verify intent")


def test_register_hook_callbacks_are_callable():
    mod = _import_plugin_module()
    ctx = FakeCtx()
    mod.register(ctx)
    for name, cb in ctx.hooks:
        assert callable(cb), f"hook callback for {name} not callable"


def test_register_wires_expected_skills():
    mod = _import_plugin_module()
    ctx = FakeCtx()
    mod.register(ctx)
    wired = {name for name, _path, _desc in ctx.skills}
    missing = EXPECTED_SKILLS - wired
    assert not missing, f"Phase H' baseline skills missing: {missing}; got {wired}"


def test_register_skills_have_existing_paths():
    mod = _import_plugin_module()
    ctx = FakeCtx()
    mod.register(ctx)
    for name, path, _desc in ctx.skills:
        assert Path(path).exists(), f"skill {name} path does not exist: {path}"


def test_register_wires_expected_slash_commands():
    mod = _import_plugin_module()
    ctx = FakeCtx()
    mod.register(ctx)
    wired = {name for name, _h, _d, _a in ctx.commands}
    missing = EXPECTED_COMMANDS - wired
    assert not missing, f"missing slash commands: {sorted(missing)}"


def test_register_command_handlers_are_callable():
    mod = _import_plugin_module()
    ctx = FakeCtx()
    mod.register(ctx)
    for name, handler, _desc, _hint in ctx.commands:
        assert callable(handler), f"slash command {name} handler not callable"


def test_register_idempotent_no_exceptions():
    """register(ctx) should be re-callable on a fresh ctx without raising."""
    mod = _import_plugin_module()
    ctx1 = FakeCtx()
    ctx2 = FakeCtx()
    mod.register(ctx1)
    mod.register(ctx2)
                           
    assert len(ctx1.hooks) == len(ctx2.hooks)
    assert len(ctx1.skills) == len(ctx2.skills)
    assert len(ctx1.commands) == len(ctx2.commands)


def test_status_provider_registration_is_capability_gated():
    """GAP 2 (ADR-0110): the status-bar provider is registered ONLY when ctx
    exposes ``register_status_provider``. FakeCtx does NOT have this method, so
    register() must not raise and must not call a non-existent method."""
    mod = _import_plugin_module()

                                                                                               
    ctx_without = FakeCtx()
    mod.register(ctx_without)                  
                                                                          
                                                                

                                                           
    calls: list[Any] = []

    @dataclass
    class FakeCtxWithSeam(FakeCtx):
        def register_status_provider(self, fn: Any) -> None:
            calls.append(fn)

    ctx_with = FakeCtxWithSeam()
    mod.register(ctx_with)
    assert len(calls) == 1, (
        f"register_status_provider should be called exactly once when the seam "
        f"is present; got {len(calls)} calls"
    )
