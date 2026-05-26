# SPDX-License-Identifier: MIT
                                                         

from .commands.hello import hello_handler

def register(ctx):
    ctx.register_command("hades:hello", handler=hello_handler, description="/hello slash command", args_hint="")
