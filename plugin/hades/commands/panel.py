# SPDX-License-Identifier: MIT
"""/hades:panel <name> slash command handler — subprocess handoff to TUI panel."""

from __future__ import annotations

from typing import Final

from hermes_plugins.hades.commands._subprocess_handoff import (
    render_hades_block,
    run_hades_subprocess,
)

                                                         
                                                      
_VALID_PANELS: frozenset[str] = frozenset(
    {
        "workforce",
        "cost",
        "audit",
        "hra",
        "confirmations",
        "memory",
        "skills",
        "doctrine",
        "codegraph",
        "inbox",
        "crossproject",
        "help",
    }
)

                                                                    
                                                               
 
                                                                           
                                                                              
                                                                               
                                                                        
                                                                 
                                                                     
                                                                          
                                                         
 
# CANONICAL SOURCE (do not edit here without reconciling Go catalog):
                                                                   
                                                                    

                                                                                 
_PANEL_VALIDATION_TITLE: Final[str] = "Argument validation failed."

                                                                       
                  
_PANEL_VALIDATION_BODY: Final[str] = (
    "One of the flags or positional arguments failed validation (wrong type, "
    "out of range, conflicting flags, or a required flag missing). The "
    "subcommand did not run."
)

                                                                       
                                                                              
                                                                                
_PANEL_VALIDATION_RECOVERY: Final[str] = (
    "show usage: hades <subcommand> --help (lists every flag with its "
    "constraints); common errors: --apply requires --dry-run=false; --panel "
    "requires a value from the 12-panel allowlist "
    "(workforce/cost/audit/hra/confirmations/memory/skills/doctrine/codegraph/"
    "inbox/crossproject/help)"
)

                                                                               
                                                                              
                                                                               
                                                                            
_PANEL_VALIDATION_HADES_BLOCK: Final[str] = render_hades_block(
    title=_PANEL_VALIDATION_TITLE,
    body=_PANEL_VALIDATION_BODY,
    recovery=_PANEL_VALIDATION_RECOVERY,
)


def panel_handler(raw_args: str) -> str | None:
    """/hades:panel <name> handler — spawn `hades dashboard --panel=<name>`.

    Args:
        raw_args: Operator's args after the slash command name. Expected: a
            single panel name with optional surrounding whitespace.
            Examples: "codegraph", "  workforce  ", "help".

    Returns:
        None when the TUI exits cleanly (returncode 0).
        A HADES-branded error string when:
        - raw_args is empty (no panel name supplied)
        - raw_args contains multiple tokens (e.g., "codegraph extra")
        - panel name is not in the 12-panel allowlist
        - `hades` binary not on PATH
        - subprocess.run raises
        - subprocess returncode != 0

    Per spec §Q8 D-pattern: lazygit-style subprocess handoff. Terminal mode is
    captured before spawn and restored after (via _subprocess_handoff helper).

    Per Stage 2 C-5 operator decision (2026-05-21): invalid panel names render
    the `cli.arg-validation-fail` HADES block LOCALLY (static
    _PANEL_VALIDATION_HADES_BLOCK) — no daemon roundtrip. inv-zen-088 is
    preserved trivially: this path makes no network calls at all.
    """
                                         
    stripped = raw_args.strip()
    if not stripped:
                                                                                
        return _PANEL_VALIDATION_HADES_BLOCK

    tokens = stripped.split()
    if len(tokens) != 1:
                                                                        
        return _PANEL_VALIDATION_HADES_BLOCK

    panel_name = tokens[0]
    if panel_name not in _VALID_PANELS:
                                                                       
        return _PANEL_VALIDATION_HADES_BLOCK

                                                              
    return run_hades_subprocess(extra_args=[f"--panel={panel_name}"])
