# Rewind Chrome Extension (Rewrite, MV3)

This is the new extension implementation (token-only auth, minimal permissions).

## Backend CORS allowlist

The backend serves CORS responses to:

- Any extension origin when you access Rewind via `localhost` or a private IP (dev-friendly)
- Otherwise, only an explicit allowlist of extension IDs

- Set `EXTENSION_ALLOWED_CLIENT_IDS` on the `web` service to your extension ID (or a comma-separated list).
- Find the extension ID at `chrome://extensions/` (Developer Mode).

Example (recommended for non-local deployments):

`EXTENSION_ALLOWED_CLIENT_IDS=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa`

## Load unpacked
1. Open `chrome://extensions/`
2. Enable Developer Mode
3. Click “Load unpacked”
4. Select `extensions/chrome-extension-v3/`

## Shared code
Common logic lives in `extensions/extension-common/` and is synced into this extension’s `common/` directory.

After changing shared files, run:

`pnpm run extensions:sync`

## Configure
1. Open the extension Options page
2. Enter your Rewind server URL (origin, e.g. `https://rewind.example.com`)
3. Click “Save server” and grant host permission
4. Click “Log in” and complete the web login flow

## What it uses on the server
- `GET /api/extension/auth/start`
- `GET /api/extension/auth/finish`
- `GET /api/extension/status`
- `POST /api/extension/archive`
- `POST /api/extension/cookies`
- `POST /api/extension/logout`

## Notes
- Token is stored in `chrome.storage.local`.
- No cookie-session auth is used for extension endpoints.
- Cookies are optional and toggleable per site; when enabled, the popup uploads a Netscape-format cookie file via `/api/extension/cookies` before archiving.
