# SPDX-License-Identifier: MIT
"""Hermes pre_tool_call + post_tool_call callbacks for zen-swarm."""

from __future__ import annotations

import contextlib
import logging
import re
from typing import Any

from ._common import invoke_event_poster, summarize_args

logger = logging.getLogger(__name__)

                                                                
                                                                            
                                                                                               
                                                                                 
                                                                                  
                                                                             
                       
_FORBIDDEN_PATTERN = re.compile(
    r"\b(claude|anthropic|generated.{0,30}(by|with)\s*ai|co-authored-by:\s*claude)",
    re.IGNORECASE,
)

                                                                       
                                      
_COMMIT_MSG_PATTERN = re.compile(r"-m\s+(['\"])((?:\\.|.)*?)\1", re.DOTALL)

                                                                          
                                                               
                                                               
                                                                             
                                                                       
                                                                        
                                                                   
_FILE_EDIT_TOOLS = frozenset({"write_file", "patch"})


def extract_commit_message(cmd: str) -> str:
    """Extract the message body from a `git commit -m "..."` command string.

    Returns "" if no `-m` flag matches.
    """
    if not isinstance(cmd, str):
        return ""
    match = _COMMIT_MSG_PATTERN.search(cmd)
    if not match:
        return ""
    return match.group(2)


def is_ai_attributed(message: str) -> bool:
    """Return True if the commit message contains an AI-attribution marker.

    Per invariant (NO Claude attribution in commits).
    """
    if not message:
        return False
    return bool(_FORBIDDEN_PATTERN.search(message))


def _block_directive() -> dict[str, str]:
    """Return the standard invariant block directive dict."""
    return {
        "action": "block",
        "message": (
            "zen-swarm: commit message contains AI attribution per inv-zen-004. "
            "Remove mention of Claude/Anthropic/AI generation."
        ),
    }


def pre_tool_call(
    tool_name: str = "",
    args: Any = None,
    task_id: str = "",
    session_id: str = "",
    tool_call_id: str = "",
    **kwargs: Any,
) -> dict | None:
    """Hermes hook callback for `pre_tool_call`.

    Signature mirrors get_pre_tool_call_block_message kwargs at
    hermes_cli/plugins.py:1224-1231. Returns None to continue, or
    {"action": "block", "message": "..."} to block.
    """
    payload = {
        "tool_name": tool_name,
        "args_summary": summarize_args(args),
        "task_id": task_id,
        "session_id": session_id,
        "tool_call_id": tool_call_id,
        "hook_event_name": "pre_tool_call",
    }

                                                                         
                                                                      
                            
    _ = invoke_event_poster("pre_tool_call", payload)

                              
    if tool_name != "Bash":
        return None
    if not isinstance(args, dict):
        return None
    cmd = args.get("command")
    if not isinstance(cmd, str):
        return None
    if not cmd.lstrip().startswith("git commit"):
        return None
    msg = extract_commit_message(cmd)
    if not msg:
        return None
    if is_ai_attributed(msg):
                                                                          
                                                                    
        blocked_payload = dict(payload)
        blocked_payload["hook_event_name"] = "pre_tool_call.blocked"
        blocked_payload["reason"] = "inv-zen-004"
        _ = invoke_event_poster("pre_tool_call.blocked", blocked_payload)
        return _block_directive()

    return None


def post_tool_call(
    tool_name: str = "",
    args: Any = None,
    result: Any = None,
    task_id: str = "",
    session_id: str = "",
    tool_call_id: str = "",
    **kwargs: Any,
) -> None:
    """Hermes hook callback for `post_tool_call`.

    Observer hook (return value ignored). Emits a `post_tool_call` event.
    For write-class tools, ALSO emits a derived `file.edited` event for
    path-level audit visibility separate from the tool-level event.
    """
                                                                          
                                                                             
                                                                          
                                                                     
    result_kind = type(result).__name__ if result is not None else "None"
    result_size = -1
    with contextlib.suppress(TypeError):
                                                                
        result_size = len(result)

    payload = {
        "tool_name": tool_name,
        "args_summary": summarize_args(args),
        "result_kind": result_kind,
        "result_size": result_size,
        "task_id": task_id,
        "session_id": session_id,
        "tool_call_id": tool_call_id,
        "hook_event_name": "post_tool_call",
    }
    _ = invoke_event_poster("post_tool_call", payload)

    if tool_name in _FILE_EDIT_TOOLS:
                                                                
        derived = dict(payload)
        derived["hook_event_name"] = "file.edited"
        _ = invoke_event_poster("file.edited", derived)

    return
