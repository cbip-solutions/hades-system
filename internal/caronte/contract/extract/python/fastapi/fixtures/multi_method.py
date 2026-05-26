# SPDX-License-Identifier: MIT
from fastapi import FastAPI

app = FastAPI()


@app.get("/users/{id}")
def get_user(id: int):
    return {"id": id}


@app.post("/users")
def create_user():
    return {"id": 1}


@app.put("/users/{id}")
def update_user(id: int):
    return {"id": id}


@app.delete("/users/{id}")
def delete_user(id: int):
    return {}


@app.patch("/users/{id}")
def patch_user(id: int):
    return {"id": id}
