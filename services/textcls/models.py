"""Explicit, revision-pinned model installation; inference never downloads weights."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import tempfile
import urllib.request

MODELS = {
    "twitter-roberta-sentiment": {
        "repo": "Xenova/twitter-roberta-base-sentiment-latest",
        "revision": "f3ec4d0925f90c3ca7ee7814f52d6ee7cf180445",
        "license": "CC-BY-4.0 (cardiffnlp/twitter-roberta-base-sentiment-latest)",
        "recipe": "roberta-sentiment-neg-neu-pos-v1",
        "kind": "sentiment",
        "files": {
            "config.json": "config.json",
            "tokenizer.json": "tokenizer.json",
            "tokenizer_config.json": "tokenizer_config.json",
            "model.onnx": "onnx/model.onnx",
        },
    },
    "unbiased-toxic-roberta": {
        "repo": "protectai/unbiased-toxic-roberta-onnx",
        "revision": "16fd63c0e51000f407fb7d28ce655e41495ebc8b",
        "license": "Apache-2.0 (unitary/unbiased-toxic-roberta)",
        "recipe": "roberta-multilabel-toxicity-v1",
        "kind": "toxicity",
        "files": {
            "config.json": "config.json",
            "tokenizer.json": "tokenizer.json",
            "tokenizer_config.json": "tokenizer_config.json",
            "model.onnx": "model.onnx",
        },
    },
}
ROOT = Path(os.environ.get("TEXTCLS_MODEL_DIR", "/models/textcls"))
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
        local_files = list(spec["files"])
        stamps = tuple((file, (directory / file).stat().st_size, (directory / file).stat().st_mtime_ns) for file in local_files)
        verify = verify or VERIFIED.get(name) != (fingerprint, stamps)
        for file in local_files:
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
    manifest = {
        "name": name,
        "revision": spec["revision"],
        "recipe": spec["recipe"],
        "license": spec["license"],
        "kind": spec["kind"],
        "files": {},
    }
    for local, remote in spec["files"].items():
        target = directory / local
        target.parent.mkdir(parents=True, exist_ok=True)
        url = f"https://huggingface.co/{spec['repo']}/resolve/{spec['revision']}/{remote}"
        fd, temporary = tempfile.mkstemp(dir=target.parent, prefix=".install-")
        try:
            with os.fdopen(fd, "wb") as out, urllib.request.urlopen(url, timeout=120) as source:
                while block := source.read(1024 * 1024):
                    out.write(block)
            os.replace(temporary, target)
        finally:
            if os.path.exists(temporary):
                os.unlink(temporary)
        manifest["files"][local] = {"size": target.stat().st_size, "sha256": digest(target)}
    manifest["fingerprint"] = hashlib.sha256(json.dumps(manifest, sort_keys=True).encode()).hexdigest()
    temporary = directory / "manifest.json.new"
    temporary.write_text(json.dumps(manifest, indent=2))
    os.replace(temporary, directory / "manifest.json")
    return manifest


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("name", choices=MODELS)
    args = parser.parse_args()
    print(json.dumps(install(args.name), indent=2))
