# Retired. Ingest no longer FROMs a Python openai-whisper image.
# Build ingest.Dockerfile --target runtime-cpu (or runtime-cuda / runtime-rocm).
# GGML weights download at runtime into /models.
FROM alpine:3.21
RUN echo "docker/runtime-whisper.Dockerfile is retired. Use ingest.Dockerfile --target runtime-cpu." >&2 && exit 1
