# Configuration Reference

All settings are configured via environment variables in your `.env` file. Copy `.env.example` to get started:

```bash
cp .env.example .env
```

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

## Database

| Variable            | Default    | Description       |
| ------------------- | ---------- | ----------------- |
| `POSTGRES_USER`     | `rewind`   | Database username |
| `POSTGRES_DB`       | `rewind`   | Database name     |
| `POSTGRES_PASSWORD` | (required) | Database password |

The `DATABASE_DSN` is constructed automatically from these values in Docker Compose.

## Transcription (Whisper)

Rewind uses [OpenAI Whisper](https://github.com/openai/whisper) to generate searchable transcripts for every video.

| Variable           | Default | Description                                                        |
| ------------------ | ------- | ------------------------------------------------------------------ |
| `WHISPER_ENABLED`  | `true`  | Set to `false` to skip transcription entirely                      |
| `WHISPER_MODEL`    | `small` | Model size: `tiny`, `base`, `small`, `medium`, `large`, `large-v2` |
| `WHISPER_DEVICE`   | `cpu`   | Set to `cuda` for NVIDIA GPU acceleration                          |
| `WHISPER_LANGUAGE` | `en`    | Language code (`en`, `es`, `ja`, etc.)                             |

**Model size trade-offs:**

| Model      | RAM    | Speed     | Accuracy  |
| ---------- | ------ | --------- | --------- |
| `tiny`     | ~1 GB  | Very fast | Low       |
| `base`     | ~1 GB  | Fast      | Fair      |
| `small`    | ~2 GB  | Moderate  | Good      |
| `medium`   | ~5 GB  | Slow      | Very good |
| `large-v2` | ~10 GB | Very slow | Best      |

The `small` model is a good default. Upgrade to `medium` or `large-v2` if accuracy matters more than processing time.

## GPU Acceleration

If you have an NVIDIA GPU, you can speed up Whisper transcription significantly.

### Setup

1. Install the [NVIDIA Container Toolkit](https://docs.nvidia.com/datacenter/cloud-native/container-toolkit/install-guide.html)
2. Set `WHISPER_DEVICE=cuda` in `.env`
3. Uncomment the GPU section in the `ingest` service in `docker-compose.yml`:

```yaml
    deploy:
      resources:
        reservations:
          devices:
            - capabilities: [gpu]
```

4. Restart the stack: `make down && make up`

## Downloads

| Variable           | Default | Description                                                                     |
| ------------------ | ------- | ------------------------------------------------------------------------------- |
| `DOWNLOAD_WORKERS` | `3`     | Number of parallel download workers (set via `--scale downloader=N` in compose) |
| `INGEST_WORKERS`   | `5`     | Number of parallel ingest workers (set via `--scale ingest=N` in compose)       |
| `ENCODER_WORKERS`  | `3`     | Number of parallel encoder workers (set via `--scale encoder=N` in compose)     |

Worker counts are controlled by Docker Compose replica scaling rather than environment variables. Adjust in `docker-compose.yml`:

```yaml
services:
  downloader:
    deploy:
      replicas: 3
  ingest:
    deploy:
      replicas: 5
  encoder:
    deploy:
      replicas: 3
```

## Storage Paths

Default paths (relative to project directory):

| Path                      | Contents                                      |
| ------------------------- | --------------------------------------------- |
| `./bin/spool`             | Temporary workspace for in-progress downloads |
| `./bin/download`          | Archived video files                          |
| `./bin/exports`           | Exported clips                                |
| `./bin/dev/postgres/data` | Database data                                 |

Change these by editing the volume mounts in `docker-compose.yml`. For large libraries, point them at a drive with plenty of space.

## Admin Settings

These are configured through the web UI at `/admin` after logging in as an admin.

| Setting              | Description                                                                                                                                                    |
| -------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Registration enabled | Allow new users to create accounts                                                                                                                             |
| Export storage limit | Maximum total size for exported clips (e.g., `10G`, `500M`). Oldest exports are cleaned up automatically when the limit is reached. Leave blank for unlimited. |
| Admin emails         | Comma-separated list of email addresses that are automatically granted admin access on registration                                                            |

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

### Backups

Regularly back up:

1. **Database** - use `pg_dump` or a scheduled backup container
2. **Video files** - the `./bin/download` directory
3. **Environment file** - your `.env` (contains encryption keys)

The `ENCRYPTION_KEY` is critical. If you lose it, encrypted data (cookies, tokens) cannot be recovered.
