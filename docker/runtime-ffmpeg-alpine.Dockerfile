# Shared Alpine runtime with ffmpeg. Built rarely — web (and any other Alpine
# service that needs ffmpeg) FROMs this so code rebuilds skip apk add ffmpeg.
#
#   make runtime-bases
FROM alpine:3.21

LABEL org.opencontainers.image.description="Rewind shared Alpine runtime (ffmpeg)"

RUN apk --no-cache add ca-certificates ffmpeg
