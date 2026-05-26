# SPDX-License-Identifier: MIT
 
                                                                            
                                                                        
                                                                   
                                                                        
                                                                        
                                                                       
                                             
 
                                                 
                                                                         
                                                      
                                                                             
                                                                           
                                              
import os
import sys

import httpx


def main() -> int:
    base = os.environ["BACKEND_URL"]
    resp = httpx.get(base + "/users/123")
    print(resp.json())
    return 0


if __name__ == "__main__":
    sys.exit(main())
