# SPDX-License-Identifier: MIT
from fastapi import FastAPI, APIRouter

app = FastAPI()
v1 = APIRouter(prefix="/v1")
users = APIRouter(prefix="/users")


@users.get("/{id}")
def get_user(id: int):
    return {"id": id}


v1.include_router(users)
app.include_router(v1, prefix="/api")
