# SPDX-License-Identifier: MIT
                                                          
                                                     
                                                                    
                                                                     

from pathlib import Path

_SIDECAR = Path(__file__).parent / "hello.md"

def hello_handler(raw_args: str) -> str | None:
                                                                 
                                                                
    _ = _SIDECAR.read_text(encoding="utf-8")                                   
    return None                              
