# SPDX-License-Identifier: MIT
import fastapi

# Using the module-attribute syntax `fastapi.APIRouter(...)` rather than the
# more common `from fastapi import APIRouter`. This exercises the
# isAPIRouterCall attribute path.
router = fastapi.APIRouter(prefix="/sys")


@router.get("/status")
def status():
    return {"ok": True}
