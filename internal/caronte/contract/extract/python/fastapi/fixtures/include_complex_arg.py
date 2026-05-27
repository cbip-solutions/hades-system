# SPDX-License-Identifier: MIT
from fastapi import FastAPI, APIRouter

app = FastAPI()


def make_router():
    return APIRouter()


# include_router with a function-call argument — not a plain identifier; the
# extractor cannot statically resolve the router so the include is dropped.
# This exercises the firstPositionalIdentifier default branch (non-identifier
# expression) + the empty childVar early-return in collectIncludeEdge.
app.include_router(make_router(), prefix="/dynamic")


# A standalone router declared but never decorated — also exercises the bare
# router-binding path without endpoints.
r = APIRouter(prefix="/lonely")
