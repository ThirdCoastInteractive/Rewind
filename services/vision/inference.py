"""ONNX inference using published model configuration."""
import json
from io import BytesIO
import numpy as np
import onnxruntime as ort
from PIL import Image, ImageOps
from tokenizers import Tokenizer
from models import ROOT, installed, ModelUnavailable

Image.MAX_IMAGE_PIXELS = 24_000_000

def array_string(vector):
    vector = np.asarray(vector, dtype=np.float32).reshape(-1)
    if len(vector) != 512 or not np.isfinite(vector).all() or np.linalg.norm(vector) == 0:
        raise ValueError("invalid embedding")
    return json.dumps(vector.tolist(), separators=(",", ":"), allow_nan=False)

class Engine:
    def __init__(self, device="cpu"):
        self.device = device
        if device == "cuda" and "CUDAExecutionProvider" not in ort.get_available_providers():
            raise RuntimeError("CUDA provider unavailable")
        self.providers = ["CUDAExecutionProvider", "CPUExecutionProvider"] if device == "cuda" else ["CPUExecutionProvider"]
        self.sessions = {}
        self.tokenizers = {}

    def session(self, name, component):
        key = (name, component)
        if key not in self.sessions:
            installed(name)
            opts = ort.SessionOptions()
            opts.intra_op_num_threads = 2
            opts.inter_op_num_threads = 1
            session = ort.InferenceSession(str(ROOT / name / component / "model.onnx"), sess_options=opts, providers=self.providers)
            if self.device == "cuda" and session.get_providers()[0] != "CUDAExecutionProvider":
                raise RuntimeError("CUDA session unexpectedly fell back to CPU")
            self.sessions[key] = session
        return self.sessions[key]

    def clip(self, name, component, payload):
        if name != "ViT-B-32__openai":
            raise ModelUnavailable("unsupported CLIP model")
        session = self.session(name, component)
        if component == "visual":
            cfg = json.loads((ROOT / name / "visual/preprocess_cfg.json").read_text())
            size = cfg["size"]
            size = size[0] if isinstance(size, list) else size
            w, h = payload.size
            target = (size, int(h / w * size)) if w < h else (int(w / h * size), size)
            interpolation = getattr(Image.Resampling, cfg["interpolation"].upper())
            image = payload.resize(target, interpolation)
            x, y = int(round((image.width - size) / 2)), int(round((image.height - size) / 2))
            image = image.crop((x, y, x + size, y + size))
            pixels = np.asarray(image, dtype=np.float32) / 255.0
            pixels = (pixels - np.array(cfg["mean"], dtype=np.float32)) / np.array(cfg["std"], dtype=np.float32)
            tensor = pixels.transpose(2, 0, 1)[None]
        elif component == "textual":
            if name not in self.tokenizers:
                cfg = json.loads((ROOT / name / "textual/tokenizer_config.json").read_text())
                token = Tokenizer.from_file(str(ROOT / name / "textual/tokenizer.json"))
                pad = cfg["pad_token"]
                token.enable_padding(length=77, pad_id=token.token_to_id(pad), pad_token=pad)
                token.enable_truncation(max_length=77)
                self.tokenizers[name] = token
            tensor = np.array([self.tokenizers[name].encode(" ".join(payload.split())).ids], dtype=np.int32)
        else:
            raise ValueError("unsupported CLIP component")
        return array_string(session.run(None, {session.get_inputs()[0].name: tensor})[0][0])

    def predict(self, entries, image=None, text=None):
        if (image is None) == (text is None):
            raise ValueError("provide exactly one image or text")
        payload = text
        if image is not None:
            with Image.open(BytesIO(image)) as im:
                if im.width * im.height > 24_000_000:
                    raise ValueError("image exceeds pixel limit")
                payload = ImageOps.exif_transpose(im).convert("RGB")
        out = {}
        if not entries or set(entries) - {"clip"}:
            raise ValueError("unsupported inference tasks")
        for task, components in entries.items():
            if task == "clip":
                component = "visual" if image is not None else "textual"
                if set(components) != {component}:
                    raise ValueError("CLIP component does not match payload")
                out[task] = self.clip(components[component]["modelName"], component, payload)
        if image is not None:
            out.update(imageWidth=payload.width, imageHeight=payload.height)
        return out
