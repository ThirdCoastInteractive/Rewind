# syntax=docker/dockerfile:1
#
# Model-agnostic ingest image. GGML weights are NOT baked in — the worker
# downloads WHISPER_MODEL into /models at runtime.
#
#   docker build -f ingest.Dockerfile --target runtime-cpu  -t rewind-ingest:cpu  .
#   docker build -f ingest.Dockerfile --target runtime-cuda -t rewind-ingest:cuda .
#   docker build -f ingest.Dockerfile --target runtime-rocm -t rewind-ingest:rocm .
#
# Compose selects the target with INGEST_RUNTIME (default runtime-cpu).

ARG WHISPER_CPP_REF=v1.7.6
ARG GO_VERSION=1.26

# ==============================================================================
# Go worker
# ==============================================================================
FROM golang:${GO_VERSION}-alpine AS go-builder
WORKDIR /app
COPY go.mod go.sum* ./
RUN go mod download
COPY cmd/ingest ./cmd/ingest
COPY internal ./internal
COPY pkg ./pkg
RUN CGO_ENABLED=0 GOOS=linux go build -o ingest ./cmd/ingest

# ==============================================================================
# whisper.cpp builders (compile only; binary is copied into slim runtimes)
# ==============================================================================
FROM alpine:3.21 AS whisper-cpu-builder
ARG WHISPER_CPP_REF
RUN apk add --no-cache git cmake make g++ linux-headers
RUN git clone --depth 1 --branch ${WHISPER_CPP_REF} https://github.com/ggml-org/whisper.cpp.git /src
WORKDIR /src
RUN cmake -B build -DGGML_NATIVE=OFF -DWHISPER_BUILD_EXAMPLES=ON && \
    cmake --build build -j"$(getconf _NPROCESSORS_ONLN)" && \
    (cp build/bin/whisper-cli /whisper-cli || cp build/bin/main /whisper-cli)

FROM nvidia/cuda:12.4.1-devel-ubuntu22.04 AS whisper-cuda-builder
ARG WHISPER_CPP_REF
RUN apt-get update && apt-get install -y --no-install-recommends git cmake make g++ ca-certificates && \
    rm -rf /var/lib/apt/lists/*
RUN git clone --depth 1 --branch ${WHISPER_CPP_REF} https://github.com/ggml-org/whisper.cpp.git /src
WORKDIR /src
RUN cmake -B build -DGGML_CUDA=ON -DWHISPER_BUILD_EXAMPLES=ON && \
    cmake --build build -j"$(nproc)" && \
    (cp build/bin/whisper-cli /whisper-cli || cp build/bin/main /whisper-cli)

FROM rocm/dev-ubuntu-22.04:6.1 AS whisper-rocm-builder
ARG WHISPER_CPP_REF
RUN apt-get update && apt-get install -y --no-install-recommends git cmake make g++ ca-certificates && \
    rm -rf /var/lib/apt/lists/*
RUN git clone --depth 1 --branch ${WHISPER_CPP_REF} https://github.com/ggml-org/whisper.cpp.git /src
WORKDIR /src
# gfx906=MI50, gfx90a=MI200, gfx1030=RX 6000, gfx1100=RX 7000
RUN cmake -B build -DGGML_HIP=ON -DAMDGPU_TARGETS="gfx906;gfx90a;gfx1030;gfx1100" -DWHISPER_BUILD_EXAMPLES=ON && \
    cmake --build build -j"$(nproc)" && \
    (cp build/bin/whisper-cli /whisper-cli || cp build/bin/main /whisper-cli)

# ==============================================================================
# TARGET: CPU (Alpine)
# ==============================================================================
FROM alpine:3.21 AS runtime-cpu
LABEL org.opencontainers.image.source="https://github.com/ThirdCoastInteractive/Rewind"
LABEL org.opencontainers.image.description="Rewind ingest worker (whisper.cpp CPU)"
LABEL org.opencontainers.image.licenses="MIT"
RUN apk add --no-cache ffmpeg ca-certificates curl
RUN addgroup -g 1000 appuser && adduser -D -u 1000 -G appuser appuser
WORKDIR /app
RUN mkdir -p /spool /models && chown -R appuser:appuser /spool /models
COPY --from=whisper-cpu-builder /whisper-cli /usr/local/bin/whisper-cli
COPY --from=go-builder /app/ingest ./
USER appuser
ENV WHISPER_CMD=whisper-cli
ENV WHISPER_MODEL_DIR=/models
VOLUME ["/models"]
CMD ["./ingest"]

# ==============================================================================
# TARGET: NVIDIA CUDA
# ==============================================================================
FROM nvidia/cuda:12.4.1-runtime-ubuntu22.04 AS runtime-cuda
LABEL org.opencontainers.image.source="https://github.com/ThirdCoastInteractive/Rewind"
LABEL org.opencontainers.image.description="Rewind ingest worker (whisper.cpp CUDA)"
LABEL org.opencontainers.image.licenses="MIT"
RUN apt-get update && apt-get install -y --no-install-recommends ffmpeg ca-certificates curl && \
    rm -rf /var/lib/apt/lists/*
RUN groupadd -g 1000 appuser && useradd -u 1000 -g appuser -m -s /bin/bash appuser
WORKDIR /app
RUN mkdir -p /spool /models && chown -R appuser:appuser /spool /models
COPY --from=whisper-cuda-builder /whisper-cli /usr/local/bin/whisper-cli
COPY --from=go-builder /app/ingest ./
USER appuser
ENV WHISPER_CMD=whisper-cli
ENV WHISPER_MODEL_DIR=/models
VOLUME ["/models"]
CMD ["./ingest"]

# ==============================================================================
# TARGET: AMD ROCm
# ==============================================================================
FROM rocm/dev-ubuntu-22.04:6.1 AS runtime-rocm
LABEL org.opencontainers.image.source="https://github.com/ThirdCoastInteractive/Rewind"
LABEL org.opencontainers.image.description="Rewind ingest worker (whisper.cpp ROCm)"
LABEL org.opencontainers.image.licenses="MIT"
RUN apt-get update && apt-get install -y --no-install-recommends ffmpeg ca-certificates curl && \
    rm -rf /var/lib/apt/lists/*
RUN groupadd -g 1000 appuser && useradd -u 1000 -g appuser -m -s /bin/bash appuser
WORKDIR /app
RUN mkdir -p /spool /models && chown -R appuser:appuser /spool /models
COPY --from=whisper-rocm-builder /whisper-cli /usr/local/bin/whisper-cli
COPY --from=go-builder /app/ingest ./
USER appuser
ENV WHISPER_CMD=whisper-cli
ENV WHISPER_MODEL_DIR=/models
VOLUME ["/models"]
CMD ["./ingest"]

# Default when compose omits `target:` (CPU).
FROM runtime-cpu
