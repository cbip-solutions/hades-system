# SPDX-License-Identifier: MIT
"""register(ctx) runtime compliance test —   extensions."""

from __future__ import annotations

                                                                          
                                                                                              
import importlib.util as _util
import inspect
import pathlib as _pathlib
import sys

_THIS_DIR = _pathlib.Path(__file__).parent
_CMD_HANDLERS_PATH = _THIS_DIR / "test_command_handlers.py"
_spec = _util.spec_from_file_location("test_command_handlers", _CMD_HANDLERS_PATH)
_cmd_handlers_mod = _util.module_from_spec(_spec)  # type: ignore[arg-type]
_spec.loader.exec_module(_cmd_handlers_mod)  # type: ignore[union-attr]
FakePluginContext = _cmd_handlers_mod.FakePluginContext


                                                           
                                                                               
PHASE_H_PRIME_COMMANDS = {
    "hades:start",
    "hades:handoff",
    "hades:install-mcps",
}
PHASE_H_PRIME_SKILLS = {"hades", "start", "handoff"}
PHASE_H_PRIME_HOOKS = {
    "on_session_start",
    "on_session_end",
    "pre_tool_call",
    "post_tool_call",
    "pre_llm_call",
}

PLAN_12_NEW_COMMANDS = {
                   
    "hades:brainstorm",
    "hades:write-plan",
    "hades:execute-plan",
                               
    "hades:doctrine",
    "hades:amendment-list",
    "hades:amendment-show",
    "hades:amendment-ack",
    "hades:amendment-deny",
                 
    "hades:impact-pre-merge",
    "hades:audit-impact",
    "hades:doctrine-drift-check",
                    
    "hades:knowledge-query",
    "hades:knowledge-promote",
              
    "hades:full",
    "hades:voice",
                        
    "hades:openspec-apply",
    "hades:openspec-archive",
    "hades:openspec-propose",
    "hades:openspec-resume",
}
assert len(PLAN_12_NEW_COMMANDS) == 19, (
    "Plan 12 Phase B adds 19 NEW commands (B-3..B-8); update count if scope changes"
)

                                                              
PLAN_18C_C_NEW_COMMANDS = {
    "hades:status",
}

PLAN_12_NEW_SKILLS = {
    "brainstorm",
    "write-plan",
    "execute-plan",
    "doctrine",
    "amendment",                                               
    "impact-pre-merge",
    "audit-impact",
    "doctrine-drift-check",
    "knowledge-query",
    "knowledge-promote",
}
assert len(PLAN_12_NEW_SKILLS) == 10

_PLUGIN_MODULE_NAME = "hermes_plugins.hades"


def _get_module():
    """Get the pre-loaded plugin module from conftest."""
    mod = sys.modules.get(_PLUGIN_MODULE_NAME)
    if mod is None:
        raise RuntimeError(
            f"Plugin module {_PLUGIN_MODULE_NAME!r} not in sys.modules; "
            "conftest.py should have pre-loaded it via _preload_plugin_as_hermes_does()"
        )
    return mod


def test_register_ctx_does_not_raise() -> None:
    """register(ctx) executes without raising — load-bearing per spike §5."""
    fake = FakePluginContext()
    mod = _get_module()
    mod.register(fake)
                                                


def test_register_ctx_wires_phase_h_prime_baseline() -> None:
    """ baseline (Task H'-2/H'-7/H'-8/H'-10) registrations preserved."""
    fake = FakePluginContext()
    mod = _get_module()
    mod.register(fake)
    for name in PHASE_H_PRIME_COMMANDS:
        assert name in fake.commands, (
            f"Phase H' baseline command /{name} missing — regression"
        )
    for name in PHASE_H_PRIME_SKILLS:
        assert name in fake.skills, f"Phase H' baseline skill {name} missing — regression"


def test_register_ctx_wires_plan_12_new_commands() -> None:
    """All 19 NEW   commands registered."""
    fake = FakePluginContext()
    mod = _get_module()
    mod.register(fake)
    missing = PLAN_12_NEW_COMMANDS - set(fake.commands.keys())
    assert not missing, (
        f"Plan 12 Phase B commands not registered: {missing}; "
        f"check __init__.py register(ctx) for missing ctx.register_command() calls"
    )


def test_register_ctx_wires_plan_12_new_skills() -> None:
    """All 10 NEW   skills registered."""
    fake = FakePluginContext()
    mod = _get_module()
    mod.register(fake)
    missing = PLAN_12_NEW_SKILLS - set(fake.skills.keys())
    assert not missing, (
        f"Plan 12 Phase B skills not registered: {missing}; "
        f"check __init__.py register(ctx) for missing ctx.register_skill() calls"
    )


def test_register_ctx_total_command_count() -> None:
    """Total registered commands =  baseline (3) +  NEW (19) +   (1) +   (2) = 25."""
                                                                  
    PLAN_18C_D_COMMANDS = {"hades:dashboard", "hades:panel"}
    fake = FakePluginContext()
    mod = _get_module()
    mod.register(fake)
    expected = (
        PHASE_H_PRIME_COMMANDS
        | PLAN_12_NEW_COMMANDS
        | PLAN_18C_C_NEW_COMMANDS
        | PLAN_18C_D_COMMANDS
    )
    actual = set(fake.commands.keys())
    extra = actual - expected
    assert not extra, (
        f"Unexpected commands registered: {extra} — verify Plan 18c Phase C+D scope"
    )
    assert actual == expected, f"Expected {expected}, got {actual}"


def test_register_ctx_hooks_in_valid_hooks() -> None:
    """Every registered hook name is in Hermes VALID_HOOKS (17 entries per spike §4)."""
    VALID_HOOKS = {
        "pre_tool_call",
        "post_tool_call",
        "transform_terminal_output",
        "transform_tool_result",
        "transform_llm_output",
        "pre_llm_call",
        "post_llm_call",
        "pre_api_request",
        "post_api_request",
        "on_session_start",
        "on_session_end",
        "on_session_finalize",
        "on_session_reset",
        "subagent_stop",
        "pre_gateway_dispatch",
        "pre_approval_request",
        "post_approval_response",
    }
    fake = FakePluginContext()
    mod = _get_module()
    mod.register(fake)
    for name, _callback in fake.hooks:
        assert name in VALID_HOOKS, (
            f"Hook name {name!r} not in Hermes VALID_HOOKS — fictional hook "
            f"(check verification report §3 for pre_completion / pre_tool_use)"
        )


def test_register_ctx_handler_signatures() -> None:
    """Every registered command handler is fn(raw_args: str) -> str | None per spike §6."""
    fake = FakePluginContext()
    mod = _get_module()
    mod.register(fake)
    for name, entry in fake.commands.items():
        handler = entry["handler"]
        sig = inspect.signature(handler)
        params = list(sig.parameters.values())
        assert len(params) == 1, (
            f"/{name} handler signature must be fn(raw_args); got {sig}"
        )
