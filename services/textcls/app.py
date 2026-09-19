"""Local-only ONNX text classifiers for comment and speech tone scoring."""
import asyncio
import os
import secrets
import threading
from concurrent.futures import ThreadPoolExecutor
from contextlib import asynccontextmanager

from fastapi import FastAPI, HTTPException, Request
from fastapi.responses import JSONResponse

from inference import Engine, stub_item
from models import MODELS, ModelUnavailable, install, installed

MAX_BYTES = 4 * 1024 * 1024
MAX_BATCH = 64
DEFAULT_SENTIMENT = os.environ.get("TEXTCLS_SENTIMENT_MODEL", "twitter-roberta-sentiment")
DEFAULT_TOXICITY = os.environ.get("TEXTCLS_TOXICITY_MODEL", "unbiased-toxic-roberta")


class Runtime:
    def __init__(self):
        device = os.environ.get("TEXTCLS_DEVICE", "cpu").lower()
        if device not in ("cpu", "cuda"):
            device = "cpu"
        self.device = device
        self.engine = Engine(device)
        self.lock = threading.Lock()

    def classify_batch(self, items, sentiment_model, toxicity_model):
        out = []
        with self.lock:
            for item in items:
                item_id = str(item.get("id", ""))
                text = item.get("text", "")
                try:
                    result = self.engine.classify(text, sentiment_model, toxicity_model)
                    out.append({"id": item_id, **result})
                except ModelUnavailable:
                    # Canary path: zeros so callers can probe without weights.
                    out.append(stub_item(item_id))
                    raise
        return out


runtime = Runtime()
executor = ThreadPoolExecutor(max_workers=2)


@asynccontextmanager
async def lifespan(app):
    for name in MODELS:
        try:
            installed(name, verify=True)
        except ModelUnavailable as exc:
            print(str(exc), flush=True)
    yield
    executor.shutdown(wait=True)


app = FastAPI(lifespan=lifespan)


@app.middleware("http")
async def authenticate(request, call_next):
    token = os.environ.get("TEXTCLS_TOKEN", "")
    if token and not secrets.compare_digest(request.headers.get("authorization", ""), "Bearer " + token):
        return JSONResponse({"error": "unauthorized"}, status_code=401)
    length = request.headers.get("content-length")
    if length and (not length.isdigit() or int(length) > MAX_BYTES):
        return JSONResponse({"error": "request too large"}, status_code=413)
    return await call_next(request)


@app.get("/health")
def health():
    return {"ok": True, "device": runtime.device}


@app.get("/v1/models")
def list_models():
    import onnxruntime as ort

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
    return {"models": models, "providers": ort.get_available_providers(), "max_batch": MAX_BATCH}


@app.post("/v1/models/manage")
async def manage_model(request: Request):
    if not os.environ.get("TEXTCLS_TOKEN"):
        raise HTTPException(503, "model management requires TEXTCLS_TOKEN")
    data = await request.json()
    name, action = data.get("model"), data.get("action")
    if name not in MODELS or action not in ("install", "load", "unload", "test", "remove"):
        raise HTTPException(422, "unsupported model operation")

    def operate():
        if not runtime.lock.acquire(blocking=False):
            raise HTTPException(409, "model is actively used")
        try:
            if action == "install":
                return install(name)
            if action in ("load", "test"):
                installed(name, verify=True)
                runtime.engine.session(name)
                return {"loaded": True, "device": runtime.device}
            runtime.engine.sessions.pop(name, None)
            runtime.engine.tokenizers.pop(name, None)
            runtime.engine.configs.pop(name, None)
            if action == "remove":
                import shutil
                from models import ROOT

                directory = (ROOT / name).resolve()
                if directory.parent != ROOT.resolve() or (ROOT / name).is_symlink():
                    raise HTTPException(422, "invalid model directory")
                if directory.exists():
                    shutil.rmtree(directory)
            return {"status": "completed", "action": action, "model": name}
        finally:
            runtime.lock.release()

    return await asyncio.get_running_loop().run_in_executor(executor, operate)


@app.post("/v1/classify")
async def classify(request: Request):
    body = bytearray()
    async for chunk in request.stream():
        body.extend(chunk)
        if len(body) > MAX_BYTES:
            raise HTTPException(413, "request too large")
    try:
        data = __import__("json").loads(bytes(body) or b"{}")
    except Exception as exc:
        raise HTTPException(422, "invalid json") from exc
    items = data.get("items", [])
    if not isinstance(items, list) or len(items) > MAX_BATCH:
        raise HTTPException(422, f"provide 0–{MAX_BATCH} items")
    if len(items) == 0:
        return {"items": []}
    for item in items:
        if not isinstance(item, dict) or "id" not in item:
            raise HTTPException(422, "each item needs id and text")
    sentiment_model = data.get("sentiment_model") or DEFAULT_SENTIMENT
    toxicity_model = data.get("toxicity_model") or DEFAULT_TOXICITY
    if sentiment_model not in MODELS or toxicity_model not in MODELS:
        raise HTTPException(422, "unsupported model")
    # Fail closed when weights are missing so ml_jobs land in waiting_model.
    try:
        installed(sentiment_model)
        installed(toxicity_model)
    except ModelUnavailable as exc:
        raise HTTPException(503, detail={"code": "waiting_model", "message": str(exc)}) from exc

    def run():
        return runtime.classify_batch(items, sentiment_model, toxicity_model)

    try:
        results = await asyncio.get_running_loop().run_in_executor(executor, run)
        return {"items": results}
    except ModelUnavailable as exc:
        raise HTTPException(503, detail={"code": "waiting_model", "message": str(exc)}) from exc
    except (ValueError, KeyError, TypeError) as exc:
        raise HTTPException(422, detail=str(exc)) from exc
    except Exception as exc:
        raise HTTPException(503, detail={"code": "inference_failed", "message": str(exc)}) from exc
