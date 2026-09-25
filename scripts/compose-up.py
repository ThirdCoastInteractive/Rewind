"""Start the Rewind stack, rebuilding only services whose sources are newer than the image.

`make up` is the normal path. Pass --rebuild to force every service, --force
SERVICE to force one, or --no-build to start whatever is already built.
"""
from __future__ import annotations

import argparse
import os
import subprocess
import sys
from datetime import datetime, timezone
from pathlib import Path


ROOT = Path(__file__).resolve().parent.parent

# What actually lands in each image. cmd/ml changes do not rebuild rewind.
SOURCES = {
    "postgres": [
        "docker/postgres-vector.Dockerfile",
    ],
    "rewind": [
        "rewind.Dockerfile",
        "go.mod",
        "go.sum",
        "cmd/web",
        "cmd/show-note-workspace",
        "cmd/downloader",
        "cmd/ingest",
        "cmd/encoder",
        "cmd/sfu",
        "cmd/pg-migrator",
        "pkg",
        "internal",
        "static/dist",
    ],
    "rewind-ml": [
        "ml.Dockerfile",
        "go.mod",
        "go.sum",
        "cmd/ml",
        "internal",
        "pkg",
        "services/vision",
        "services/textcls",
        "services/alignment",
        "services/diarize",
    ],
}

SKIP_DIRS = {".git", "__pycache__", "node_modules", "testdata", ".venv"}

HOST_DIRS = [
    "bin/runtime",
    "bin/models/whisper",
    "bin/models/ollama",
    "bin/models/vision",
    "bin/models/diarize",
    "bin/spool",
    "bin/exports",
]


def load_dotenv(path: Path) -> dict[str, str]:
    values: dict[str, str] = {}
    if not path.is_file():
        return values
    for raw in path.read_text(encoding="utf-8").splitlines():
        line = raw.strip()
        if not line or line.startswith("#") or "=" not in line:
            continue
        key, value = line.split("=", 1)
        values[key.strip()] = value.strip().strip('"').strip("'")
    return values


def merged_env() -> dict[str, str]:
    env = load_dotenv(ROOT / ".env")
    env.update({k: v for k, v in os.environ.items() if v})
    return env


def wants_gpu(env: dict[str, str]) -> bool:
    runtime = env.get("ML_RUNTIME", "").lower()
    device = env.get("WHISPER_DEVICE", env.get("VISION_DEVICE", "")).lower()
    return "cuda" in runtime or "rocm" in runtime or device in {"cuda", "rocm"}


def compose_env() -> dict[str, str]:
    env = os.environ.copy()
    env.setdefault("BUILDX_BUILDER", "rewind")
    if env.get("COMPOSE_FILE"):
        return env
    files = ["docker-compose.yml"]
    if wants_gpu(merged_env()):
        files.append("docker-compose.gpu.yml")
    override = ROOT / "docker-compose.override.yml"
    if override.is_file() and override.name not in files:
        files.append(override.name)
    env["COMPOSE_FILE"] = os.pathsep.join(files)
    return env


def compose(args: list[str], env: dict[str, str], check: bool = True) -> subprocess.CompletedProcess:
    cmd = ["docker", "compose", *args]
    print("+", " ".join(cmd), flush=True)
    return subprocess.run(cmd, cwd=ROOT, env=env, check=check)


def parse_created(raw: str) -> float | None:
    text = raw.strip()
    if not text:
        return None
    if text.endswith("Z"):
        text = text[:-1] + "+00:00"
    if "." in text:
        head, rest = text.split(".", 1)
        digits = ""
        tz = ""
        for i, ch in enumerate(rest):
            if ch.isdigit():
                digits += ch
            else:
                tz = rest[i:]
                break
        text = head + "." + (digits + "000000")[:6] + tz
    try:
        dt = datetime.fromisoformat(text)
    except ValueError:
        return None
    if dt.tzinfo is None:
        dt = dt.replace(tzinfo=timezone.utc)
    return dt.timestamp()


def newest_mtime(rel_paths: list[str]) -> float:
    newest = 0.0
    for rel in rel_paths:
        path = ROOT / rel
        if path.is_file():
            newest = max(newest, path.stat().st_mtime)
            continue
        if not path.is_dir():
            continue
        for dirpath, dirnames, filenames in os.walk(path):
            dirnames[:] = [name for name in dirnames if name not in SKIP_DIRS]
            for name in filenames:
                newest = max(newest, (Path(dirpath) / name).stat().st_mtime)
    return newest


def image_created(service: str, env: dict[str, str]) -> float | None:
    proc = subprocess.run(
        ["docker", "compose", "images", "-q", service],
        cwd=ROOT,
        env=env,
        capture_output=True,
        text=True,
        check=False,
    )
    ids = [line.strip() for line in proc.stdout.splitlines() if line.strip()]
    if not ids:
        return None
    inspect = subprocess.run(
        ["docker", "inspect", "-f", "{{.Created}}", ids[-1]],
        capture_output=True,
        text=True,
        check=False,
    )
    if inspect.returncode != 0:
        return None
    return parse_created(inspect.stdout)


def stale_services(env: dict[str, str], force: set[str], rebuild_all: bool) -> list[str]:
    stale: list[str] = []
    for service, paths in SOURCES.items():
        if rebuild_all or service in force:
            stale.append(service)
            continue
        created = image_created(service, env)
        src = newest_mtime(paths)
        if created is None:
            print(f"{service}: no image, will build", flush=True)
            stale.append(service)
        elif src > created:
            print(f"{service}: sources newer than image, will rebuild", flush=True)
            stale.append(service)
        else:
            print(f"{service}: up to date", flush=True)
    return stale


def ensure_host_dirs() -> None:
    for rel in HOST_DIRS:
        (ROOT / rel).mkdir(parents=True, exist_ok=True)


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--rebuild", action="store_true", help="rebuild every service")
    parser.add_argument("--force", action="append", default=[], metavar="SERVICE",
                        help="force rebuild of one service (repeatable)")
    parser.add_argument("--no-build", action="store_true", help="never rebuild, just start")
    parser.add_argument("--dry-run", action="store_true", help="print rebuild decisions and exit")
    args = parser.parse_args()

    os.chdir(ROOT)
    env = compose_env()
    ensure_host_dirs()
    files = env.get("COMPOSE_FILE", "docker-compose.yml")
    print(f"compose files: {files}", flush=True)

    if args.no_build:
        stale = []
    else:
        stale = stale_services(env, set(args.force), args.rebuild)

    if args.dry_run:
        print("rebuild:", ", ".join(stale) or "(none)", flush=True)
        return 0

    if stale:
        compose(["build", *stale], env)
    compose(["up", "-d", "--remove-orphans"], env)
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except subprocess.CalledProcessError as err:
        raise SystemExit(err.returncode) from err
