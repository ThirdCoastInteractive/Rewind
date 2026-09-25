"""Pinned Nemotron 3 Diarization weights. Inference never downloads them."""
import hashlib
import json
import os
from pathlib import Path
import tempfile
import urllib.request

# Hub revision of the 2026-09-23 general-access snapshot.
REVISION = "a435e9867d79e789e90053f9b6d6834053af564a"
SAFETENSORS_SHA256 = "c074d86335b3b794f8fa5edc25594558f128bdb3914d27806a3a5a2e44963cb6"

MODELS = {
    "nemotron-3": {
        "repo": "nvidia/Nemotron-3-Diarization",
        "revision": REVISION,
        "license": "OpenMDW-1.1",
        "recipe": "nemotron3-diarization-offline-30.4s-v1",
        "kind": "diarization",
        "files": {
            "config.json": "config.json",
            "processor_config.json": "processor_config.json",
            "model.safetensors": "model.safetensors",
        },
    },
}
ROOT = Path(os.environ.get("DIARIZE_MODEL_DIR", "/models/diarize"))
VERIFIED = {}


class ModelUnavailable(Exception):
    pass


def digest(path):
    hashed = hashlib.sha256()
    with path.open("rb") as handle:
        for block in iter(lambda: handle.read(1024 * 1024), b""):
            hashed.update(block)
    return hashed.hexdigest()


def installed(name, verify=False):
    if name not in MODELS:
        raise ModelUnavailable("unsupported model")
    directory = ROOT / name
    try:
        manifest = json.loads((directory / "manifest.json").read_text())
        spec = MODELS[name]
        fingerprint = manifest.pop("fingerprint")
        body = json.dumps(manifest, sort_keys=True).encode()
        if hashlib.sha256(body).hexdigest() != fingerprint:
            raise ValueError("manifest fingerprint mismatch")
        manifest["fingerprint"] = fingerprint
        if manifest["revision"] != spec["revision"] or manifest["recipe"] != spec["recipe"]:
            raise ValueError("model revision or recipe mismatch")
        local_files = list(spec["files"])
        stamps = tuple(
            (file, (directory / file).stat().st_size, (directory / file).stat().st_mtime_ns) for file in local_files
        )
        verify = verify or VERIFIED.get(name) != (fingerprint, stamps)
        for file in local_files:
            path = directory / file
            recorded = manifest["files"][file]
            if not path.is_file() or path.stat().st_size != recorded["size"]:
                raise ValueError(f"missing or incomplete {file}")
            if verify and digest(path) != recorded["sha256"]:
                raise ValueError(f"checksum mismatch {file}")
            if file == "model.safetensors" and recorded["sha256"] != SAFETENSORS_SHA256:
                raise ValueError("safetensors checksum is not the pinned revision")
        VERIFIED[name] = (fingerprint, stamps)
        return manifest
    except (OSError, ValueError, KeyError) as exc:
        raise ModelUnavailable(f"waiting_model: {name}: {exc}") from exc


def install(name):
    if name not in MODELS:
        raise ModelUnavailable("unsupported model")
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
        url = f"https://huggingface.co/{spec['repo']}/resolve/{spec['revision']}/{remote}"
        fd, temporary = tempfile.mkstemp(dir=target.parent, prefix=".install-")
        try:
            with os.fdopen(fd, "wb") as out, urllib.request.urlopen(url, timeout=300) as source:
                while block := source.read(1024 * 1024):
                    out.write(block)
            file_digest = digest(Path(temporary))
            if local == "model.safetensors" and file_digest != SAFETENSORS_SHA256:
                raise ModelUnavailable("safetensors checksum mismatch; refusing install")
            os.replace(temporary, target)
        finally:
            if os.path.exists(temporary):
                os.unlink(temporary)
        manifest["files"][local] = {"size": target.stat().st_size, "sha256": digest(target)}
    manifest["fingerprint"] = hashlib.sha256(json.dumps(manifest, sort_keys=True).encode()).hexdigest()
    temporary = directory / "manifest.json.new"
    temporary.write_text(json.dumps(manifest, indent=2))
    os.replace(temporary, directory / "manifest.json")
    return installed(name, verify=True)
