# Browser Extension

Rewind includes browser extensions for Chrome and Firefox that let you archive videos with one click while browsing.

## Chrome / Chromium

1. Open `chrome://extensions` in your browser
2. Enable **Developer mode** (toggle in the top-right corner)
3. Click **Load unpacked**
4. Select the `extensions/chrome-extension-v3/` folder from the Rewind repository

The extension icon appears in your toolbar. Click it or right-click any page to send a video URL to Rewind.

## Firefox

1. Open `about:debugging#/runtime/this-firefox`
2. Click **Load Temporary Add-on**
3. Select any file inside the `extensions/firefox-extension/` folder

Note: Temporary add-ons in Firefox are removed when the browser restarts. For persistent installation, the extension needs to be signed or installed via `about:config` with `xpinstall.signatures.required` set to `false`.

## Authentication

The extension authenticates via a flow that opens the Rewind login page in a new tab, then redirects back with a session token. This only needs to happen once per browser.

If the extension cannot connect, check:

1. **Server URL** - make sure the extension is pointed at the correct Rewind instance URL
2. **Client ID** - your Rewind instance must have the extension's client ID in the `EXTENSION_ALLOWED_CLIENT_IDS` environment variable
3. **Network** - the browser must be able to reach your Rewind server

## Usage

Once authenticated:

- **Toolbar button** - click the Rewind icon on any page with a video to archive it
- **Context menu** - right-click on any page and select the Rewind option to send the current URL
- **Status** - the extension shows whether the URL has already been archived

The extension sends the URL to your Rewind instance, which creates a download job just like pasting the URL into the Home page. You can monitor progress from the Jobs page.
