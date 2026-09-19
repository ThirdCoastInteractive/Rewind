"""Exercise Go-only rebuilds in disposable contexts; never modify or deploy the app."""
import argparse
import pathlib
import re
import shutil
import subprocess
import tempfile
import time
import uuid


def build(root, service, builder, log):
    started = time.monotonic()
    with log.open("w", encoding="utf-8") as output:
        subprocess.run(
            ["docker", "buildx", "build", "--builder", builder,
             "--file", str(root / f"{service}.Dockerfile"),
             "--target", "runtime-cuda", "--output", "type=cacheonly",
             "--progress", "plain", str(root)],
            stdout=output, stderr=subprocess.STDOUT, check=True,
        )
    return time.monotonic() - started


def verify(log):
    text = log.read_text(encoding="utf-8")
    steps = {}
    cached = set()
    for line in text.splitlines():
        step = re.match(r"#(\d+) \[([^]]+)\] (.*)", line)
        if step:
            steps[step[1]] = (step[2], step[3])
        hit = re.match(r"#(\d+) CACHED", line)
        if hit:
            cached.add(hit[1])
    rebuilt_go = False
    checked_runtime = 0
    for key, (stage, instruction) in steps.items():
        if stage.startswith("go-builder") and "go build" in instruction:
            rebuilt_go = key not in cached
        if (stage.startswith("whisper-cuda-builder") or stage.startswith("runtime-cuda")):
            # Base image metadata checks and the changed application copy are expected.
            if instruction.startswith("FROM ") or "--from=go-builder" in instruction:
                continue
            checked_runtime += 1
            if key not in cached:
                raise RuntimeError(f"Dependency not reported as cached: {stage}: {instruction}; "
                                   f"finish any concurrent builds before this check; see {log}")
    if not rebuilt_go or checked_runtime == 0:
        raise RuntimeError(f"Build did not exercise a Go-only change and cached runtime: {log}")
    return checked_runtime


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--builder", default="rewind")
    parser.add_argument("--service", choices=["ml"], action="append")
    args = parser.parse_args()
    repo = pathlib.Path(__file__).resolve().parent.parent
    logs = pathlib.Path(tempfile.gettempdir()) / "rewind-cache-checks"
    logs.mkdir(exist_ok=True)
    for service in args.service or ["ml"]:
        with tempfile.TemporaryDirectory(prefix="rewind-cache-check-") as scratch:
            root = pathlib.Path(scratch)
            for filename in ["go.mod", "go.sum", f"{service}.Dockerfile"]:
                shutil.copy2(repo / filename, root / filename)
            for directory in ["internal", "pkg", f"cmd/{service}", "services/vision"]:
                shutil.copytree(repo / directory, root / directory)
            print(f"{service}: warming baseline (logs: {logs})", flush=True)
            baseline = build(root, service, args.builder, logs / f"{service}-baseline.log")
            # A real source change, confined to the disposable context.
            (root / f"cmd/{service}/rewind_cache_probe.go").write_text(
                f'package main\nvar rewindCacheProbe = "{uuid.uuid4().hex}"\n', encoding="utf-8"
            )
            print(f"{service}: checking changed Go source", flush=True)
            changed_log = logs / f"{service}-changed.log"
            changed = build(root, service, args.builder, changed_log)
            count = verify(changed_log)
            print(f"PASS {service}: {count} dependency steps cached; "
                  f"baseline {baseline:.1f}s, Go change {changed:.1f}s", flush=True)


if __name__ == "__main__":
    main()
