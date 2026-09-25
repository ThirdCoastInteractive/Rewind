"""Explicit, revision-pinned model installation; inference never downloads weights."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import tempfile
import urllib.request

MODELS = {
    "ViT-B-32__openai": {
        "revision": "a857c8de2c07bbcfa6646adfcf31b798845afa1e",
        "license": "OpenAI CLIP MIT",
        "recipe": "openclip-rgb-center-crop-v1",
        "files": ["config.json", "visual/model.onnx", "visual/preprocess_cfg.json", "textual/model.onnx", "textual/tokenizer.json", "textual/tokenizer_config.json"],
    },
}
ROOT = Path(os.environ.get("VISION_MODEL_DIR", "/models/vision"))
VERIFIED = {}

class ModelUnavailable(Exception):
    pass

def digest(path):
    h = hashlib.sha256()
    with path.open("rb") as f:
        for block in iter(lambda: f.read(1024 * 1024), b""):
            h.update(block)
    return h.hexdigest()

def installed(name, verify=False):
    if name not in MODELS:
        raise ModelUnavailable("unsupported model")
    directory = ROOT / name
    try:
        manifest = json.loads((directory / "manifest.json").read_text())
        spec = MODELS[name]
        fingerprint = manifest.pop("fingerprint")
        if hashlib.sha256(json.dumps(manifest, sort_keys=True).encode()).hexdigest() != fingerprint:
            raise ValueError("manifest fingerprint mismatch")
        manifest["fingerprint"] = fingerprint
        if manifest["revision"] != spec["revision"] or manifest["recipe"] != spec["recipe"]:
            raise ValueError("model revision or recipe mismatch")
        stamps = tuple((file, (directory / file).stat().st_size, (directory / file).stat().st_mtime_ns) for file in spec["files"])
        verify = verify or VERIFIED.get(name) != (fingerprint, stamps)
        for file in spec["files"]:
            path = directory / file
            if not path.is_file() or path.stat().st_size != manifest["files"][file]["size"]:
                raise ValueError(f"missing or incomplete {file}")
            if verify and digest(path) != manifest["files"][file]["sha256"]:
                raise ValueError(f"checksum mismatch {file}")
        VERIFIED[name] = (fingerprint, stamps)
        return manifest
    except (OSError, ValueError, KeyError) as exc:
        raise ModelUnavailable(f"waiting_model: {name}: {exc}") from exc

def install(name):
    spec = MODELS[name]
    directory = ROOT / name
    directory.mkdir(parents=True, exist_ok=True)
    manifest = {"name": name, "revision": spec["revision"], "recipe": spec["recipe"], "license": spec["license"], "dimensions": 512, "files": {}}
    for file in spec["files"]:
        target = directory / file
        target.parent.mkdir(parents=True, exist_ok=True)
        url = f"https://huggingface.co/immich-app/{name}/resolve/{spec['revision']}/{file}"
        fd, temporary = tempfile.mkstemp(dir=target.parent, prefix=".install-")
        try:
            # The installer may run as root while the vision service runs as
            # an unprivileged user. Publish public model artifacts as
            # world-readable files before the atomic rename.
            os.fchmod(fd, 0o644)
            with os.fdopen(fd, "wb") as out, urllib.request.urlopen(url, timeout=120) as source:
                while block := source.read(1024 * 1024):
                    out.write(block)
            os.replace(temporary, target)
        finally:
            if os.path.exists(temporary):
                os.unlink(temporary)
        manifest["files"][file] = {"size": target.stat().st_size, "sha256": digest(target)}
    manifest["fingerprint"] = hashlib.sha256(json.dumps(manifest, sort_keys=True).encode()).hexdigest()
    temporary = directory / "manifest.json.new"
    temporary.write_text(json.dumps(manifest, indent=2))
    temporary.chmod(0o644)
    os.replace(temporary, directory / "manifest.json")
    return manifest

if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("name", choices=MODELS)
    args = parser.parse_args()
    print(json.dumps(install(args.name), indent=2))
