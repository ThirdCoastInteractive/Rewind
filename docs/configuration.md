# Configuration Reference

All settings are configured via environment variables in your `.env` file. Copy `.env.example` to get started:

```bash
cp .env.example .env
```

Rewind is three Compose services: `postgres`, `rewind` (web + workers + SFU + migrate), and `rewind-ml` (Whisper, Ollama, vision).

## Required

These must be set before starting Rewind.

| Variable            | Description                                                                            |
| ------------------- | -------------------------------------------------------------------------------------- |
| `POSTGRES_PASSWORD` | Password for the PostgreSQL database                                                   |
| `SESSION_SECRET`    | Random string used to sign session cookies                                             |
| `ENCRYPTION_KEY`    | 32-byte hex string for encrypting sensitive data. Generate with `openssl rand -hex 32` |

## Server

| Variable         | Default                 | Description                                                              |
| ---------------- | ----------------------- | ------------------------------------------------------------------------ |
| `WEBSERVER_PORT` | `8080`                  | Port the web UI listens on                                               |
| `WEBSERVER_HOST` | `0.0.0.0`               | Bind address for the web server                                          |
| `BASE_URL`       | `http://localhost:8080` | Public URL of your Rewind instance (used for bookmarklet and extensions) |
| `REWIND_ROLES`   | `web,download,ingest,encode,sfu,migrate` | Comma-separated roles for the unified `rewind` process |

## Database

| Variable            | Default    | Description       |
| ------------------- | ---------- | ----------------- |
| `POSTGRES_USER`     | `rewind`   | Database username |
| `POSTGRES_DB`       | `rewind`   | Database name     |
| `POSTGRES_PASSWORD` | (required) | Database password |

The `DATABASE_DSN` is constructed automatically from these values in Docker Compose. The `rewind` container runs migrations on boot.

## Transcription (whisper.cpp)

Rewind transcribes with [whisper.cpp](https://github.com/ggml-org/whisper.cpp) inside **`rewind-ml`**, not the ingest role. GGML weights are **not** in the image: install `WHISPER_MODEL` into `/models/whisper` (compose maps `./bin/models/whisper`) from Admin runtime settings. The ML worker will not download weights on its own. Ollama and the vision Python environment download into `./bin/runtime` on first container start.

| Variable            | Default            | Description |
| ------------------- | ------------------ | ----------- |
| `WHISPER_CMD`       | `whisper-cli`      | whisper.cpp binary |
| `WHISPER_MODEL`     | `large-v3-turbo`   | GGML id: `tiny`, `base`, `small`, `medium`, `large-v3`, `large-v3-turbo`, `small.en`, … |
| `WHISPER_MODEL_DIR` | `/models/whisper`  | Persistent cache for `ggml-*.bin` |
| `WHISPER_LANGUAGE`  | `en`               | ISO 639-1, or `auto` to detect |
| `WHISPER_TASK`      | `transcribe`       | `transcribe` or `translate` (English via `-tr`) |
| `WHISPER_DEVICE`    | `cpu`              | `cpu` or `cuda` (see GPU overlay) |

**Model trade-offs:**

| Model            | Speed        | Notes |
| ---------------- | ------------ | ----- |
| `tiny` / `base`  | Very fast    | Rough captions |
| `small`          | Moderate     | Good balance |
| `medium`         | Slower       | Better multilingual |
| `large-v3`       | Slow         | Best quality / translation |
| `large-v3-turbo` | Fast large   | Default — strong quality, much quicker |

Tune model, language, task, and device from **Admin → runtime settings**. Transcription is always on; `WHISPER_*` env vars are bootstrap defaults only.

## GPU Acceleration

NVIDIA:

1. Install the [NVIDIA Container Toolkit](https://docs.nvidia.com/datacenter/cloud-native/container-toolkit/install-guide.html)
2. `docker compose -f docker-compose.yml -f docker-compose.gpu.yml up -d --build`

That overlay sets `rewind-ml` to the CUDA runtime (`WHISPER_DEVICE=cuda`). Weights stay in `./bin/models` and Ollama/vision binaries stay in `./bin/runtime` across rebuilds. From a development checkout, set `ML_RUNTIME=runtime-cuda` and `WHISPER_DEVICE=cuda` in `.env` and run `make up` — it includes the overlay automatically. The default compose file is CPU-only.

## ML runtime (Ollama / vision)

The `rewind-ml` image does not bake Ollama or the vision Python environment. On first start the worker downloads them into `RUNTIME_DIR` (compose maps `./bin/runtime`). Later starts reuse the stamp files in that directory. Whisper.cpp itself stays in the image because there is no official CUDA `whisper-cli` tarball.

| Variable         | Default     | Description |
| ---------------- | ----------- | ----------- |
| `RUNTIME_DIR`    | `/runtime`  | Persistent dir for Ollama binaries and the vision venv |
| `OLLAMA_VERSION` | `0.33.2`    | GitHub release tag fetched on first start |

## Vision

The vision service runs inside `rewind-ml`. Compose sets `VISION_URL=http://rewind-ml:3003` on `rewind` and `http://127.0.0.1:3003` inside `rewind-ml`. Override `VISION_URL` only for non-compose deployments.

| Variable       | Default | Description |
| -------------- | ------- | ----------- |
| `VISION_URL`   | (compose) | Optional. HTTP endpoint for CLIP embeddings |
| `VISION_TOKEN` | `ENCRYPTION_KEY` | Shared token for the vision HTTP API |
| `VISION_DEVICE`| `cpu`   | `cpu` or `cuda` |

## Downloads and workers

Download, ingest, and encode workers run in-process inside `rewind`. Parallelism is an **admin runtime setting**, not Compose replica scaling.

| Setting | Where |
| ------- | ----- |
| Download workers | Admin → runtime settings (`downloads.workers`) |
| Ingest workers | Admin → runtime settings (`processing.ingest_workers`) |
| Asset generation workers | Admin → runtime settings (`processing.asset_workers`) |
| Ingest/asset FFmpeg threads | Admin → runtime settings (`processing.ffmpeg_threads`) |
| Encoder workers | Admin → runtime settings (`processing.encoder_workers`) |

`DOWNLOAD_WORKERS`, `INGEST_WORKERS`, `ASSET_WORKERS`, `FFMPEG_THREADS`, and `ENCODER_WORKERS` are optional bootstrap defaults used until an admin value is saved. Ingest publishes the archive file; asset workers generate preview, seek sprites, and waveform off that queue. Background FFmpeg never uses the GPU decoder and only one of those jobs runs at a time so the host player and YouTube keep NVDEC. Ingest/catchup transcription runs on CPU; CUDA Whisper is only for interactive caption jobs, and only one of those at a time.

## Storage Paths

Default paths (relative to project directory):

| Path                      | Contents                                      |
| ------------------------- | --------------------------------------------- |
| `./bin/spool`             | Temporary workspace for in-progress downloads |
| `./bin/download`          | Archived video files                          |
| `./bin/exports`           | Exported clips                                |
| `./bin/fonts`             | Downloaded Google Fonts (TTF) for titles/captions |
| `./bin/models/whisper`    | Whisper GGML weights                          |
| `./bin/models/ollama`     | Ollama model blobs                            |
| `./bin/models/vision`     | Vision/CLIP weights                           |
| `./bin/runtime`           | Ollama binaries + vision Python venv (first start) |
| `./bin/dev/postgres/data` | Database data                                 |

Change these by editing the volume mounts in `docker-compose.yml`. For large libraries, point them at a drive with plenty of space.

## Admin Settings

These are configured through the web UI at `/admin` after logging in as an admin.

| Setting              | Description                                                                                                                                                    |
| -------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Registration enabled | Allow new users to create accounts                                                                                                                             |
| Export storage limit | Maximum total size for exported clips (e.g., `10G`, `500M`). Oldest exports are cleaned up automatically when the limit is reached. Leave blank for unlimited. |
| Admin emails         | Comma-separated list of email addresses that are automatically granted admin access on registration                                                            |
| Runtime settings     | Worker counts, Whisper, vision, and model options applied live without restarting Compose                                      |

## Extensions

| Variable                       | Default | Description                                                  |
| ------------------------------ | ------- | ------------------------------------------------------------ |
| `EXTENSION_ALLOWED_CLIENT_IDS` | (empty) | Comma-separated list of allowed browser extension client IDs |

See [Browser Extension](browser-extension.md) for setup instructions.

## SponsorBlock

SponsorBlock segment fetching is enabled by default for YouTube videos. Segments appear as markers with an **SB** badge and are automatically skipped during playback.

To disable auto-skip, set `videoPlayer.autoSkipSponsors = false` in your browser's localStorage.

## Deployment Notes

### Local network

Run `docker compose up -d` and access via `http://<your-ip>:8080` from any device on your network.

### Internet access

Put Rewind behind a reverse proxy with HTTPS. Recommended options:

- [Cloudflare Tunnel](https://developers.cloudflare.com/cloudflare-one/connections/connect-networks/) (easiest, no port forwarding needed)
- [Caddy](https://caddyserver.com/) (automatic HTTPS)
- [Nginx](https://nginx.org/)
- [Traefik](https://traefik.io/)

WebRTC media uses UDP `50000-50100` on the `rewind` service. Forward that range if remote hosts need to publish camera/mic.

### Backups

Regularly back up:

1. **Database** - use `pg_dump` or a scheduled backup container
2. **Video files** - the `./bin/download` directory
3. **Environment file** - your `.env` (contains encryption keys)

The `ENCRYPTION_KEY` is critical. If you lose it, encrypted data (cookies, tokens) cannot be recovered.
