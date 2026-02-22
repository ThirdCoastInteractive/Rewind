# Getting Started

This guide walks you through installing Rewind, configuring it, and archiving your first video.

## Requirements

- [Docker Desktop](https://www.docker.com/products/docker-desktop/) (includes Docker Compose)
- Enough disk space for your video library (videos are stored at original quality)

## Installation

### 1. Clone the repository

```bash
git clone https://github.com/ThirdCoastInteractive/Rewind.git
cd Rewind
```

### 2. Create your environment file

```bash
cp .env.example .env
```

Open `.env` in a text editor and fill in these three required values:

| Variable            | Description                                               |
| ------------------- | --------------------------------------------------------- |
| `POSTGRES_PASSWORD` | Pick any password for the database                        |
| `SESSION_SECRET`    | A random string for signing session cookies               |
| `ENCRYPTION_KEY`    | 32-byte hex key. Generate one with `openssl rand -hex 32` |

Everything else has sensible defaults. See [Configuration](configuration.md) for the full list.

### 3. Start the stack

```bash
docker compose up -d
```

This pulls the pre-built container images and starts all services. First startup takes a minute or two while the database is initialized.

### 4. Open the web UI

Visit **http://localhost:8080** in your browser.

You can change the port by setting `WEBSERVER_PORT` in your `.env` file.

### 5. Create your account

On first visit you'll see a registration page. Create your admin account. You can disable public registration later in the admin settings.

## Archive your first video

1. Go to **Home**
2. Paste a video URL (YouTube, Vimeo, or any [yt-dlp supported site](https://github.com/yt-dlp/yt-dlp/blob/master/supportedsites.md))
3. Click **SUBMIT JOB**

The download starts in the background. You can watch progress on the **Jobs** page. Once complete, the video appears in your **Library** with auto-generated thumbnails and a searchable transcript.

## Useful commands

```bash
# View logs from all services
docker compose logs -f

# Stop all services
docker compose down

# Check service status
docker compose ps

# Rebuild after updating
docker compose up -d --build
```

## Next steps

- [Configuration](configuration.md) - all environment variables and options
- [Keyboard Shortcuts](keyboard-shortcuts.md) - full keybinding reference
- [Browser Extension](browser-extension.md) - one-click archiving from Chrome or Firefox
