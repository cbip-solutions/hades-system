# SPDX-License-Identifier: MIT
from fastapi import FastAPI

app = FastAPI()


@app.get("/items/{item_id:int}")
def get_item(item_id: int):
    return {"id": item_id}


@app.get("/files/{file_path:path}")
def get_file(file_path: str):
    return {"path": file_path}
