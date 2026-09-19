# Incremental container builds

`make up` is the normal command. It starts the stack and rebuilds only the
services whose sources are newer than the image currently loaded in Docker.

```bash
make up          # rebuild stale services, start everything
make up-fast     # start without rebuilding
make up-web      # force rebuild rewind
make up-ml       # force rebuild rewind-ml
```

These initialize and reuse the `rewind` Buildx builder. They do not switch the
global builder or restart Docker Engine. `BUILDX_BUILDER=another-builder` selects
an existing builder instead. Direct Compose calls can use the same environment
variable after `make build-cache`.

The builder uses Docker's container driver with automatic image loading, so
Compose sees the built images normally. Its dedicated cache volume retains
48 GiB, permits up to 64 GiB, and targets 20 GiB of free disk space. The policy is
in `docker/buildkitd.toml`; it is applied when the builder is created. Existing
builders are never silently removed or reconfigured. Changing this file requires
creating a new named builder or deliberately maintaining the existing builder.

The first build in a new builder is cold. Subsequent application changes reuse
Whisper/CUDA compilation and package installation. Deleting the builder's state,
pruning its cache, changing dependency build arguments, or exceeding its cache
budget can still require rebuilding dependencies. Model weights, Ollama, and the
vision venv live in `./bin/models` and `./bin/runtime` and are not affected by
image rebuilds.

`rewind-ml` no longer bakes Ollama CUDA runners or onnxruntime wheels into the
image. Those download into `./bin/runtime` on first container start. The image
is the Go worker, whisper.cpp, ffmpeg, and a CUDA/CPU base. A Go-only change is
a small layer; Docker Desktop still has to load the new image ID from the Buildx
container, which is why `make up` skips `rewind-ml` when `cmd/ml` has not
changed.

## What changed

- Ollama and the vision Python environment are fetched at startup, not at build.
- Application binaries use `COPY --link`, keeping their layer independent.
- All Go service builders share module and compiler cache mounts.
- Whisper's source and CUDA build stages live only in `ml.Dockerfile` and stay independent of Go sources.
- Private worktrees, test results and browser artifacts are excluded from context.
- Normal service builds no longer build unused local ffmpeg base images first.
  Service Dockerfiles continue using public bases and build standalone in CI;
  existing CI caches remain separate from this local builder.

Docker references: [cache optimization](https://docs.docker.com/build/cache/optimize/),
[container builder persistence](https://docs.docker.com/build/builders/drivers/docker-container/),
and [BuildKit cache policy](https://docs.docker.com/build/buildkit/toml-configuration/).

## Verify source-only rebuilds

`make test-build-cache` builds ML in a disposable source context, adds a Go
source file there, and rebuilds. It fails if the Go build stays cached or any
Whisper/CUDA/runtime dependency step executes again. It never changes the
working tree or deploys its test images. Logs are saved under the OS temporary
directory in `rewind-cache-checks`. Requires Python 3; use `PYTHON=python3` where
appropriate. A cold cache must compile dependencies once before this check can
demonstrate reuse. Run after other builds finish: BuildKit can replay historical
progress for shared in-flight work without marking those vertices `CACHED`.
