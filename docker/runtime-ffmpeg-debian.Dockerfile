# syntax=docker/dockerfile:1
# Shared Debian runtime with ffmpeg. Built rarely — service images FROM this so
# day-to-day Go rebuilds never re-run apt-get install ffmpeg.
#
#   make runtime-bases
#   # or:
#   docker build -t rewind-runtime-ffmpeg-debian:local -f docker/runtime-ffmpeg-debian.Dockerfile docker/
FROM debian:12-slim

LABEL org.opencontainers.image.description="Rewind shared Debian runtime (ffmpeg)"

# BuildKit apt caches make even a cold rebuild of THIS base much faster.
RUN --mount=type=cache,target=/var/cache/apt,sharing=locked \
    --mount=type=cache,target=/var/lib/apt,sharing=locked \
    apt-get update && \
    apt-get install -y --no-install-recommends ca-certificates ffmpeg && \
    rm -rf /var/lib/apt/lists/*
