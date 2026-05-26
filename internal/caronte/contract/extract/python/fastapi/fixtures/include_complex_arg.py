# SPDX-License-Identifier: MIT
from fastapi import FastAPI, APIRouter

app = FastAPI()


def make_router():
    return APIRouter()


                                                                            
                                                                           
                                                                             
                                                                      
app.include_router(make_router(), prefix="/dynamic")


                                                                            
                                        
r = APIRouter(prefix="/lonely")
