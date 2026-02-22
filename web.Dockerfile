FROM golang:1.25.4-alpine3.21 AS builder

WORKDIR /app

# Copy go mod files first for better layer caching
COPY go.mod go.sum* ./
RUN go mod download

# Copy only necessary source files
COPY cmd/web ./cmd/web
COPY pkg ./pkg
COPY internal ./internal
COPY static ./static

RUN go build -o web ./cmd/web

FROM alpine:3.21

LABEL org.opencontainers.image.source="https://github.com/ThirdCoastInteractive/Rewind"
LABEL org.opencontainers.image.description="Rewind web server"
LABEL org.opencontainers.image.licenses="MIT"

# Install ca-certificates for HTTPS requests and ffmpeg for clip exports
RUN apk --no-cache add ca-certificates ffmpeg

# Create non-root user
RUN addgroup -g 1000 appuser && \
    adduser -D -u 1000 -G appuser appuser

WORKDIR /app

COPY --from=builder /app/web ./

USER appuser

EXPOSE 8080

CMD ["./web"]
