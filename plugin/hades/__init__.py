# SPDX-License-Identifier: MIT
"""hades — HADES system Hermes Agent plugin."""

from __future__ import annotations

from pathlib import Path
from typing import Any

                                                                           
                                                                             
                       
from .commands.handoff import handle_handoff
from .commands.install_mcps import handle_install_mcps
from .commands.start import handle_start
from .commands.status import handle_status
from .hooks.llm_handlers import pre_llm_call
from .hooks.session_handlers import on_session_end, on_session_start
from .hooks.tool_handlers import post_tool_call, pre_tool_call
from .hooks.wizard_handler import _maybe_launch_wizard
from .renderers import RendererRegistry, register_default_renderers
from .skins.hades import _maybe_activate_hades, register_hades_skin

_PLUGIN_ROOT = Path(__file__).resolve().parent
_SKILLS_DIR = _PLUGIN_ROOT / "skills"

                                                                          
                                                                            
                                                                        
                                                                      
                                                  
 
                                                                       
                                                                          
                                                                      
_RENDERER_REGISTRY: RendererRegistry = RendererRegistry()


def get_renderer_registry() -> RendererRegistry:
    """Return the module-global RendererRegistry populated by ``register(ctx)``.

    Used by zen-side callbacks (``hooks/llm_handlers.py``, AFK summary
    builder, slash command handlers) to dispatch citation envelopes
    through the 6 platform renderers + markdown fallback.

    Phase A this returns a registry pre-populated with the 6 default
    platform renderers (Ink/Telegram/Slack/Email/Voice/Web) and the
    universal markdown fallback (registry-internal)."""
    return _RENDERER_REGISTRY


def register(ctx: Any) -> None:
    """Hermes plugin entry point.

    Called once by Hermes after importing this package. ``ctx`` is a
    ``hermes_cli.plugins.PluginContext`` instance; we use its register_*
    methods to wire hooks, skills, and slash commands.

    Wiring summary:
    - 5 lifecycle hooks: on_session_start, on_session_end, pre_tool_call,
      post_tool_call, pre_llm_call
    - 13 skills (resolvable as ``hades:<name>``): hades, start, handoff,
      brainstorm, write-plan, execute-plan, doctrine, amendment,
      impact-pre-merge, audit-impact, doctrine-drift-check, knowledge-query,
      knowledge-promote
    - 25 slash commands under ``hades:`` namespace per spec §Q4 hard cutover:
      /hades:start, /hades:handoff, /hades:install-mcps, /hades:brainstorm,
      /hades:write-plan, /hades:execute-plan, /hades:doctrine,
      /hades:amendment-{list,show,ack,deny}, /hades:impact-pre-merge,
      /hades:audit-impact, /hades:doctrine-drift-check,
      /hades:knowledge-{query,promote}, /hades:full, /hades:voice,
      /hades:openspec-{apply,archive,propose,resume},
      /hades:status (Plan 18c C), /hades:dashboard + /hades:panel (Plan 18c D)
    """
                                                                               
    ctx.register_hook("on_session_start", on_session_start)
    ctx.register_hook("on_session_end", on_session_end)
    ctx.register_hook("pre_tool_call", pre_tool_call)
    ctx.register_hook("post_tool_call", post_tool_call)
    ctx.register_hook("pre_llm_call", pre_llm_call)

                                                                               
                                                                          
                                                                        
                                                                         
                                                                           
                                                                          
                                                                         
                                                              
    try:
        register_hades_skin()
    except Exception as e:  # pragma: no cover — defensive
                                                                        
                                                            
        import logging

        logging.getLogger(__name__).warning(
            "HADES skin deploy failed (plugin loads, skin remains default): %s", e
        )
    ctx.register_hook("on_session_start", _maybe_activate_hades)

                                                                               
                                                                          
                                                                          
                                                                                
                                                                           
                                                                          
    ctx.register_hook("on_session_start", _maybe_launch_wizard)

                                                                               
    ctx.register_skill(
        "hades",
        _SKILLS_DIR / "hades" / "SKILL.md",
        description="Canonical HADES system project skill (doctrine + workflow + hard rules; formerly zen-swarm).",
    )
    ctx.register_skill(
        "start",
        _SKILLS_DIR / "start" / "SKILL.md",
        description="Recover a hades session: read HANDOFF.md TL;DR + git state + active plan.",
    )
    ctx.register_skill(
        "handoff",
        _SKILLS_DIR / "handoff" / "SKILL.md",
        description="Snapshot hades session state to HANDOFF.md, commit, optionally push.",
    )

                                                                               
    ctx.register_skill(
        "brainstorm",
        _SKILLS_DIR / "brainstorm" / "SKILL.md",
        description="hades research-first brainstorming (project-doctrine override on superpowers:brainstorming)",
    )
    ctx.register_skill(
        "write-plan",
        _SKILLS_DIR / "write-plan" / "SKILL.md",
        description="hades TDD-task-decomposed plan writing (master+phase files, watchdog mitigation, two-stage review)",
    )
    ctx.register_skill(
        "execute-plan",
        _SKILLS_DIR / "execute-plan" / "SKILL.md",
        description="hades subagent-driven plan execution (Stage 0 reality-check, per-task dispatch, review gates)",
    )
    ctx.register_skill(
        "doctrine",
        _SKILLS_DIR / "doctrine" / "SKILL.md",
        description="hades doctrine control: show active doctrine or apply runtime override (audit-logged)",
    )
    ctx.register_skill(
        "amendment",
        _SKILLS_DIR / "amendment" / "SKILL.md",
        description="hades doctrine-amendment lifecycle: list, show, acknowledge, or deny proposals (Plan 5+8)",
    )
    ctx.register_skill(
        "impact-pre-merge",
        _SKILLS_DIR / "impact-pre-merge" / "SKILL.md",
        description="hades pre-merge blast radius analysis via Plan 11 augmentation pipeline + caronte code-graph",
    )
    ctx.register_skill(
        "audit-impact",
        _SKILLS_DIR / "audit-impact" / "SKILL.md",
        description="hades audit event KG context: resolve Tessera-anchored event + show 5-lane RRF context",
    )
    ctx.register_skill(
        "doctrine-drift-check",
        _SKILLS_DIR / "doctrine-drift-check" / "SKILL.md",
        description="hades doctrine drift detection via caronte code-graph query vs Plan 8 config",
    )
    ctx.register_skill(
        "knowledge-query",
        _SKILLS_DIR / "knowledge-query" / "SKILL.md",
        description="hades cross-project federated knowledge query (Plan 9 D aggregator + Plan 11 privacy filter)",
    )
    ctx.register_skill(
        "knowledge-promote",
        _SKILLS_DIR / "knowledge-promote" / "SKILL.md",
        description="hades knowledge promotion to global cross-project memory (Plan 9 D, audit-logged)",
    )

                                                                               
                                                                         
                                                                   
                                                                        
                                                                         
    ctx.register_command(
        "hades:start",
        handler=handle_start,
        description="Recover a HADES session (HANDOFF.md + git state + active plan).",
    )
    ctx.register_command(
        "hades:handoff",
        handler=handle_handoff,
        description="Snapshot HADES session state to HANDOFF.md and commit.",
    )
    ctx.register_command(
        "hades:install-mcps",
        handler=handle_install_mcps,
        description="Install HADES zen MCPs into ~/.hermes/config.yaml via 'hermes mcp add' (caronte is in-process, no MCP needed).",
    )
    ctx.register_command(
        "hades:status",
        handler=handle_status,
        description="Show verbose HADES runtime status: daemon, model, cascade, bypass, cost, context, profile, cwd.",
        args_hint="[--json]",
    )

                                                                               
    from .commands.brainstorm import brainstorm_handler
    from .commands.write_plan import write_plan_handler
    from .commands.execute_plan import execute_plan_handler

    ctx.register_command(
        "hades:brainstorm",
        handler=brainstorm_handler,
        description="Research-first brainstorming Q&A (HADES project-doctrine override)",
        args_hint="[topic-seed]",
    )
    ctx.register_command(
        "hades:write-plan",
        handler=write_plan_handler,
        description="Generate TDD-task-decomposed plan files from a frozen design spec",
        args_hint="<spec-path>",
    )
    ctx.register_command(
        "hades:execute-plan",
        handler=execute_plan_handler,
        description="Execute phase plan via subagent dispatch with review gates",
        args_hint="<plan-path>",
    )

                                                                               
    from .commands.doctrine import doctrine_handler
    from .commands.amendment_list import amendment_list_handler
    from .commands.amendment_show import amendment_show_handler
    from .commands.amendment_ack import amendment_ack_handler
    from .commands.amendment_deny import amendment_deny_handler

    ctx.register_command(
        "hades:doctrine",
        handler=doctrine_handler,
        description="Show active doctrine OR override at runtime (audit-logged via Plan 9)",
        args_hint="[doctrine-name]",
    )
    ctx.register_command(
        "hades:amendment-list",
        handler=amendment_list_handler,
        description="List pending doctrine-amendment proposals (Plan 5 + Plan 8 lifecycle)",
        args_hint="[project]",
    )
    ctx.register_command(
        "hades:amendment-show",
        handler=amendment_show_handler,
        description="Show full detail of a pending doctrine-amendment proposal",
        args_hint="<amendment-id>",
    )
    ctx.register_command(
        "hades:amendment-ack",
        handler=amendment_ack_handler,
        description="Acknowledge + apply pending doctrine amendment (audit-logged)",
        args_hint="<amendment-id> [reason]",
    )
    ctx.register_command(
        "hades:amendment-deny",
        handler=amendment_deny_handler,
        description="Deny pending doctrine amendment (audit-logged; reason required)",
        args_hint="<amendment-id> <reason>",
    )

                                                                               
    from .commands.impact_pre_merge import impact_pre_merge_handler
    from .commands.audit_impact import audit_impact_handler
    from .commands.doctrine_drift_check import doctrine_drift_check_handler

    ctx.register_command(
        "hades:impact-pre-merge",
        handler=impact_pre_merge_handler,
        description="Analyze blast radius of pending merge (zen-specific KG augmentation per spec §4.4)",
        args_hint="<branch>",
    )
    ctx.register_command(
        "hades:audit-impact",
        handler=audit_impact_handler,
        description="Show KG context (augmentation citations + caller/callee/community) for an audit event",
        args_hint="<event-id>",
    )
    ctx.register_command(
        "hades:doctrine-drift-check",
        handler=doctrine_drift_check_handler,
        description="Detect doctrine drift across project (caronte code-graph query vs current config)",
        args_hint="[project]",
    )

                                                                               
    from .commands.knowledge_query import knowledge_query_handler
    from .commands.knowledge_promote import knowledge_promote_handler

    ctx.register_command(
        "hades:knowledge-query",
        handler=knowledge_query_handler,
        description="Cross-project federated knowledge query (Plan 9 D aggregator + Plan 11 privacy filter)",
        args_hint="<pattern> [scope]",
    )
    ctx.register_command(
        "hades:knowledge-promote",
        handler=knowledge_promote_handler,
        description="Promote a knowledge item to global (Plan 9 D promote with audit chain anchor)",
        args_hint="<item-id> <reason>",
    )

                                                                               
    from .commands.full import full_handler
    from .commands.voice import voice_handler

    ctx.register_command(
        "hades:full",
        handler=full_handler,
        description="Expand a mobile-format citation summary to full content (Q6=B AFK comprehensive)",
        args_hint="<citation-id>",
    )
    ctx.register_command(
        "hades:voice",
        handler=voice_handler,
        description="Voice memo input flow (sync if estimated <10s; async beyond) — Q6=B AFK",
        args_hint="[query]",
    )

                                                                               
    from .commands.openspec_apply import openspec_apply_handler
    from .commands.openspec_archive import openspec_archive_handler
    from .commands.openspec_propose import openspec_propose_handler
    from .commands.openspec_resume import openspec_resume_handler

    ctx.register_command(
        "hades:openspec-apply",
        handler=openspec_apply_handler,
        description="Run the swarm to implement tasks.md (parallel subagents)",
        args_hint="<feature-name>",
    )
    ctx.register_command(
        "hades:openspec-archive",
        handler=openspec_archive_handler,
        description="Three-tier review (routine/attention/decision) and merge",
        args_hint="<feature-name>",
    )
    ctx.register_command(
        "hades:openspec-propose",
        handler=openspec_propose_handler,
        description="Begin the propose phase for a new feature (Modo C híbrido)",
        args_hint="<feature-name>",
    )
    ctx.register_command(
        "hades:openspec-resume",
        handler=openspec_resume_handler,
        description="Resume a paused propose/apply/archive phase",
        args_hint="<feature-name>",
    )

                                                                                
                                                                            
                                                                             
                                                                               
                                                                                 
                                                                               
                                                                                
            
    from .commands.dashboard import dashboard_handler
    from .commands.panel import panel_handler

    ctx.register_command(
        "hades:dashboard",
        handler=dashboard_handler,
        description=(
            "Open the HADES TUI dashboard on the default panel "
            "(lazygit-style subprocess handoff per spec §Q8 D-pattern)"
        ),
    )
    ctx.register_command(
        "hades:panel",
        handler=panel_handler,
        description=(
            "Open the HADES TUI dashboard direct to a named panel "
            "(workforce/cost/audit/hra/confirmations/memory/skills/"
            "doctrine/codegraph/inbox/crossproject/help)"
        ),
        args_hint="<name>",
    )

                                                                               
                                                                              
                                                                  
                                                                             
                                                                          
                                                
    if hasattr(ctx, "register_status_provider"):
        from .commands.status_provider import status_segments

        ctx.register_status_provider(status_segments)

                                                                               
                                                                     
                                                                       
                                                                         
                                                                      
                                                                       
                                                                         
                                  
    register_default_renderers(_RENDERER_REGISTRY)
