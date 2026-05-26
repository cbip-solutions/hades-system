# SPDX-License-Identifier: MIT
"""Sentinel brand-pass tests for HADES rebrand of slash command handlers."""

from __future__ import annotations

import re
from collections.abc import Callable

import pytest

                                                    
                                                                      
from hermes_plugins.hades.commands.amendment_ack import amendment_ack_handler
from hermes_plugins.hades.commands.amendment_deny import amendment_deny_handler
from hermes_plugins.hades.commands.amendment_list import amendment_list_handler
from hermes_plugins.hades.commands.amendment_show import amendment_show_handler
from hermes_plugins.hades.commands.audit_impact import audit_impact_handler
from hermes_plugins.hades.commands.brainstorm import brainstorm_handler
from hermes_plugins.hades.commands.doctrine import doctrine_handler
from hermes_plugins.hades.commands.doctrine_drift_check import (
    doctrine_drift_check_handler,
)
from hermes_plugins.hades.commands.execute_plan import execute_plan_handler
from hermes_plugins.hades.commands.full import full_handler
from hermes_plugins.hades.commands.handoff import handle_handoff
from hermes_plugins.hades.commands.impact_pre_merge import impact_pre_merge_handler
from hermes_plugins.hades.commands.knowledge_promote import knowledge_promote_handler
from hermes_plugins.hades.commands.knowledge_query import knowledge_query_handler
from hermes_plugins.hades.commands.openspec_apply import openspec_apply_handler
from hermes_plugins.hades.commands.openspec_archive import openspec_archive_handler
from hermes_plugins.hades.commands.openspec_propose import openspec_propose_handler
from hermes_plugins.hades.commands.openspec_resume import openspec_resume_handler
from hermes_plugins.hades.commands.start import handle_start
from hermes_plugins.hades.commands.voice import voice_handler
from hermes_plugins.hades.commands.write_plan import write_plan_handler

                                                                              
                                                                           
                                                                            
                                                                          
                                                                   
                                                             
HANDLER_SUCCESS_CASES: list[tuple[Callable[[str], str | None], str, str]] = [
    (amendment_ack_handler, "amend-1234 testing-only", "amendment-ack"),
    (amendment_deny_handler, "amend-1234 testing-only", "amendment-deny"),
    (amendment_list_handler, "", "amendment-list"),
    (amendment_show_handler, "amend-1234", "amendment-show"),
    (audit_impact_handler, "evt-abc123", "audit-impact"),
    (brainstorm_handler, "testing topic", "brainstorm"),
    (doctrine_handler, "", "doctrine-show"),
    (doctrine_handler, "max-scope", "doctrine-override"),
    (doctrine_drift_check_handler, "", "doctrine-drift-check"),
    (execute_plan_handler, "Plan N", "execute-plan"),
    (full_handler, "c1", "full"),
    (handle_handoff, "session-end seed", "handoff"),
    (impact_pre_merge_handler, "PR-123", "impact-pre-merge"),
    (knowledge_promote_handler, "item-1 max-scope", "knowledge-promote"),
    (knowledge_query_handler, "doctrine pattern self", "knowledge-query"),
    (openspec_apply_handler, "feature-x", "openspec-apply"),
    (openspec_archive_handler, "feature-x", "openspec-archive"),
    (openspec_propose_handler, "feature-x", "openspec-propose"),
    (openspec_resume_handler, "feature-x", "openspec-resume"),
    (handle_start, "", "start"),
    (voice_handler, "test query", "voice"),
    (write_plan_handler, "Plan N feature x", "write-plan"),
]

                                                                            
HANDLER_ERROR_CASES: list[tuple[Callable[[str], str | None], str, str]] = [
    (amendment_ack_handler, "", "amendment-ack-error"),
    (amendment_deny_handler, "", "amendment-deny-error"),
    (amendment_show_handler, "", "amendment-show-error"),
    (audit_impact_handler, "", "audit-impact-error"),
    (doctrine_handler, "invalid-doctrine", "doctrine-error"),
    (full_handler, "", "full-error"),
    (impact_pre_merge_handler, "", "impact-pre-merge-error"),
    (knowledge_promote_handler, "", "knowledge-promote-error"),
    (knowledge_query_handler, "", "knowledge-query-error"),
    (openspec_apply_handler, "", "openspec-apply-error"),
    (openspec_archive_handler, "", "openspec-archive-error"),
    (openspec_propose_handler, "", "openspec-propose-error"),
    (openspec_resume_handler, "", "openspec-resume-error"),
]

                                                                  
                                                                       
                                                                       
HISTORICAL_ALLOWLIST: list[str] = [
    "github.com/cbip-solutions/hades-system",
    "(formerly zen-swarm)",
    "zen-swarm era",
    "Plan 12 era of zen-swarm",
    "zen-swarm-ctld",
    "/tmp/zen-swarm.sock",
    "~/.config/zen-swarm/",
    "$HERMES_HOME/plugins/model-providers/zen-swarm",
    "model-providers/zen-swarm",
    "model.provider: zen-swarm",
    ".zen-swarm.toml",
                                                                            
                                                                           
                                         
    'PROVIDER_NAME = "zen-swarm"',
    "zen://audit/",
                                                                        
                                                                       
    "<date>-zen-swarm-<topic>-design.md",
    "<date>-zen-swarm-<topic>-plan.md",
    "zen-swarm-<topic>-design.md",
    "zen-swarm-<topic>-plan.md",
    "2026-05-20-zen-swarm-plan-",
    "2026-05-20-zen-swarm-",
    "2026-04-30-zen-swarm-",
    "2026-04-29-zen-swarm-",
    "zen-swarm-plan-",
    "zen-swarm-design",
    "docs/superpowers/specs/",
    "docs/superpowers/plans/",
                                                                              
                                                                          
    "mcp_zen-swarm_caronte_query",
    "mcp_zen-swarm_caronte_context",
                                                                      
    "plugin/zen-swarm/providers",
    "PROVIDER_PLUGIN_SRC",
    "_constants.py",
                                                                              
                                                                        
                           
    "-path-to-projects-hades-system",
    "the-operator-projects-hades-system",
]


def _strip_allowlist(text: str) -> str:
    """Redact historical-allowlist substrings before brand-string scan.

    Replaces each allowlisted substring with a neutral sentinel that does
    NOT contain "zen-swarm" / "zen_swarm" / "ZenSwarm". This lets the
    brand-string assertion treat every remaining occurrence as a violation.
    """
    out = text
    for allowed in HISTORICAL_ALLOWLIST:
        out = out.replace(allowed, "<<ALLOWLIST_REDACTED>>")
    return out


_BRAND_FORBIDDEN_PATTERN = re.compile(
    r"zen[-_]swarm|ZenSwarm",
    flags=re.IGNORECASE,
)


def _scan_forbidden(text: str) -> list[str]:
    """Return list of forbidden brand-string matches (post-allowlist strip)."""
    stripped = _strip_allowlist(text)
    return _BRAND_FORBIDDEN_PATTERN.findall(stripped)


@pytest.mark.parametrize(
    "handler,raw_args,case_id",
    HANDLER_SUCCESS_CASES,
    ids=[case[2] for case in HANDLER_SUCCESS_CASES],
)
def test_handler_success_path_has_hades_branding(
    handler: Callable[[str], str | None],
    raw_args: str,
    case_id: str,
) -> None:
    """Phase E rebrand: every handler's main / success path output contains
    "HADES" (or "HADES system") and contains NO non-allowlisted "zen-swarm".

    RED on post-A+B + pre-E-3..E-10. GREEN after the appropriate E-3..E-10
    commit lands for the handler.
    """
    result = handler(raw_args)
    assert result is not None, f"{case_id}: handler returned None"
    assert isinstance(result, str), f"{case_id}: handler returned non-str: {type(result)}"

    assert "HADES" in result, (
        f"{case_id}: handler output missing HADES brand string; "
        "Phase E rebrand incomplete"
    )

    leaks = _scan_forbidden(result)
    assert not leaks, (
        f"{case_id}: handler output contains {len(leaks)} forbidden "
        f"brand-string match(es): {leaks}. "
        "Either rebrand to HADES or extend HISTORICAL_ALLOWLIST with explicit "
        "rationale per spec §Q3 BORDERLINE."
    )


@pytest.mark.parametrize(
    "handler,raw_args,case_id",
    HANDLER_ERROR_CASES,
    ids=[case[2] for case in HANDLER_ERROR_CASES],
)
def test_handler_error_path_has_hades_branding(
    handler: Callable[[str], str | None],
    raw_args: str,
    case_id: str,
) -> None:
    """Phase E rebrand: every handler's _PROMPT_NO_* error path output also
    carries HADES branding. Per spec §Q6 recovery-hint template.
    """
    result = handler(raw_args)
    assert result is not None, f"{case_id}: handler returned None on error path"
    assert isinstance(result, str), f"{case_id}: handler returned non-str: {type(result)}"

    assert "HADES" in result, f"{case_id}: error-path output missing HADES brand string"

    leaks = _scan_forbidden(result)
    assert not leaks, (
        f"{case_id}: error-path output contains {len(leaks)} forbidden "
        f"brand-string match(es): {leaks}"
    )


@pytest.mark.parametrize(
    "handler,raw_args,case_id",
    HANDLER_ERROR_CASES,
    ids=[case[2] for case in HANDLER_ERROR_CASES],
)
def test_recovery_hints_match_q6_template(
    handler: Callable[[str], str | None],
    raw_args: str,
    case_id: str,
) -> None:
    """Spec §Q6 recovery-hint template: HADES: <short>\\n  <body>\\n  → <hint>.

    Phase E-11 ships placeholder strings matching this shape; Plan 18c
    rewires through internal/cli/error_render.go. This test asserts the
    shape: a line beginning with "HADES:" + a subsequent line beginning
    with "  →" (the green recovery-hint arrow).
    """
    result = handler(raw_args)
    assert result is not None, f"{case_id}: handler returned None"

                                                                  
    assert re.search(r"^HADES:", result, flags=re.MULTILINE), (
        f"{case_id}: error-path output does not contain a 'HADES:' headline "
        "matching spec §Q6 recovery-hint template"
    )

                                                            
    assert re.search(r"^\s+→", result, flags=re.MULTILINE), (
        f"{case_id}: error-path output does not contain a '→' recovery-hint "
        "line matching spec §Q6 recovery-hint template"
    )


def test_commands_init_docstring_uses_hades_slash_namespace() -> None:
    """plugin/hades/commands/__init__.py docstring references slash names;
    Phase E-3 rebrands those references to /hades:start, /hades:handoff,
    /hades:install-mcps form (matching the post-B namespace).
    """
    import importlib

    mod = importlib.import_module("hermes_plugins.hades.commands")
    doc = mod.__doc__ or ""

                                                                          
                                 
    if "/" in doc:
        assert "/zen-swarm:" not in doc, (
            "plugin/hades/commands/__init__.py docstring still references "
            "/zen-swarm: namespace; Phase E-3 rebrand incomplete"
        )


def test_handler_signatures_unchanged() -> None:
    """Sister-test: Phase E MUST NOT change handler signatures. Each handler
    accepts a single str arg and returns str | None.
    """
    import inspect

    for handler, _, case_id in HANDLER_SUCCESS_CASES:
        sig = inspect.signature(handler)
        params = list(sig.parameters.values())
        assert len(params) == 1, (
            f"{case_id}: handler signature changed; expected 1 arg, got {len(params)}"
        )
        param = params[0]
                                                                                    
        ann = param.annotation
        assert ann is str or ann == "str" or ann is inspect.Parameter.empty, (
            f"{case_id}: handler arg annotation must be str (got {ann!r})"
        )
