# SPDX-License-Identifier: MIT
"""Shared test harness for slash command handler registration (Plan 12 Phase B B-2..B-8)."""

from __future__ import annotations

import importlib
import sys
from collections.abc import Callable
from pathlib import Path
from typing import Any

import pytest

                                                                                   
                                                                           
                                                                          
                                                 
from hermes_plugins.hades.commands.amendment_ack import amendment_ack_handler
from hermes_plugins.hades.commands.amendment_deny import amendment_deny_handler
from hermes_plugins.hades.commands.amendment_show import amendment_show_handler
from hermes_plugins.hades.commands.audit_impact import audit_impact_handler
from hermes_plugins.hades.commands.brainstorm import brainstorm_handler
from hermes_plugins.hades.commands.doctrine import doctrine_handler
from hermes_plugins.hades.commands.execute_plan import execute_plan_handler
from hermes_plugins.hades.commands.full import full_handler
from hermes_plugins.hades.commands.impact_pre_merge import impact_pre_merge_handler
from hermes_plugins.hades.commands.knowledge_promote import knowledge_promote_handler
from hermes_plugins.hades.commands.knowledge_query import knowledge_query_handler
from hermes_plugins.hades.commands.openspec_apply import openspec_apply_handler
from hermes_plugins.hades.commands.openspec_archive import openspec_archive_handler
from hermes_plugins.hades.commands.openspec_propose import openspec_propose_handler
from hermes_plugins.hades.commands.openspec_resume import openspec_resume_handler
from hermes_plugins.hades.commands.voice import voice_handler
from hermes_plugins.hades.commands.write_plan import write_plan_handler

PLUGIN_ROOT = Path(__file__).resolve().parents[1]

                                                                            
_PLUGIN_MODULE_NAME = "hermes_plugins.hades"


class FakePluginContext:
    """Test double matching Hermes' PluginContext signature for register_*.

    Per spike §5, the methods we exercise:
      - register_hook(name, callback)
      - register_command(name, handler, description="", args_hint="")
      - register_skill(name, path, description="")

    We record every call; tests assert the recorded set.
    """

    def __init__(self) -> None:
        self.hooks: list[tuple] = []
        self.commands: dict[str, dict] = {}
        self.skills: dict[str, dict] = {}
                                                                                       
                                                                                              
        self.tools: list[tuple] = []
        self.cli_commands: list[tuple] = []

    def register_hook(self, name: str, callback: Callable[..., Any]) -> None:
        self.hooks.append((name, callback))

    def register_command(
        self,
        name: str,
        handler: Callable[[str], str | None],
        description: str = "",
        args_hint: str = "",
    ) -> None:
        self.commands[name] = {
            "handler": handler,
            "description": description,
            "args_hint": args_hint,
        }

    def register_skill(self, name: str, path: Any, description: str = "") -> None:
        self.skills[name] = {"path": str(path), "description": description}

    def register_tool(self, *args: Any, **kwargs: Any) -> None:
        self.tools.append((args, kwargs))

    def register_cli_command(self, *args: Any, **kwargs: Any) -> None:
        self.cli_commands.append((args, kwargs))


@pytest.fixture(scope="function")
def fake_ctx() -> FakePluginContext:
    return FakePluginContext()


@pytest.fixture(scope="function")
def loaded_plugin(fake_ctx: FakePluginContext) -> FakePluginContext:
    """Import plugin/zen-swarm/__init__.py and invoke register(ctx).

    Uses the hermes_plugins.hades module pre-loaded by conftest.py
    (mirrors Hermes' loader at hermes_cli/plugins.py:1030-1065).
    """
                                                       
    mod = sys.modules.get(_PLUGIN_MODULE_NAME)
    if mod is None:
                                                                                         
        mod = importlib.import_module(_PLUGIN_MODULE_NAME)
    register = mod.register
    register(fake_ctx)
    return fake_ctx


                                                                               


def test_start_command_registered(loaded_plugin: FakePluginContext) -> None:
    """Phase H' Task H'-7 wired start; Phase B renames to hades: namespace.

    After Phase B (Plan 18b) all commands are registered as "hades:<name>".
    """
    assert "hades:start" in loaded_plugin.commands, (
        "Phase B should have wired start as 'hades:start' via ctx.register_command(). "
        "Expected 'hades:start' in registered commands."
    )


def test_handoff_command_registered(loaded_plugin: FakePluginContext) -> None:
    """Phase H' Task H'-8 wired handoff; Phase B renames to hades: namespace."""
    assert "hades:handoff" in loaded_plugin.commands, (
        "Phase B should have wired handoff as 'hades:handoff' via ctx.register_command()"
    )


def test_start_handler_callable_and_returns_string(
    loaded_plugin: FakePluginContext,
) -> None:
    """Smoke: start handler invokes without raising and returns str or None."""
    entry = loaded_plugin.commands.get("hades:start")
    assert entry is not None, "start command not found (expected 'hades:start')"
    handler = entry["handler"]
    result = handler("")
    assert result is None or isinstance(result, str), (
        f"start handler must return str | None per spike §6; got {type(result)}"
    )


def test_handoff_handler_callable_and_returns_string(
    loaded_plugin: FakePluginContext,
) -> None:
    """Smoke: handoff handler invokes without raising and returns str or None."""
    entry = loaded_plugin.commands.get("hades:handoff")
    assert entry is not None, "handoff command not found (expected 'hades:handoff')"
    handler = entry["handler"]
    result = handler("")
    assert result is None or isinstance(result, str)


                                                                              

WORKFLOW_COMMANDS = ["brainstorm", "write-plan", "execute-plan"]


@pytest.mark.parametrize("name", WORKFLOW_COMMANDS)
def test_workflow_command_registered(name: str, loaded_plugin: FakePluginContext) -> None:
    """Each workflow command is registered via ctx.register_command() as hades:<name>."""
    assert f"hades:{name}" in loaded_plugin.commands, (
        f"/hades:{name} not registered; expected ctx.register_command('hades:{name}', ...) "
        f"call in plugin/hades/__init__.py register(ctx)"
    )


@pytest.mark.parametrize(
    "name,test_arg",
    [
        ("brainstorm", ""),
        ("write-plan", "docs/superpowers/specs/test-spec.md"),
        ("execute-plan", "docs/superpowers/plans/test-plan.md"),
    ],
)
def test_workflow_handler_returns_skill_load_prompt(
    name: str, test_arg: str, loaded_plugin: FakePluginContext
) -> None:
    """Workflow handlers return a prompt that invokes superpowers skill_load explicitly."""
    handler = loaded_plugin.commands[f"hades:{name}"]["handler"]
    output = handler(test_arg)
    assert output is not None, f"/hades:{name} handler must return a prompt string"
                                                                                        
    assert "skill_load" in output or "superpowers:" in output, (
        f"/hades:{name} handler output must invoke superpowers skill explicitly; "
        f"see inv-zen-015 + spec §1 Q9"
    )


                                                                              

DOCTRINE_COMMANDS = [
    "doctrine",
    "amendment-list",
    "amendment-show",
    "amendment-ack",
    "amendment-deny",
]


@pytest.mark.parametrize("name", DOCTRINE_COMMANDS)
def test_doctrine_command_registered(name: str, loaded_plugin: FakePluginContext) -> None:
    """Each doctrine/amendment command is registered via ctx.register_command() as hades:<name>."""
    assert f"hades:{name}" in loaded_plugin.commands, (
        f"/hades:{name} not registered via ctx.register_command() in __init__.py"
    )


def test_doctrine_handler_references_audit_chain(
    loaded_plugin: FakePluginContext,
) -> None:
    """/hades:doctrine override flow must reference Plan 9 audit chain + daemon endpoint."""
    handler = loaded_plugin.commands["hades:doctrine"]["handler"]
                              
    output = handler("max-scope")
    assert output is not None
    assert "audit" in output.lower(), "/doctrine override must mention audit chain"
    assert "doctrine/override" in output, "must reference daemon endpoint"


                                                                              

ZEN_KG_COMMANDS = ["impact-pre-merge", "audit-impact", "doctrine-drift-check"]


@pytest.mark.parametrize("name", ZEN_KG_COMMANDS)
def test_zen_kg_command_registered(name: str, loaded_plugin: FakePluginContext) -> None:
    """Each zen-KG command is registered via ctx.register_command() as hades:<name>."""
    assert f"hades:{name}" in loaded_plugin.commands, (
        f"/hades:{name} not registered via ctx.register_command() in __init__.py"
    )


@pytest.mark.parametrize("name", ZEN_KG_COMMANDS)
def test_zen_kg_handler_references_caronte_or_augment(
    name: str, loaded_plugin: FakePluginContext
) -> None:
    """zen KG handlers reference caronte code-graph or augmentation pipeline."""
    handler = loaded_plugin.commands[f"hades:{name}"]["handler"]
    output = handler("test-branch")
    assert output is not None
    body = output.lower()
    assert "caronte" in body or "augment" in body or "v1/" in body, (
        f"/{name} handler must reference Plan 11 augmentation or caronte code-graph"
    )


                                                                              

KNOWLEDGE_COMMANDS = ["knowledge-query", "knowledge-promote"]


@pytest.mark.parametrize("name", KNOWLEDGE_COMMANDS)
def test_knowledge_command_registered(
    name: str, loaded_plugin: FakePluginContext
) -> None:
    """Each knowledge command is registered via ctx.register_command() as hades:<name>."""
    assert f"hades:{name}" in loaded_plugin.commands, (
        f"/hades:{name} not registered via ctx.register_command() in __init__.py"
    )


def test_knowledge_query_handler_references_aggregator(
    loaded_plugin: FakePluginContext,
) -> None:
    """/hades:knowledge-query output must reference Plan 9 D aggregator + privacy."""
    handler = loaded_plugin.commands["hades:knowledge-query"]["handler"]
    output = handler("test query")
    assert output is not None
    body = output.lower()
    assert "aggregator" in body or "knowledge" in body
    assert "privacy" in body, "must mention cross-project privacy boundary"


def test_knowledge_promote_references_audit(loaded_plugin: FakePluginContext) -> None:
    """/hades:knowledge-promote output must reference audit chain."""
    handler = loaded_plugin.commands["hades:knowledge-promote"]["handler"]
    output = handler("item-123 reason=test")
    assert output is not None
    assert "audit" in output.lower(), "promote-to-global must mention audit"


                                                                              

AFK_COMMANDS = ["full", "voice"]


@pytest.mark.parametrize("name", AFK_COMMANDS)
def test_afk_command_registered(name: str, loaded_plugin: FakePluginContext) -> None:
    """Each AFK command is registered via ctx.register_command() as hades:<name>."""
    assert f"hades:{name}" in loaded_plugin.commands, (
        f"/hades:{name} not registered via ctx.register_command() in __init__.py"
    )


def test_full_handler_references_citation(loaded_plugin: FakePluginContext) -> None:
    """/hades:full output must reference citation expansion."""
    handler = loaded_plugin.commands["hades:full"]["handler"]
    output = handler("c1")
    assert output is not None
    assert "citation" in output.lower() or "platform" in output.lower()


def test_voice_handler_references_sync_async(loaded_plugin: FakePluginContext) -> None:
    """/hades:voice output must describe sync/async flow."""
    handler = loaded_plugin.commands["hades:voice"]["handler"]
    output = handler("")
    assert output is not None
    body = output.lower()
    assert "sync" in body or "async" in body, "must describe sync/async flow"


                                                                              

OPENSPEC_COMMANDS = [
    "openspec-apply",
    "openspec-archive",
    "openspec-propose",
    "openspec-resume",
]


@pytest.mark.parametrize("name", OPENSPEC_COMMANDS)
def test_openspec_command_registered(name: str, loaded_plugin: FakePluginContext) -> None:
    """Each openspec command is registered via ctx.register_command() as hades:<name>."""
    assert f"hades:{name}" in loaded_plugin.commands, (
        f"/hades:{name} not registered via ctx.register_command() in __init__.py"
    )


def test_openspec_propose_loads_brainstorming(loaded_plugin: FakePluginContext) -> None:
    """openspec-propose must explicitly invoke brainstorming skill per inv-zen-015."""
    handler = loaded_plugin.commands["hades:openspec-propose"]["handler"]
    output = handler("my-feature")
    assert output is not None
    assert "skill_load" in output or "superpowers:brainstorming" in output, (
        "openspec-propose must explicitly invoke brainstorming skill per inv-zen-015"
    )


def test_openspec_archive_references_inv_zen_004(
    loaded_plugin: FakePluginContext,
) -> None:
    """openspec-archive must reference inv-zen-004 (no Claude attribution)."""
    handler = loaded_plugin.commands["hades:openspec-archive"]["handler"]
    output = handler("my-feature")
    assert output is not None
    assert "inv-zen-004" in output, "openspec-archive must reference inv-zen-004"


                                                                                
 
                                                                   
                                                                          
                                                                              
                                                                       
 
                                                                  
                                                                        
                                                                             
                                                                        
                                                                             


                                                                              


@pytest.mark.parametrize("bad_input", ["", "   ", "\t"])
def test_amendment_ack_no_id_returns_error(bad_input: str) -> None:
    """amendment-ack with empty/whitespace input returns the _PROMPT_NO_ID error prompt."""
    result = amendment_ack_handler(bad_input)
    assert result is not None
    assert "id is required" in result
    assert "amendment-ack" in result


def test_amendment_ack_with_id_only_returns_happy_path() -> None:
    """amendment-ack with id-only (no reason) uses default reason and returns main prompt."""
    result = amendment_ack_handler("amend-2026-05-10-0001")
    assert result is not None
    assert "amend-2026-05-10-0001" in result
    assert "audit" in result.lower()


def test_amendment_ack_with_id_and_reason_interpolates_both() -> None:
    """amendment-ack with id + reason interpolates both into the prompt."""
    result = amendment_ack_handler("amend-xyz because approved")
    assert result is not None
    assert "amend-xyz" in result
    assert "because approved" in result


                                                                              


@pytest.mark.parametrize("bad_input", ["", "   "])
def test_amendment_deny_no_id_returns_error(bad_input: str) -> None:
    """amendment-deny with empty input returns the _PROMPT_NO_ID error prompt."""
    result = amendment_deny_handler(bad_input)
    assert result is not None
    assert "id and reason are required" in result


def test_amendment_deny_id_but_no_reason_returns_error() -> None:
    """amendment-deny with id but no reason returns _PROMPT_NO_REASON."""
    result = amendment_deny_handler("amend-2026-05-10-0002")
    assert result is not None
    assert "reason is required" in result


def test_amendment_deny_id_whitespace_only_reason_returns_error() -> None:
    """amendment-deny with id + whitespace reason (strips to empty) returns _PROMPT_NO_REASON."""
    result = amendment_deny_handler("amend-2026-05-10-0002   ")
    assert result is not None
    assert "reason is required" in result


def test_amendment_deny_with_id_and_reason_returns_happy_path() -> None:
    """amendment-deny with id + reason interpolates both into the prompt."""
    result = amendment_deny_handler("amend-abc out of scope")
    assert result is not None
    assert "amend-abc" in result
    assert "out of scope" in result
    assert "audit" in result.lower()


                                                                              


@pytest.mark.parametrize("bad_input", ["", "   "])
def test_amendment_show_no_id_returns_error(bad_input: str) -> None:
    """amendment-show with empty/whitespace input returns the _PROMPT_NO_ID error prompt."""
    result = amendment_show_handler(bad_input)
    assert result is not None
    assert "id is required" in result
    assert "amendment-show" in result


def test_amendment_show_with_id_returns_happy_path() -> None:
    """amendment-show with id interpolates it into the prompt."""
    result = amendment_show_handler("amend-2026-05-10-0003")
    assert result is not None
    assert "amend-2026-05-10-0003" in result


                                                                              


@pytest.mark.parametrize("bad_input", ["", "   "])
def test_openspec_apply_no_feature_returns_error(bad_input: str) -> None:
    """openspec-apply with empty/whitespace input returns the Q6-format _PROMPT_NO_FEATURE error prompt."""
    result = openspec_apply_handler(bad_input)
    assert result is not None
    assert "HADES: feature name required" in result
    assert "openspec-apply" in result


def test_openspec_apply_with_feature_returns_happy_path() -> None:
    """openspec-apply with feature name interpolates it into the prompt."""
    result = openspec_apply_handler("my-feature")
    assert result is not None
    assert "my-feature" in result
    assert "tasks.md" in result


                                                                              


@pytest.mark.parametrize("bad_input", ["", "   "])
def test_openspec_resume_no_feature_returns_error(bad_input: str) -> None:
    """openspec-resume with empty/whitespace input returns the Q6-format _PROMPT_NO_FEATURE error prompt."""
    result = openspec_resume_handler(bad_input)
    assert result is not None
    assert "HADES: feature name required" in result
    assert "openspec-resume" in result


def test_openspec_resume_with_feature_returns_happy_path() -> None:
    """openspec-resume with feature name interpolates it into the prompt."""
    result = openspec_resume_handler("my-feature")
    assert result is not None
    assert "my-feature" in result
    assert "phase" in result.lower()


                                                                              


@pytest.mark.parametrize("bad_input", ["", "   "])
def test_openspec_archive_no_feature_returns_error(bad_input: str) -> None:
    """openspec-archive with empty/whitespace input returns the Q6-format _PROMPT_NO_FEATURE error prompt."""
    result = openspec_archive_handler(bad_input)
    assert result is not None
    assert "HADES: feature name required" in result
    assert "openspec-archive" in result


def test_openspec_archive_with_feature_returns_happy_path() -> None:
    """openspec-archive with feature name interpolates it + references inv-zen-004."""
    result = openspec_archive_handler("my-feature")
    assert result is not None
    assert "my-feature" in result
    assert "inv-zen-004" in result


                                                                              


@pytest.mark.parametrize("bad_input", ["", "   "])
def test_openspec_propose_no_feature_returns_error(bad_input: str) -> None:
    """openspec-propose with empty/whitespace input returns the Q6-format _PROMPT_NO_FEATURE error prompt."""
    result = openspec_propose_handler(bad_input)
    assert result is not None
    assert "HADES: feature name required" in result
    assert "openspec-propose" in result


def test_openspec_propose_with_feature_returns_happy_path() -> None:
    """openspec-propose with feature name must load brainstorming skill (inv-zen-015)."""
    result = openspec_propose_handler("my-feature")
    assert result is not None
    assert "my-feature" in result
    assert "skill_load" in result or "superpowers:brainstorming" in result


                                                                              


@pytest.mark.parametrize("bad_input", ["", "   "])
def test_audit_impact_no_id_returns_error(bad_input: str) -> None:
    """audit-impact with empty/whitespace input returns the _PROMPT_NO_ID error prompt."""
    result = audit_impact_handler(bad_input)
    assert result is not None
    assert "event id is required" in result


def test_audit_impact_with_id_returns_happy_path() -> None:
    """audit-impact with event id interpolates it and references augment pipeline."""
    result = audit_impact_handler("evt-abc123")
    assert result is not None
    assert "evt-abc123" in result
    assert "augment" in result.lower() or "v1/augment" in result


                                                                              


@pytest.mark.parametrize("bad_input", ["", "   "])
def test_full_no_id_returns_error(bad_input: str) -> None:
    """/full with empty/whitespace input returns the _PROMPT_NO_ID error prompt."""
    result = full_handler(bad_input)
    assert result is not None
    assert "citation id is required" in result


def test_full_with_id_returns_happy_path() -> None:
    """/full with citation id interpolates it and mentions platform rendering."""
    result = full_handler("c1")
    assert result is not None
    assert "c1" in result
    assert "citation" in result.lower() or "platform" in result.lower()


                                                                              


@pytest.mark.parametrize("bad_input", ["", "   "])
def test_impact_pre_merge_no_branch_returns_error(bad_input: str) -> None:
    """impact-pre-merge with empty/whitespace input returns the _PROMPT_NO_BRANCH error."""
    result = impact_pre_merge_handler(bad_input)
    assert result is not None
    assert "branch name is required" in result


def test_impact_pre_merge_with_branch_returns_happy_path() -> None:
    """impact-pre-merge with branch name interpolates it and references augment/caronte."""
    result = impact_pre_merge_handler("feature/my-feature")
    assert result is not None
    assert "feature/my-feature" in result
    assert "augment" in result.lower() or "caronte" in result.lower()


                                                                              


@pytest.mark.parametrize("bad_input", ["", "   "])
def test_knowledge_query_no_pattern_returns_error(bad_input: str) -> None:
    """knowledge-query with empty/whitespace input returns the _PROMPT_NO_PATTERN error."""
    result = knowledge_query_handler(bad_input)
    assert result is not None
    assert "pattern is required" in result


def test_knowledge_query_with_pattern_uses_default_scope() -> None:
    """knowledge-query with pattern-only uses default scope and returns happy path."""
    result = knowledge_query_handler("doctrine max_kg_tokens")
    assert result is not None
    assert "doctrine max_kg_tokens" in result or "doctrine" in result
    assert "privacy" in result.lower()


def test_knowledge_query_with_pattern_and_scope() -> None:
    """knowledge-query with pattern + scope interpolates both."""
    result = knowledge_query_handler("doctrine max-scope")
    assert result is not None
    assert "aggregator" in result.lower() or "knowledge" in result.lower()


                                                                              


@pytest.mark.parametrize("bad_input", ["", "   "])
def test_knowledge_promote_no_id_returns_error(bad_input: str) -> None:
    """knowledge-promote with empty/whitespace input returns the _PROMPT_NO_ID error."""
    result = knowledge_promote_handler(bad_input)
    assert result is not None
    assert "item id is required" in result


def test_knowledge_promote_id_but_no_reason_returns_error() -> None:
    """knowledge-promote with id but no reason returns _PROMPT_NO_REASON."""
    result = knowledge_promote_handler("item-123")
    assert result is not None
    assert "reason is required" in result


def test_knowledge_promote_id_whitespace_reason_returns_error() -> None:
    """knowledge-promote with id + whitespace-only reason returns _PROMPT_NO_REASON."""
    result = knowledge_promote_handler("item-123   ")
    assert result is not None
    assert "reason is required" in result


def test_knowledge_promote_with_id_and_reason_returns_happy_path() -> None:
    """knowledge-promote with id + reason interpolates both and mentions audit."""
    result = knowledge_promote_handler("item-123 supersedes old item")
    assert result is not None
    assert "item-123" in result
    assert "supersedes old item" in result
    assert "audit" in result.lower()


                                                                              


@pytest.mark.parametrize(
    "invalid_name",
    [
        "invalid-doctrine",
        "MaxScope",
        "CAPA-FIREWALL",
        "unknown",
        "max_scope",                                
    ],
)
def test_doctrine_invalid_name_returns_error(invalid_name: str) -> None:
    """/doctrine with an invalid doctrine name returns an error string (not the override prompt)."""
    result = doctrine_handler(invalid_name)
    assert result is not None
    assert "invalid doctrine name" in result
    assert "max-scope" in result                              
    assert "capa-firewall" in result


def test_doctrine_empty_returns_show_prompt() -> None:
    """/doctrine with empty arg returns the _SHOW_PROMPT (show mode)."""
    result = doctrine_handler("")
    assert result is not None
    assert "Show active doctrine" in result or "show" in result.lower()


@pytest.mark.parametrize("valid_name", ["max-scope", "default", "capa-firewall"])
def test_doctrine_valid_name_returns_override_prompt(valid_name: str) -> None:
    """/doctrine with a valid name returns the _OVERRIDE_PROMPT with name interpolated."""
    result = doctrine_handler(valid_name)
    assert result is not None
    assert valid_name in result
    assert "audit" in result.lower()
    assert "doctrine/override" in result


                                                                              


@pytest.mark.parametrize("bad_input", ["", "   "])
def test_execute_plan_no_path_returns_error(bad_input: str) -> None:
    """execute-plan with empty/whitespace input returns an error string."""
    result = execute_plan_handler(bad_input)
    assert result is not None
    assert "execute-plan" in result
    assert "plan_path" in result or "path" in result.lower()


def test_execute_plan_with_path_returns_happy_path() -> None:
    """execute-plan with a plan path interpolates it into the prompt."""
    result = execute_plan_handler("docs/superpowers/plans/2026-05-16-plan-12-phase-b.md")
    assert result is not None
    assert "2026-05-16-plan-12-phase-b.md" in result
    assert "skill_load" in result or "superpowers:" in result


                                                                              


@pytest.mark.parametrize("bad_input", ["", "   "])
def test_write_plan_no_path_returns_error(bad_input: str) -> None:
    """write-plan with empty/whitespace input returns an error string."""
    result = write_plan_handler(bad_input)
    assert result is not None
    assert "write-plan" in result
    assert "spec_path" in result or "path" in result.lower()


def test_write_plan_with_path_returns_happy_path() -> None:
    """write-plan with a spec path interpolates it into the prompt."""
    result = write_plan_handler("docs/superpowers/specs/2026-05-16-zen-swarm-design.md")
    assert result is not None
    assert "2026-05-16-zen-swarm-design.md" in result
    assert "skill_load" in result or "superpowers:" in result


                                                                              


def test_brainstorm_empty_returns_no_seed_prompt() -> None:
    """/brainstorm with empty arg returns a prompt with 'No topic seed' section."""
    result = brainstorm_handler("")
    assert result is not None
    assert isinstance(result, str)
    assert "No topic seed" in result
    assert "skill_load" in result or "superpowers:brainstorming" in result


def test_brainstorm_with_topic_returns_seeded_prompt() -> None:
    """/brainstorm with topic arg returns a prompt with 'Topic seed' section."""
    result = brainstorm_handler("knowledge graph indexing")
    assert result is not None
    assert isinstance(result, str)
    assert "knowledge graph indexing" in result
    assert "skill_load" in result or "superpowers:brainstorming" in result


                                                                              


def test_voice_empty_uses_stt_placeholder() -> None:
    """/voice with empty arg substitutes '(voice STT — operator speaks)' as query."""
    result = voice_handler("")
    assert result is not None
    assert "voice STT" in result or "sync" in result.lower() or "async" in result.lower()


def test_voice_with_query_interpolates_it() -> None:
    """/voice with a text query interpolates it into the prompt."""
    result = voice_handler("what is the current doctrine?")
    assert result is not None
    assert "what is the current doctrine?" in result
