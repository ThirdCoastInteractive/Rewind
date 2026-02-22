FROM golang:1.25.4-alpine3.21 AS builder

WORKDIR /app

# Copy go mod files first for better layer caching
COPY go.mod go.sum* ./
RUN go mod download

# Copy necessary source files (internal deps needed by application layer)
COPY cmd/pg-migrator ./cmd/pg-migrator
COPY internal ./internal
COPY pkg ./pkg

RUN go build -o pg-migrator ./cmd/pg-migrator

FROM alpine:3.21

LABEL org.opencontainers.image.source="https://github.com/ThirdCoastInteractive/Rewind"
LABEL org.opencontainers.image.description="Rewind database migrator"
LABEL org.opencontainers.image.licenses="MIT"

# Install ca-certificates for HTTPS requests
RUN apk --no-cache add ca-certificates

# Create non-root user
RUN addgroup -g 1000 appuser && \
    adduser -D -u 1000 -G appuser appuser

WORKDIR /app

COPY --from=builder /app/pg-migrator ./

USER appuser

CMD ["./pg-migrator"]
