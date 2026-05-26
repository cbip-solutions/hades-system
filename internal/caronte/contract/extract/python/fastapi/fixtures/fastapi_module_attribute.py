# SPDX-License-Identifier: MIT
import fastapi

                                                                            
                                                                 
                                 
router = fastapi.APIRouter(prefix="/sys")


@router.get("/status")
def status():
    return {"ok": True}
