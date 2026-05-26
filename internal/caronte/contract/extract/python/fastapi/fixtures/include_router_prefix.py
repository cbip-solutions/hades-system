# SPDX-License-Identifier: MIT
from fastapi import FastAPI, APIRouter

app = FastAPI()
inner = APIRouter(prefix="/v1")


@inner.get("/items/{item_id}")
def get_item(item_id: int):
    return {"id": item_id}


app.include_router(inner, prefix="/api")
