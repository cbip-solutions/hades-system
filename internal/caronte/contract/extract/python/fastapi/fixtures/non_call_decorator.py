# SPDX-License-Identifier: MIT
from fastapi import FastAPI

app = FastAPI()


                                                                            
                                                                         
                                  
@app
def bare_decorator_target():
    return {}


                                                                
@cached
def cached_target():
    return {}


                                                                             
                                                              
@(app.dep_chain).get("/skip")
def complex_decorator():
    return {}
