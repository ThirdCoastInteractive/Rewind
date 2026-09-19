# syntax=docker/dockerfile:1.7
#
# Thin rewind-ml image. GGML / Ollama / ONNX weights and GPU runners are NOT
# baked in. First container start downloads Ollama + a vision venv into
# /runtime (compose maps ./bin/runtime). Whisper.cpp is still compiled here
# because there is no official CUDA whisper-cli tarball.
#
#   docker build -f ml.Dockerfile --target runtime-cpu  -t rewind-ml:cpu  .
#   docker build -f ml.Dockerfile --target runtime-cuda -t rewind-ml:cuda .
#   docker build -f ml.Dockerfile --target runtime-rocm -t rewind-ml:rocm .

ARG WHISPER_CPP_REF=v1.7.6
ARG GO_VERSION=1.26

# ==============================================================================
# Go worker
# ==============================================================================
FROM golang:${GO_VERSION}-alpine AS go-builder
WORKDIR /app
COPY go.mod go.sum* ./
RUN --mount=type=cache,id=rewind-go-mod,target=/go/pkg/mod go mod download
COPY cmd/ml ./cmd/ml
# plugin builtins still import the cookie session manager.
COPY cmd/web/auth ./cmd/web/auth
COPY internal ./internal
COPY pkg ./pkg
RUN --mount=type=cache,id=rewind-go-mod,target=/go/pkg/mod --mount=type=cache,id=rewind-go-build,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux go build -o ml ./cmd/ml

# ==============================================================================
# whisper.cpp builders (compile only; binary is copied into slim runtimes)
# ==============================================================================
FROM ubuntu:22.04 AS whisper-cpu-builder
ARG WHISPER_CPP_REF
RUN apt-get update && apt-get install -y --no-install-recommends git cmake make g++ ca-certificates && rm -rf /var/lib/apt/lists/*
RUN git clone --depth 1 --branch ${WHISPER_CPP_REF} https://github.com/ggml-org/whisper.cpp.git /src
WORKDIR /src
RUN cmake -B build -DGGML_NATIVE=OFF -DBUILD_SHARED_LIBS=OFF -DWHISPER_BUILD_EXAMPLES=ON && \
    cmake --build build -j"$(getconf _NPROCESSORS_ONLN)" && \
    (cp build/bin/whisper-cli /whisper-cli || cp build/bin/main /whisper-cli)

FROM nvidia/cuda:12.4.1-devel-ubuntu22.04 AS whisper-cuda-builder
ARG WHISPER_CPP_REF
RUN apt-get update && apt-get install -y --no-install-recommends git cmake make g++ ca-certificates && \
    rm -rf /var/lib/apt/lists/*
RUN ln -sf /usr/local/cuda/lib64/stubs/libcuda.so /usr/local/cuda/lib64/stubs/libcuda.so.1
ENV LIBRARY_PATH=/usr/local/cuda/lib64/stubs
ENV LD_LIBRARY_PATH=/usr/local/cuda/lib64/stubs
RUN git clone --depth 1 --branch ${WHISPER_CPP_REF} https://github.com/ggml-org/whisper.cpp.git /src
WORKDIR /src
RUN cmake -B build -DGGML_CUDA=ON -DWHISPER_BUILD_EXAMPLES=ON && \
    cmake --build build -j"$(nproc)" --target whisper-cli && \
    cp build/bin/whisper-cli /whisper-cli
RUN mkdir -p /whisper-libs && \
    find /src/build -type f \( -name 'libwhisper.so*' -o -name 'libggml*.so*' \) -exec cp -a {} /whisper-libs/ \;

FROM rocm/dev-ubuntu-22.04:6.1 AS whisper-rocm-builder
ARG WHISPER_CPP_REF
RUN apt-get update && apt-get install -y --no-install-recommends git cmake make g++ ca-certificates && \
    rm -rf /var/lib/apt/lists/*
RUN git clone --depth 1 --branch ${WHISPER_CPP_REF} https://github.com/ggml-org/whisper.cpp.git /src
WORKDIR /src
RUN cmake -B build -DGGML_HIP=ON -DAMDGPU_TARGETS="gfx906;gfx90a;gfx1030;gfx1100" -DWHISPER_BUILD_EXAMPLES=ON && \
    cmake --build build -j"$(nproc)" && \
    (cp build/bin/whisper-cli /whisper-cli || cp build/bin/main /whisper-cli)

# ==============================================================================
# TARGET: CPU
# ==============================================================================
FROM ubuntu:22.04 AS runtime-cpu
LABEL org.opencontainers.image.source="https://github.com/ThirdCoastInteractive/Rewind"
LABEL org.opencontainers.image.description="Rewind ML worker (whisper.cpp CPU; Ollama/vision downloaded at start)"
LABEL org.opencontainers.image.licenses="MIT"
RUN apt-get update && apt-get install -y --no-install-recommends \
        ffmpeg ca-certificates curl libgomp1 libglib2.0-0 libgl1 \
        python3 python3-venv python3-pip \
    && rm -rf /var/lib/apt/lists/*
RUN groupadd -g 1000 appuser && useradd -u 1000 -g appuser -m -s /bin/bash appuser
WORKDIR /app
RUN mkdir -p /downloads /models/whisper /models/ollama /models/vision /runtime && \
    chown -R appuser:appuser /downloads /models /runtime
COPY --from=whisper-cpu-builder /whisper-cli /usr/local/bin/whisper-cli
COPY services/vision/ /opt/vision/
COPY services/textcls/ /opt/textcls/
COPY services/alignment/ /opt/alignment/
COPY --link --from=go-builder /app/ml ./
USER appuser
ENV WHISPER_CMD=whisper-cli
ENV WHISPER_MODEL_DIR=/models/whisper
ENV OLLAMA_MODELS=/models/ollama
ENV OLLAMA_HOST=127.0.0.1:11434
ENV OLLAMA_KEEP_ALIVE=0
ENV RUNTIME_DIR=/runtime
ENV CONTEXT_MODEL=qwen3.8:27b
ENV VISION_URL=http://127.0.0.1:3003
ENV VISION_MODEL_DIR=/models/vision
ENV VISION_APP_DIR=/opt/vision
ENV TEXTCLS_APP_DIR=/opt/textcls
VOLUME ["/models/whisper", "/models/ollama", "/models/vision", "/runtime", "/downloads"]
CMD ["./ml"]

# ==============================================================================
# TARGET: NVIDIA CUDA
# ==============================================================================
FROM nvidia/cuda:12.4.1-runtime-ubuntu22.04 AS runtime-cuda
LABEL org.opencontainers.image.source="https://github.com/ThirdCoastInteractive/Rewind"
LABEL org.opencontainers.image.description="Rewind ML worker (whisper.cpp CUDA; Ollama/vision downloaded at start)"
LABEL org.opencontainers.image.licenses="MIT"
RUN apt-get update && apt-get install -y --no-install-recommends \
        ffmpeg ca-certificates curl libopenblas0 libgomp1 libglib2.0-0 libgl1 \
        python3 python3-venv python3-pip \
    && rm -rf /var/lib/apt/lists/*
RUN groupadd -g 1000 appuser && useradd -u 1000 -g appuser -m -s /bin/bash appuser
WORKDIR /app
RUN mkdir -p /downloads /models/whisper /models/ollama /models/vision /runtime && \
    chown -R appuser:appuser /downloads /models /runtime
COPY --from=whisper-cuda-builder /whisper-cli /usr/local/bin/whisper-cli
COPY --from=whisper-cuda-builder /whisper-libs/ /usr/local/lib/
RUN ldconfig
COPY services/vision/ /opt/vision/
COPY services/textcls/ /opt/textcls/
COPY --link --from=go-builder /app/ml ./
USER appuser
ENV WHISPER_CMD=whisper-cli
ENV WHISPER_MODEL_DIR=/models/whisper
ENV OLLAMA_MODELS=/models/ollama
ENV OLLAMA_HOST=127.0.0.1:11434
ENV OLLAMA_KEEP_ALIVE=0
ENV RUNTIME_DIR=/runtime
ENV CONTEXT_MODEL=qwen3.8:27b
ENV VISION_URL=http://127.0.0.1:3003
ENV VISION_MODEL_DIR=/models/vision
ENV VISION_APP_DIR=/opt/vision
ENV TEXTCLS_APP_DIR=/opt/textcls
ENV NVIDIA_VISIBLE_DEVICES=all
ENV NVIDIA_DRIVER_CAPABILITIES=compute,utility
VOLUME ["/models/whisper", "/models/ollama", "/models/vision", "/runtime", "/downloads"]
CMD ["./ml"]

# ==============================================================================
# TARGET: AMD ROCm
# ==============================================================================
FROM rocm/dev-ubuntu-22.04:6.1 AS runtime-rocm
LABEL org.opencontainers.image.source="https://github.com/ThirdCoastInteractive/Rewind"
LABEL org.opencontainers.image.description="Rewind ML worker (whisper.cpp ROCm; Ollama/vision downloaded at start)"
LABEL org.opencontainers.image.licenses="MIT"
RUN apt-get update && apt-get install -y --no-install-recommends \
        ffmpeg ca-certificates curl libgomp1 libglib2.0-0 libgl1 \
        python3 python3-venv python3-pip \
    && rm -rf /var/lib/apt/lists/*
RUN groupadd -g 1000 appuser && useradd -u 1000 -g appuser -m -s /bin/bash appuser
WORKDIR /app
RUN mkdir -p /downloads /models/whisper /models/ollama /models/vision /runtime && \
    chown -R appuser:appuser /downloads /models /runtime
COPY --from=whisper-rocm-builder /whisper-cli /usr/local/bin/whisper-cli
COPY services/vision/ /opt/vision/
COPY services/textcls/ /opt/textcls/
COPY --link --from=go-builder /app/ml ./
USER appuser
ENV WHISPER_CMD=whisper-cli
ENV WHISPER_MODEL_DIR=/models/whisper
ENV OLLAMA_MODELS=/models/ollama
ENV OLLAMA_HOST=127.0.0.1:11434
ENV OLLAMA_KEEP_ALIVE=0
ENV RUNTIME_DIR=/runtime
ENV ML_RUNTIME=runtime-rocm
ENV CONTEXT_MODEL=qwen3.8:27b
ENV VISION_URL=http://127.0.0.1:3003
ENV VISION_MODEL_DIR=/models/vision
ENV VISION_APP_DIR=/opt/vision
ENV TEXTCLS_APP_DIR=/opt/textcls
VOLUME ["/models/whisper", "/models/ollama", "/models/vision", "/runtime", "/downloads"]
CMD ["./ml"]

FROM runtime-cpu
