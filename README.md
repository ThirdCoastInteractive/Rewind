# Rewind

**Your personal video archive.** Save videos from YouTube, Vimeo, X, and [hundreds of other sites](https://github.com/yt-dlp/yt-dlp/blob/master/supportedsites.md) to your own server. Search transcripts, cut clips, stitch compilations, and never worry about a video disappearing from the internet again.

Rewind runs entirely on your hardware. No cloud accounts, no subscriptions, no data leaving your network.

**Docs:** [Getting started](docs/getting-started.md) · [Library](docs/library.md) · [Clipping](docs/clipping.md) · [Wiki](docs/wiki.md) · [Show notes](docs/show-notes.md) · [MCP](docs/mcp.md) · [Full index](docs/README.md)

## Screenshots

### Home

Paste a URL to start archiving. Recent downloads appear below for quick access.

![Home](screenshots/readme/home.png)

### Library

Browse the archive with search, filters, and sorting. Thumbnails and metadata make it easy to find what you are looking for.

![Video archive grid](screenshots/readme/videos.png)

### Watch

Play a video with a synced, searchable transcript. Click any line to jump to that moment.

![Video detail view](screenshots/readme/video-detail.png)

![Transcript search](screenshots/readme/video-detail-search.png)

Generated context windows chapter the tape and flag nested shorts — spoken beats you can clip without dumping a whole segment.

![Context windows](screenshots/readme/video-detail-context.png)

### Cut

Trim a single video on a zoomable timeline. Align with subtitles, stack color filters, add crop variants, and export.

![Clip editor overview](screenshots/readme/cut-editor.png)

![Filters and crops](screenshots/readme/cut-editor-filters.png)

### Stitch

Assemble clips, title cards, and overlays into a multi-source project. Export an explicit revision.

![Stitch library](screenshots/readme/stitch.png)

![Stitch editor](screenshots/readme/stitch-editor.png)

### Wiki

A local vault over the archive: creators, channels, clipping notes, and topics, connected with wikilinks.

![Wiki](screenshots/readme/wiki.png)

### Channels and creators

Every uploader in the archive, with follow actions. Group a person's channels across platforms.

![Channels](screenshots/readme/channels.png)

![Creators](screenshots/readme/creators.png)

![Creator page](screenshots/readme/creator-view.png)

### Follows

Watch a channel for new uploads on a schedule.

![Follows](screenshots/readme/follows.png)

### Network

A live graph of outlinks, mentions, comments, and wiki links between archived channels. Same-creator channels cluster together.

![Network graph](screenshots/readme/network.png)

### Jobs

Monitor download progress, retries, and processing from one dashboard.

![Jobs dashboard](screenshots/readme/jobs.png)

### Settings

Configure cookies for age-restricted downloads, interface preferences, and MCP API tokens.

![Settings](screenshots/readme/settings.png)

### Admin

Storage metrics, archives-per-day, sources, and worker health.

![Admin dashboard](screenshots/readme/admin.png)

## Features

- **Video archival** — YouTube, Vimeo, X, and [hundreds of other sites](https://github.com/yt-dlp/yt-dlp/blob/master/supportedsites.md) via [yt-dlp](https://github.com/yt-dlp/yt-dlp)
- **Channel and playlist ingest** — batch-archive a channel or playlist; already-saved videos are skipped
- **Follows** — scan a channel on a schedule and queue new uploads
- **Automatic transcription** — [whisper.cpp](https://github.com/ggml-org/whisper.cpp) generates searchable captions (install GGML weights from Admin; they persist across rebuilds)
- **Context windows** — local chapters and nested shorts from the transcript
- **Transcript search** — find a word across the library and jump to that moment
- **Cut editor** — in/out on a zoomable timeline, FFmpeg color filters, crop presets, export
- **Stitch** — multi-source timeline with title cards, overlays, captions, and revisioned export
- **Wiki** — nested vault pages and wikilinks over creators, channels, clipping notes, and topics
- **Creators** — group a person's channels across platforms; confirm suggested links
- **Network graph** — outlinks, mentions, comments, and vault edges
- **Show notes** — collaborative Markdown rundowns and live program output for OBS
- **MCP** — point Claude, Cursor, or similar at `/mcp` to search, clip, and stitch from an agent
- **Browser extension** — queue a page, or archive a live tab, from Chrome or Firefox
- **SponsorBlock** — auto-skip sponsor segments on YouTube with on-screen notifications
- **Markers and comments** — timestamped markers; imported comments with clickable timestamps
- **Visual search** — find scenes by description or a reference image after optional indexing
- **Admin dashboard** — storage, archives-per-day, users, exports, asset health
- **Customizable keybindings** — rebind every shortcut, including hardware keys (F14–F24)
- **Fully self-hosted** — Docker, local storage, no external accounts required

## Quick Start

### Requirements

- [Docker Desktop](https://www.docker.com/products/docker-desktop/) (includes Docker Compose)
- Enough disk space for your video library

### 1. Clone and configure

```bash
git clone https://github.com/ThirdCoastInteractive/Rewind.git
cd Rewind
cp .env.example .env
```

Open `.env` in a text editor and fill in these values:

| Variable            | Description                                                        |
| ------------------- | ------------------------------------------------------------------ |
| `POSTGRES_PASSWORD` | Pick any password for the database                                 |
| `SESSION_SECRET`    | A random string for signing session cookies                        |
| `ENCRYPTION_KEY`    | 32-byte hex key. Generate one with `openssl rand -hex 32`          |

Pin a release with `REWIND_VERSION=v0.1.0` (or omit it to follow `latest`).

### 2. Start

```bash
make up
```

Or without `make`:

```bash
docker compose up -d
```

### 3. Open

Visit **http://localhost:8080** in your browser.

Change the port with `WEBSERVER_PORT` in `.env` if needed.

### 4. View logs

```bash
make logs
```

### 5. Stop

```bash
make down
```

See [Getting started](docs/getting-started.md) for the first-video walkthrough.

## Configuration

All settings live in your `.env` file. See [configuration.md](docs/configuration.md) and [.env.example](.env.example) for the full list.

### Transcription (whisper.cpp)

Every video is transcribed with [whisper.cpp](https://github.com/ggml-org/whisper.cpp) inside **`rewind-ml`**. Install GGML weights (`ggml-*.bin`) into `./bin/models/whisper` from Admin runtime settings; they are reused across container rebuilds. The ML worker will not download weights on its own.

| Variable           | Default          | Description |
| ------------------ | ---------------- | ----------- |
| `WHISPER_MODEL`    | `large-v3-turbo` | `tiny`, `base`, `small`, `medium`, `large-v3`, `large-v3-turbo`, … |
| `WHISPER_LANGUAGE` | `en`             | ISO code, or `auto` |
| `WHISPER_TASK`     | `transcribe`     | `translate` forces English output (`-tr`) |
| `ML_RUNTIME`       | `runtime-cpu`    | `runtime-cuda` / `runtime-rocm` for GPU images |

### GPU acceleration (optional)

NVIDIA: install the [NVIDIA Container Toolkit](https://docs.nvidia.com/datacenter/cloud-native/container-toolkit/install-guide.html), then set these in `.env` and run `make up`:

```bash
# .env
ML_RUNTIME=runtime-cuda
WHISPER_DEVICE=cuda
VISION_DEVICE=cuda
WHISPER_MODEL=large-v3-turbo
```

Restart the stack after making changes.

### Storage

By default, Rewind stores files in subdirectories of the project folder:

| Path                      | Contents                          |
| ------------------------- | --------------------------------- |
| `./bin/spool`             | Temporary workspace for downloads |
| `./bin/download`          | Archived video files              |
| `./bin/exports`           | Exported clips                    |
| `./bin/runtime`           | Ollama + vision Python (first start) |
| `./bin/dev/postgres/data` | Database data                     |

You can change these paths in `docker-compose.yml`. For large libraries, point them at a drive with plenty of space.

## Deployment

- **Home / local network** — `docker compose up -d`, then `http://localhost:8080`
- **Expose to the internet** — put Rewind behind a reverse proxy with HTTPS. Good options: [Cloudflare Tunnel](https://developers.cloudflare.com/cloudflare-one/connections/connect-networks/), [Caddy](https://caddyserver.com/), [Nginx](https://nginx.org/), or [Traefik](https://traefik.io/)
- **Backups** — regularly back up your PostgreSQL database and video files

## Browser extension

Save videos to Rewind with one click from your browser. On a live tab, the extension can also start an in-progress archive. Setup: [browser-extension.md](docs/browser-extension.md).

**Chrome / Chromium:**

1. Open `chrome://extensions`
2. Enable **Developer mode** (top-right toggle)
3. Click **Load unpacked** and select the `extensions/chrome-extension-v3/` folder

**Firefox:**

Load the extension from `extensions/firefox-extension/`.

## How it works

Rewind is three Docker Compose services:

| Service      | What it does                                                                 |
| ------------ | ---------------------------------------------------------------------------- |
| `postgres`   | Metadata, transcripts, job state (PostgreSQL 17 + pgvector)                  |
| `rewind`     | Web UI, download/ingest/encode workers, WebRTC SFU, and migrations           |
| `rewind-ml`  | Whisper transcription, Ollama context windows, and visual search (optional GPU) |

Download/ingest/encode parallelism is an admin runtime setting, not Compose replica counts. Transcription and vision run in `rewind-ml`.

## Roadmap

- [x] Playlist and channel archival
- [x] Scheduled/recurring downloads (channel follows)
- [x] Collections and tagging
- [x] Bulk tag selected videos (bulk delete omitted on purpose; bulk export still TBD)
- [x] Stitch multi-source editor
- [x] Wiki vault and canonical topics
- [x] MCP clipping into Stitch
- [ ] Theme customization (light mode, custom accent colors)
- [ ] Bulk export from the library grid
- [ ] Richer live directing (multi-host webcam, shader library)

## Contributing

Contributions are welcome. The project uses Go, PostgreSQL, [templ](https://templ.guide/) for HTML templates, and [sqlc](https://sqlc.dev/) for type-safe SQL.

```bash
# Install tools
go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest
go install github.com/a-h/templ/cmd/templ@latest
pnpm install

# Regenerate after changes to .templ or .sql files
make generate

# Run tests
make test
```

## License

[MIT](LICENSE)
