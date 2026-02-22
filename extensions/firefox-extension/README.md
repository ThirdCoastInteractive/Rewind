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
