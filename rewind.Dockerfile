# syntax=docker/dockerfile:1
FROM golang:1.26-alpine AS builder

WORKDIR /app

# Copy go mod files first for better layer caching
COPY go.mod go.sum* ./
RUN --mount=type=cache,id=rewind-go-mod,target=/go/pkg/mod go mod download

# Unified binary needs web (templates + static embed), SFU, workers, and migrator.
COPY cmd ./cmd
COPY pkg ./pkg
COPY internal ./internal
COPY static ./static

RUN --mount=type=cache,id=rewind-go-mod,target=/go/pkg/mod --mount=type=cache,id=rewind-go-build,target=/root/.cache/go-build \
    go build -o rewind ./cmd/web && \
    go build -o show-note-workspace ./cmd/show-note-workspace

FROM debian:12-slim

LABEL org.opencontainers.image.source="https://github.com/ThirdCoastInteractive/Rewind"
LABEL org.opencontainers.image.description="Rewind"
LABEL org.opencontainers.image.licenses="MIT"

RUN --mount=type=cache,target=/var/cache/apt,sharing=locked \
    --mount=type=cache,target=/var/lib/apt,sharing=locked \
    apt-get update && \
    apt-get install -y --no-install-recommends ca-certificates ffmpeg curl unzip && \
    rm -rf /var/lib/apt/lists/*

RUN curl -fSL https://deno.land/install.sh -o /tmp/install_deno.sh
RUN sh /tmp/install_deno.sh && mv /root/.deno/bin/deno /usr/local/bin/deno

# yt-dlp: the latest release binary by default. Set YTDLP_REF to a git ref
# (branch, tag, commit, or a PR's refs/pull/N/head) to install from source
# instead — for picking up an extractor fix that hasn't shipped in a release.
# Clear it once the fix lands upstream.
#
# The curl-cffi extra is mandatory, not optional: pkg/ytdlp passes
# --impersonate chrome on every invocation, which the release binary satisfies
# with a bundled curl_cffi. Without the extra, a source install fails every
# download, not just the ones the pin was meant to fix. The build asserts this.
ARG YTDLP_REF=""
RUN set -eux; \
    if [ -n "$YTDLP_REF" ]; then \
        apt-get update; \
        apt-get install -y --no-install-recommends git python3 python3-venv; \
        rm -rf /var/lib/apt/lists/*; \
        python3 -m venv /opt/ytdlp; \
        /opt/ytdlp/bin/pip install --no-cache-dir --upgrade pip; \
        /opt/ytdlp/bin/pip install --no-cache-dir \
            "yt-dlp[default,curl-cffi] @ git+https://github.com/yt-dlp/yt-dlp.git@${YTDLP_REF}"; \
        ln -s /opt/ytdlp/bin/yt-dlp /usr/local/bin/yt-dlp; \
    else \
        curl -L https://github.com/yt-dlp/yt-dlp/releases/latest/download/yt-dlp_linux -o /usr/local/bin/yt-dlp; \
        chmod +x /usr/local/bin/yt-dlp; \
    fi; \
    yt-dlp --version; \
    yt-dlp --list-impersonate-targets | grep -qi chrome

# Tells the worker not to run `yt-dlp -U` at startup: self-updating would
# replace the pinned ref with the latest release on every container start.
ENV YTDLP_PINNED=${YTDLP_REF:+1}

# Create non-root user (Debian syntax)
RUN groupadd -g 1000 appuser && \
    useradd -u 1000 -g appuser -m -s /bin/bash appuser

WORKDIR /app

RUN mkdir -p /spool/downloads /downloads /exports /fonts /audio && \
    chown -R appuser:appuser /spool /downloads /exports /fonts /audio

COPY --link --from=builder /app/rewind ./
COPY --link --from=builder /app/show-note-workspace ./

USER appuser

EXPOSE 8080

ENTRYPOINT ["./rewind"]
CMD ["serve"]
