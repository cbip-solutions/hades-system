# SPDX-License-Identifier: MIT
from fastapi import FastAPI, APIRouter

app = FastAPI()
# APIRouter without prefix= keyword arg — exercises the "" prefix branch.
r = APIRouter()


@r.get("/items")
def items():
    return []


# include_router without prefix= keyword arg — exercises the "" branch.
app.include_router(r)
