# SPDX-License-Identifier: MIT
                                                         

from .commands.recall import recall_handler

def register(ctx):
    ctx.register_command("hades:recall", handler=recall_handler, description="/recall <topic>", args_hint="")
