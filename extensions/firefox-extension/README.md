# Rewind Firefox Extension

This is the Firefox wrapper.

- Shared logic is copied into `common/` from `extensions/extension-common/`.
- Run `pnpm extensions:sync` (or `node scripts/sync-extension-common.js`) after changing shared files.

## Load temporarily (dev)
1. Open `about:debugging#/runtime/this-firefox`
2. Click “Load Temporary Add-on…”
3. Select the file `extensions/firefox-extension/manifest.json`

Notes:
- There isn’t a “choose folder” option - picking `manifest.json` loads the entire extension directory.
- This is temporary (it goes away when Firefox restarts).

## Stable ID / server allowlist
This manifest sets `browser_specific_settings.gecko.id` to `rewind@local`.
Use that value in `EXTENSION_ALLOWED_CLIENT_IDS` on the server.

## Live streams
On a detected live tab the primary button becomes **Start archiving this stream**, with optional **From the beginning** and **Wait for stream (minutes)** controls. Cookies are still recommended for Kick and for restricted X broadcasts. The popup inspects the active tab on demand via `tabs.executeScript` (no always-on content script). X broadcast and event URLs count as live while the page shows a LIVE chip, “Started … ago”, or a player with no end.
