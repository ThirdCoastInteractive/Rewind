FROM golang:1.25.4-alpine3.21 AS builder

WORKDIR /app

# Copy go mod files first for better layer caching
COPY go.mod go.sum* ./
RUN go mod download

# Copy only necessary source files
COPY cmd/encoder ./cmd/encoder
COPY internal ./internal
COPY pkg ./pkg

RUN go build -o encoder ./cmd/encoder

FROM debian:12-slim

LABEL org.opencontainers.image.source="https://github.com/ThirdCoastInteractive/Rewind"
LABEL org.opencontainers.image.description="Rewind encoder worker"
LABEL org.opencontainers.image.licenses="MIT"

# Install runtime deps: ffmpeg for encoding
RUN apt-get update && \
    apt-get install -y --no-install-recommends ca-certificates ffmpeg && \
    rm -rf /var/lib/apt/lists/*

# Create non-root user
RUN groupadd -g 1000 appuser && \
    useradd -u 1000 -g appuser -m -s /bin/bash appuser

WORKDIR /app

# Create directories with proper ownership
RUN mkdir -p /downloads /exports && chown -R appuser:appuser /downloads /exports

COPY --from=builder /app/encoder ./

USER appuser

CMD ["./encoder"]
