# SPDX-License-Identifier: MIT
# Do not edit by hand; re-run with --force to regenerate.

def register(ctx):
    ctx.register_hook("pre_tool_call", pre_tool_call_callback)
