"""Private Immich-compatible endpoints with independent CPU/GPU process lifetimes."""
import json
import multiprocessing as mp
import os
import secrets
import threading
from concurrent.futures import ThreadPoolExecutor
from contextlib import asynccontextmanager
import asyncio

from fastapi import FastAPI, Request, HTTPException
from fastapi.responses import PlainTextResponse, JSONResponse
from inference import Engine
from models import MODELS, ROOT, VERIFIED, installed, install, ModelUnavailable

MAX_BYTES = 24 * 1024 * 1024

def gpu_main(pipe):
    try:
        engine = Engine("cuda")
        while True:
            item = pipe.recv()
            if item is None:
                break
            try:
                pipe.send((engine.predict(**item), None))
            except Exception as exc:
                pipe.send((None, (type(exc).__name__, str(exc))))
    except Exception as exc:
        try:
            pipe.send((None, (type(exc).__name__, str(exc))))
        except (BrokenPipeError, EOFError):
            pass
    finally:
        pipe.close()

class Runtime:
    def __init__(self):
        self.cpu = Engine("cpu")
        self.cpu_lock = threading.Lock()
        self.gpu_lock = threading.Lock()
        self.child = None
        self.pipe = None

    def predict(self, entries, image=None, text=None, device="cpu"):
        if device == "cpu":
            with self.cpu_lock:
                return self.cpu.predict(entries, image, text)
        if device != "cuda":
            raise ValueError("device must be cpu or cuda")
        with self.gpu_lock:
            if self.child is None or not self.child.is_alive():
                self._release()
                ctx = mp.get_context("spawn")
                self.pipe, child_pipe = ctx.Pipe()
                self.child = ctx.Process(target=gpu_main, args=(child_pipe,), daemon=True)
                self.child.start()
                child_pipe.close()
            self.pipe.send({"entries": entries, "image": image, "text": text})
            if not self.pipe.poll(120):
                self._release()
                raise RuntimeError("GPU inference timed out")
            result, error = self.pipe.recv()
            if error:
                if error[0] == "ModelUnavailable":
                    raise ModelUnavailable(error[1])
                raise RuntimeError(error[1])
            return result

    def _release(self):
        if self.child is not None:
            if self.child.is_alive():
                try:
                    self.pipe.send(None)
                except (BrokenPipeError, EOFError):
                    pass
                self.child.join(5)
                if self.child.is_alive():
                    self.child.kill()
                    self.child.join()
            self.child.close()
            self.child = None
        if self.pipe is not None:
            self.pipe.close()
            self.pipe = None

    def release(self):
        with self.gpu_lock:
            self._release()
        return {"released": True}

runtime = Runtime()
executor = ThreadPoolExecutor(max_workers=4)
gpu_admission = asyncio.Lock()

@asynccontextmanager
async def lifespan(app):
    for name in MODELS:
        try:
            installed(name, verify=True)
        except ModelUnavailable as exc:
            print(str(exc), flush=True)
    yield
    runtime.release()
    executor.shutdown(wait=True)

app = FastAPI(lifespan=lifespan)

@app.post("/v1/models/manage")
async def manage_model(request: Request):
    # Weight mutations are unavailable on deployments without an internal credential.
    if not os.environ.get("VISION_TOKEN"):
        raise HTTPException(503, "model management requires VISION_TOKEN")
    data = await request.json()
    name, action = data.get("model"), data.get("action")
    if name not in MODELS or action not in ("install", "load", "unload", "test", "remove"):
        raise HTTPException(422, "unsupported model operation")
    if gpu_admission.locked():
        raise HTTPException(409, "model is actively used by a GPU batch")
    async with gpu_admission:
        def operate():
            if not runtime.cpu_lock.acquire(blocking=False):
                raise HTTPException(409, "model is actively used")
            if not runtime.gpu_lock.acquire(blocking=False):
                runtime.cpu_lock.release()
                raise HTTPException(409, "model is actively used")
            try:
                if action == "install":
                    return install(name)
                if action in ("load", "test"):
                    manifest = installed(name, verify=True)
                    components = ("visual", "textual")
                    for component in components:
                        runtime.cpu.session(name, component)
                    return {"loaded": True, "device": "cpu", "fingerprint": manifest["fingerprint"]}
                runtime._release()
                runtime.cpu.sessions = {key: value for key, value in runtime.cpu.sessions.items() if key[0] != name}
                runtime.cpu.tokenizers.pop(name, None)
                if action == "remove":
                    # Remove only the allowlisted model's weights under the model root.
                    import shutil
                    directory = (ROOT / name).resolve()
                    if directory.parent != ROOT.resolve() or (ROOT / name).is_symlink():
                        raise HTTPException(422, "invalid model directory")
                    if directory.exists():
                        shutil.rmtree(directory)
                    VERIFIED.pop(name, None)
                return {"status": "completed", "action": action, "model": name}
            finally:
                runtime.gpu_lock.release()
                runtime.cpu_lock.release()
        return await asyncio.get_running_loop().run_in_executor(executor, operate)

@app.middleware("http")
async def authenticate(request, call_next):
    token = os.environ.get("VISION_TOKEN", "")
    if token and not secrets.compare_digest(request.headers.get("authorization", ""), "Bearer " + token):
        return JSONResponse({"error": "unauthorized"}, status_code=401)
    length = request.headers.get("content-length")
    if length and (not length.isdigit() or int(length) > MAX_BYTES):
        return JSONResponse({"error": "request too large"}, status_code=413)
    return await call_next(request)

@app.get("/ping", response_class=PlainTextResponse)
def ping():
    return "pong"

@app.get("/v1/capabilities")
def capabilities():
    import onnxruntime as ort
    models = []
    for name in MODELS:
        try:
            manifest = installed(name)
            models.append({"name": name, "ready": True, **{key: manifest[key] for key in ("fingerprint", "dimensions", "recipe", "revision", "license")}})
        except ModelUnavailable as exc:
            models.append({"name": name, "ready": False, "error": str(exc)})
    return {"protocol": "immich-3.1.0", "models": models, "providers": ort.get_available_providers(), "max_batch": 32, "max_bytes": MAX_BYTES}

async def run(**kwargs):
    try:
        return await asyncio.get_running_loop().run_in_executor(executor, lambda: runtime.predict(**kwargs))
    except ModelUnavailable as exc:
        raise HTTPException(503, detail={"code": "waiting_model", "message": str(exc)}) from exc
    except (ValueError, KeyError, TypeError) as exc:
        raise HTTPException(422, detail=str(exc)) from exc
    except Exception as exc:
        raise HTTPException(503, detail={"code": "inference_failed", "message": str(exc)}) from exc

async def form(request):
    # Bound actual bytes even when Content-Length was omitted.
    body = bytearray()
    async for chunk in request.stream():
        body.extend(chunk)
        if len(body) > MAX_BYTES:
            raise HTTPException(413, "request too large")
    request._body = bytes(body)
    try:
        return await request.form(max_files=32, max_fields=8)
    except Exception as exc:
        raise HTTPException(422, "invalid multipart request") from exc

@app.post("/predict")
async def predict(request: Request):
    values = await form(request)
    try:
        entries = json.loads(values.get("entries", "{}"))
        if len(values.getlist("image")) + len(values.getlist("text")) != 1:
            raise HTTPException(422, "provide exactly one image upload or text field")
        image = values.get("image")
        if image is not None and not hasattr(image, "read"):
            raise HTTPException(422, "image must be an upload")
        image = await image.read() if image is not None else None
        return await run(entries=entries, image=image, text=values.get("text"), device="cpu")
    except json.JSONDecodeError as exc:
        raise HTTPException(422, "invalid entries") from exc
    finally:
        await values.close()

@app.post("/v1/predict-batch")
async def batch(request: Request):
    values = await form(request)
    try:
        entries = json.loads(values.get("entries", "{}"))
        ids = json.loads(values.get("ids", "[]"))
        images = values.getlist("images")
        if not isinstance(ids,list) or not all(isinstance(id,str) and 1<=len(id)<=128 for id in ids) or not 1 <= len(ids) <= 32 or len(ids) != len(images) or len(set(ids)) != len(ids):
            raise HTTPException(422, "provide 1–32 unique ids and matching images")
        if not all(hasattr(image, "read") for image in images):
            raise HTTPException(422, "images must be uploads")
        results = []
        # A release waits for the entire admitted GPU batch, not just its current image.
        gpu = values.get("device", "cpu") == "cuda"
        if gpu:
            await gpu_admission.acquire()
            admitted = True
        for id, image in zip(ids, images):
            try:
                result = await run(entries=entries, image=await image.read(), device=values.get("device", "cpu"))
                results.append({"id": id, "result": result})
            except HTTPException as exc:
                results.append({"id": id, "error": exc.detail})
        return {"results": results}
    except json.JSONDecodeError as exc:
        raise HTTPException(422, "invalid entries or ids") from exc
    finally:
        if locals().get("admitted", False):
            gpu_admission.release()
        await values.close()

@app.post("/v1/runtime/release")
async def release():
    async with gpu_admission:
        return await asyncio.get_running_loop().run_in_executor(executor, runtime.release)
