"""Bounded installed-model canary; never downloads weights or enrolls archive media."""
import json
import os
import time
import resource
from io import BytesIO
import numpy as np
from PIL import Image, ImageDraw
from inference import Engine
from models import installed, ModelUnavailable

def main():
    device = os.environ.get("VISION_DEVICE", "cpu")
    engine = Engine(device)
    images = []
    for color in ("red", "green", "blue"):
        im = Image.new("RGB", (640, 360), color)
        ImageDraw.Draw(im).rectangle((200, 100, 440, 260), fill="white")
        out = BytesIO(); im.save(out, "JPEG"); images.append(out.getvalue())
    started = time.monotonic()
    vectors = []
    for raw in images:
        result = engine.predict({"clip": {"visual": {"modelName": "ViT-B-32__openai"}}}, image=raw)
        vector = np.array(json.loads(result["clip"]))
        assert vector.shape == (512,) and np.isfinite(vector).all()
        vectors.append(vector / np.linalg.norm(vector))
    repeated = json.loads(engine.predict({"clip": {"visual": {"modelName": "ViT-B-32__openai"}}}, image=images[0])["clip"])
    assert np.dot(vectors[0], repeated / np.linalg.norm(repeated)) > .9999
    text = engine.predict({"clip": {"textual": {"modelName": "ViT-B-32__openai"}}}, text="a red background " * 200)
    assert len(json.loads(text["clip"])) == 512
    print(json.dumps({"device": device, "seconds": time.monotonic() - started, "peak_rss_kib": resource.getrusage(resource.RUSAGE_SELF).ru_maxrss, "clip": "passed"}))

if __name__ == "__main__":
    main()
