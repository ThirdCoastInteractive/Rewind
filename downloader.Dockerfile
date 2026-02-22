FROM golang:1.25.4-alpine3.21 AS builder

WORKDIR /app

# Copy go mod files first for better layer caching
COPY go.mod go.sum* ./
RUN go mod download

# Copy only necessary source files (excludes static/, chrome-extension/, etc.)
COPY cmd/downloader ./cmd/downloader
COPY internal ./internal
COPY pkg ./pkg

RUN go build -o downloader ./cmd/downloader

FROM debian:12-slim

LABEL org.opencontainers.image.source="https://github.com/ThirdCoastInteractive/Rewind"
LABEL org.opencontainers.image.description="Rewind download worker"
LABEL org.opencontainers.image.licenses="MIT"

# Install runtime deps: curl, ffmpeg, CA certs, and deno for JS extraction
RUN apt-get update && \
    apt-get install -y --no-install-recommends ca-certificates curl ffmpeg unzip && \
    curl -fsSL https://deno.land/install.sh | sh && \
    mv /root/.deno/bin/deno /usr/local/bin/deno && \
    rm -rf /var/lib/apt/lists/*

# Download yt-dlp binary from GitHub releases
RUN curl -L https://github.com/yt-dlp/yt-dlp/releases/latest/download/yt-dlp_linux -o /usr/local/bin/yt-dlp && \
    chmod +x /usr/local/bin/yt-dlp && \
    yt-dlp --version

# Create non-root user (Debian syntax)
RUN groupadd -g 1000 appuser && \
    useradd -u 1000 -g appuser -m -s /bin/bash appuser

WORKDIR /app

# Create /spool directory with proper ownership BEFORE switching to non-root
RUN mkdir -p /spool/downloads && chown -R appuser:appuser /spool

COPY --from=builder /app/downloader ./

USER appuser

CMD ["./downloader"]
