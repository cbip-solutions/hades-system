# SPDX-License-Identifier: MIT
from fastapi import FastAPI, APIRouter

app = FastAPI()
                                                                         
r = APIRouter()


@r.get("/items")
def items():
    return []


                                                                       
app.include_router(r)
