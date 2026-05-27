# SPDX-License-Identifier: MIT
# Do not edit by hand; re-run with --force to regenerate.

from .commands.recall import recall_handler

def register(ctx):
    ctx.register_command("hades:recall", handler=recall_handler, description="/recall <topic>", args_hint="")
