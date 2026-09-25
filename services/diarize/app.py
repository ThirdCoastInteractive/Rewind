"""Local speaker-diarization sidecar. Weights load only after an explicit install."""
import asyncio
import os
import secrets
import threading
from concurrent.futures import ThreadPoolExecutor
from contextlib import asynccontextmanager

from fastapi import FastAPI, HTTPException, Request
from fastapi.responses import JSONResponse

from models import MODELS, ROOT, ModelUnavailable, install, installed
from paths import allowed_audio

MAX_BYTES = 1 * 1024 * 1024
executor = ThreadPoolExecutor(max_workers=1)
lock = threading.Lock()


@asynccontextmanager
async def lifespan(app):
    yield
    executor.shutdown(wait=False)


app = FastAPI(lifespan=lifespan)


@app.middleware("http")
async def authenticate(request, call_next):
    token = os.environ.get("DIARIZE_TOKEN", "")
    if token and not secrets.compare_digest(request.headers.get("authorization", ""), "Bearer " + token):
        return JSONResponse({"error": "unauthorized"}, status_code=401)
    length = request.headers.get("content-length")
    if length and (not length.isdigit() or int(length) > MAX_BYTES):
        return JSONResponse({"error": "request too large"}, status_code=413)
    return await call_next(request)


@app.get("/health")
def health():
    return {"ok": True, "device": os.environ.get("DIARIZE_DEVICE", "cpu")}


@app.get("/v1/models")
def list_models():
    models = []
    for name, spec in MODELS.items():
        try:
            manifest = installed(name)
            models.append(
                {
                    "name": name,
                    "ready": True,
                    "fingerprint": manifest["fingerprint"],
                    "revision": manifest["revision"],
                    "license": manifest["license"],
                    "recipe": manifest["recipe"],
                    "kind": spec["kind"],
                }
            )
        except ModelUnavailable as exc:
            models.append({"name": name, "ready": False, "kind": spec["kind"], "error": str(exc)})
    return {"models": models}


@app.post("/v1/models/manage")
async def manage_model(request: Request):
    if not os.environ.get("DIARIZE_TOKEN"):
        raise HTTPException(503, "model management requires DIARIZE_TOKEN")
    data = await request.json()
    name, action = data.get("model"), data.get("action")
    if name not in MODELS or action not in ("install", "load", "unload", "test", "remove"):
        raise HTTPException(422, "unsupported model operation")

    def operate():
        if not lock.acquire(blocking=False):
            raise HTTPException(409, "model is actively used")
        try:
            if action == "install":
                return install(name)
            if action in ("load", "test"):
                return {"loaded": True, "manifest": installed(name, verify=True)}
            if action == "remove":
                import shutil

                directory = (ROOT / name).resolve()
                if directory.parent != ROOT.resolve() or (ROOT / name).is_symlink():
                    raise HTTPException(422, "invalid model directory")
                if directory.exists():
                    shutil.rmtree(directory)
            return {"status": "completed", "action": action, "model": name}
        finally:
            lock.release()

    return await asyncio.get_running_loop().run_in_executor(executor, operate)


@app.post("/v1/diarize")
async def diarize(request: Request):
    data = await request.json()
    name = data.get("model") or "nemotron-3"
    path = data.get("audio_path") or ""
    device = data.get("device") or os.environ.get("DIARIZE_DEVICE", "cpu")
    if device not in ("cpu", "cuda"):
        raise HTTPException(422, "device must be cpu or cuda")
    if name not in MODELS:
        raise HTTPException(422, "unsupported model")
    if not allowed_audio(path):
        raise HTTPException(422, "audio_path must be a file under the temp directory")
    try:
        installed(name)
    except ModelUnavailable as exc:
        raise HTTPException(503, detail={"code": "waiting_model", "message": str(exc)}) from exc

    def run():
        if not lock.acquire(blocking=True, timeout=1):
            raise HTTPException(409, "model is actively used")
        try:
            from inference import DiarizeError, diarize_path

            try:
                turns, fingerprint = diarize_path(path, name, device)
            except DiarizeError as exc:
                raise HTTPException(422, str(exc)) from exc
            return {"turns": turns, "fingerprint": fingerprint, "model": name}
        finally:
            lock.release()

    try:
        return await asyncio.get_running_loop().run_in_executor(executor, run)
    except ModelUnavailable as exc:
        raise HTTPException(503, detail={"code": "waiting_model", "message": str(exc)}) from exc


def create_app():
    return app
