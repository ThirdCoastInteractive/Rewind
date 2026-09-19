# Stitch editor delivery

The workspace is the only Stitch editor. Opening an owned project at its existing
`/stitch/{id}` URL migrates any legacy row into the canonical document on open.
Cut and existing project URLs remain available. Source clips, transcripts, and
archived media are never changed by project editing.

## Product contract

One wider left panel contains Edit, Captions, Layers, Assist, and history. The
preview and transport remain visible beside it. Tabs preserve selection,
playhead, zoom, and playback. A primary video sequence supports explicit gaps,
title cards, and declared transitions. Timed overlays sit above that sequence.

Humans and agents use the same typed commands. Every edit supplies an expected
revision and idempotency key. The authenticated server actor owns attribution.
Commands are atomic, revisions monotonic, and persisted history supports shared
undo/redo. A new edit after undo clears redo. Local drafts survive conflicts.

Timing links move every member together, without changing stacking order.
Canvas groups move graphics together, without changing timing. Both are flat
and independent. Individual trimming affects only the selected item. Source
captions follow their segment and are clipped for visibility without losing cues.

Caption text edits immediately preview and invalidate affected word alignment.
Genuine source word timing is preserved. Requested ranges use queued WhisperX
forced alignment in an isolated runtime; whisper.cpp remains the transcription
engine. Pending or unalignable words never get invented timings. Stale alignment
results cannot overwrite newer edits. Highlighted-word export requires valid
word timing.

Text, raster images, callouts, arrows, rectangles, ellipses, and freehand paths
have explicit geometry, style, time, visibility, lock, and stacking order.
Project assets use immutable hashes under exports. Exports and authoritative
previews capture an explicit revision and all dependencies.

## Gates

1. Canonical model, faithful legacy conversion, transactional commands,
   ownership, revision conflict handling, retry semantics, shared history.
2. Functional left-panel editor, precise editing and playback, SSE updates,
   shared MCP operations, embedded Assist with structured project context.
3. Project-owned captions, queued alignment, live highlighting, caption export.
4. All overlay types, timing links, canvas groups, authoritative rendering.
5. Disposable integration fixtures, generation/build/test targets, desktop
   checks at 1100/1280/1440 pixels, and local deployment with `make up`.

A canonical project must never pass through a legacy whole-document writer.
Original legacy data is preserved transactionally during migration on open.

## Acceptance evidence required

- Real legacy clip/video/title/export-reference fixtures retain source trims,
  transitions, gain, look, fonts, filters, and output behavior.
- HTTP and MCP commands yield equivalent documents; concurrent writes and
  duplicate requests cannot overwrite or duplicate committed edits.
- Shared undo/redo survives reconnects and mixed human/agent edits.
- Group motion preserves offsets and fails atomically when invalid.
- Caption edits survive trims, reorder, reconnect, and alignment completion.
- Reviewed word fixtures are accurate; unsupported results are explicit.
- Browser and encoded timing agree within one output frame.
- Unicode text, transparency, every shape, and missing assets are exercised.
- Queued exports remain tied to the captured revision after later edits.
- Ownership covers documents, operations, history, assets, previews, exports.
- Desktop layouts retain left-only tools, usable tabs, and visible transport.

## Verification — 2026-09-08

The canonical workspace is the Stitch editor for every owned project. Opening
`/stitch/{id}` migrates a legacy row when needed and preserves the original
legacy snapshot. Canonical projects cannot be written by the old whole-document
editor.

Passing checks in the main workspace:

- `make test` — all Go packages.
- `make test-stitch-integration` — fresh disposable schema through migration 83;
  real HTTP authentication, revision conflicts, retries, shared history, source
  bounds, multi-clip legacy trims, compilation snapshots, assets, alignment
  caching/staleness, and PostgreSQL notifications delivered over HTTP SSE.
- `make test-stitch-mcp` — SDK wire calls exercise inspect, atomic edit, retries,
  structured conflicts, undo, and foreign ownership rejection.
- `make test-stitch-render` — real FFmpeg worker exports, frame/range previews,
  MP4/WebM, Unicode/vector/image compositing, opacity, caption backgrounds,
  active-word/pause captions, declared fades, hard cuts, and explicit black gaps.
- `make test-stitch-client` — source/trim commands, serialized gestures, group
  identifiers, and real client conflict reload/reapply behavior.

Browser checks at 1100, 1280, and 1440 pixels confirm left-only workspace tools,
visible transport controls, correct preview aspect ratio, and no horizontal
page overflow. Actual gestures verified numerical trimming, caption editing,
conflict recovery, freehand point capture, Escape cancellation, timed overlay
visibility, and continued playback across workspace changes.

An isolated CPU WhisperX runtime aligned the spoken English fixture “This is a
Rewind caption alignment test.” using actual acoustic word boundaries; unsupported
language and omitted-word fixtures return explicit states. Production model
availability remains visible through job status; alignment installs separately
from the existing transcription runtime. Pending alignment never fabricates
word timestamps or permits highlighted-word export.

The browser uses immediate DOM/SVG feedback. Authoritative frame/short-preview
renders remain the reference for font metrics and wrapping. All destructive
integration fixtures use the disposable PostgreSQL database on port 15439 and
temporary media, never the archive database or download volume.

Local generation/build and deployment status is recorded after the final gate.
