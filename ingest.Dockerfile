FROM golang:1.25.4-alpine3.21 AS builder

WORKDIR /app

COPY go.mod go.sum* ./
RUN go mod download

# Copy only necessary source files
COPY cmd/ingest ./cmd/ingest
COPY internal ./internal
COPY pkg ./pkg

RUN go build -o ingest ./cmd/ingest

FROM debian:12-slim

LABEL org.opencontainers.image.source="https://github.com/ThirdCoastInteractive/Rewind"
LABEL org.opencontainers.image.description="Rewind ingest worker"
LABEL org.opencontainers.image.licenses="MIT"

RUN apt-get update && \
    apt-get install -y --no-install-recommends ca-certificates ffmpeg python3 python3-venv && \
    python3 -m venv /opt/whisper-venv && \
    /opt/whisper-venv/bin/pip install --no-cache-dir --upgrade pip && \
    /opt/whisper-venv/bin/pip install --no-cache-dir openai-whisper && \
    rm -rf /var/lib/apt/lists/*

ENV PATH="/opt/whisper-venv/bin:$PATH"

RUN groupadd -g 1000 appuser && \
    useradd -u 1000 -g appuser -m -s /bin/bash appuser

WORKDIR /app

# Create /spool directory with proper ownership BEFORE switching to non-root
RUN mkdir -p /spool && chown -R appuser:appuser /spool

COPY --from=builder /app/ingest ./

USER appuser

CMD ["./ingest"]
