# SPDX-License-Identifier: MIT
                                                         

def register(ctx):
    ctx.register_hook("pre_tool_call", pre_tool_call_callback)
