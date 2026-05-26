# SPDX-License-Identifier: MIT
                                                          
                                                                   
 
                                                                                   
                                                                    
                                                                     

import subprocess
from pathlib import Path

_SIDECAR = Path(__file__).parent / "pre_tool_call.sh"

def pre_tool_call_callback(**kwargs):
    body = _SIDECAR.read_text(encoding="utf-8")
    result = subprocess.run(
        ["/bin/bash", "-c", body],
        capture_output=True, text=True, env=kwargs.get("env", None),
    )
    if result.returncode != 0:
        return {"action": "block", "message": result.stderr}
    return None
