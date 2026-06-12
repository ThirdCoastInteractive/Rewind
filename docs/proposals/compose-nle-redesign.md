# Proposal: Compose NLE Redesign

## Summary

Redesign the Compose editor into a proper non-linear editor (NLE) with graphical manipulation of layers, visual filter/effect controls, a multi-track timeline, and drag-based crop/position editing. The current compose page uses numeric inputs for all spatial properties and has no filter system at all. This proposal transforms it into a visual editing environment while keeping the server-side FFmpeg render pipeline and DataStar reactivity model.

---

## Problem Statement

### Numeric-only spatial editing

Compose layers define crop (normalized 0–1) and position (pixel coordinates) through scrub-input number fields. Users type `crop.x = 0.35, crop.width = 0.5` to frame a shot. There is no way to drag a crop rectangle on the source video, no way to drag/resize a layer on the canvas, and no way to visually confirm spatial transforms without mentally converting numbers to screen positions.

The cut page already has a `CropOverlay` class with drag-to-move and handle-to-resize interactions. Compose has a static `drawCropOverlay()` that renders read-only rectangles on the source video — it cannot be dragged.

### No filter or effect system

The Cut page has a comprehensive filter stack (`pkg/filters/filter_defs.go`) with 35+ filter types across five categories: spatial (crop, rotate, flip), color (brightness, contrast, saturation, curves, LUT, color temperature), temporal (speed, fade), audio (volume, EQ, compressor), and overlay (text). Each filter has typed parameter definitions (range sliders, select dropdowns, color pickers, presets), live preview via canvas, and drag-to-reorder.

Compose has zero filter support. There is no way to adjust brightness on a layer, apply color grading, add text overlays, or modify audio. The FFmpeg compose pipeline (`pkg/ffmpeg/compose.go`) only does trim → crop → scale → overlay per layer, with no filter chain insertion point.

### Flat timeline

The timeline is a horizontal row of segment boxes. Each segment shows its index, duration, and layer count. There is no concept of tracks, no waveform visualization, no time ruler, and no way to see how layers relate to each other temporally. Adding, moving, and trimming segments is done through buttons and number inputs, not drag.

### Single-source limitation (now partially resolved)

The universal source types implementation added multi-source support at the data and encoder layers. Per-layer `source` overrides are stored in the timeline JSON and resolved by the encoder. However, no UI exists to set a layer's source — the compose page still shows a single "Source Video" panel with one `<video>` element. The canvas preview only draws from that one video.

---

## Current Architecture Reference

### Template layout (compose.templ)

Three-column layout:
- **Left (w-80):** Source video player, crop overlay (read-only), layer list, preset buttons (Stacked/PIP/Side×Side)
- **Center (flex-1):** Canvas `<canvas>` element, dimensions display
- **Right (w-72):** Layer inspector (numeric inputs), export status, export history

Bottom bar: Flat timeline (horizontal segment boxes), "+ Segment" button.

### Signal state

```
_composeProjectId, _composeVideoId, _composeTitle
_composeFormat, _composeQuality
_composeCanvas        {width, height, color}
_composeTimeline      [{start_time, end_time, layers: [{crop, position, z, source?}], transition?}]
_composeSelectedSegIdx, _composeSelectedLayerIdx
_composeDirty, _composeSaving, _composeExporting
_composeVideoDuration, _composeVideoWidth, _composeVideoHeight
```

### JS rendering pipeline

Four `data-effect` watchers drive rendering:
1. `renderComposeTimeline()` — segment boxes in bottom bar
2. `renderComposeLayers()` — layer list in left sidebar
3. `renderComposeLayerInspector()` — numeric inputs in right sidebar
4. `renderComposeCanvas()` — draws layers on `<canvas>` from source `<video>` using `drawImage()` with crop→position transform

### FFmpeg pipeline (compose.go)

Per-segment per-layer: `[inputIdx:v] trim → crop (center-based, unnormalized to px) → scale (to position size)` → overlay all layers in z-order on color canvas → xfade between segments.

### Filter system (cut page only)

`pkg/filters/filter_defs.go` defines `FilterParam` structs with `Key, Label, Type, Min, Max, Step, DefaultVal, Decimals, Options, Presets`. Cut page stores filter stack as `[{type, params}]` signal array. Backend renders filter cards via SSE, JS applies live preview.

---

## Design

### 1. Draggable Canvas Layers

Replace the static canvas preview with interactive layer manipulation.

**Canvas interaction modes:**
- **Select mode (default):** Click a layer on canvas to select it. Drag to reposition. Corner/edge handles to resize. Shift+drag to maintain aspect ratio. Crop and position signals update live.
- **Crop mode:** When active, clicking a layer shows the crop region on the source with drag-to-resize handles (porting the cut page's `CropOverlay` pattern). Changes update `layer.crop` in real time.

**Implementation approach:**

The current `renderComposeCanvas()` does `requestAnimationFrame` looping with `ctx.drawImage()`. Add hit-testing on click/mousedown:

```js
// Hit-test: iterate layers top-to-bottom (reverse z-order) to find clicked layer
function hitTestLayer(canvasX, canvasY, layers, scale) {
  for (let i = layers.length - 1; i >= 0; i--) {
    const pos = layers[i].position;
    if (canvasX >= pos.x * scale && canvasX <= (pos.x + pos.width) * scale &&
        canvasY >= pos.y * scale && canvasY <= (pos.y + pos.height) * scale) {
      return i;
    }
  }
  return -1;
}
```

On mousedown → track drag delta → update `position.x`/`position.y` through `composeUpdateLayerField()`. Corner handles follow the same CropOverlay handle pattern from the cut page.

**Visual feedback:**
- Selected layer: white 2px border, corner handles (8×8px squares)
- Unselected layers: dashed white/30 border
- Drag: show guide lines when aligned to canvas center, edges, or other layers (snap at ±4px)

**Signals added:**
```
_composeInteractionMode: 'select' | 'crop'
```

**Templ changes:**
- Add mode toggle buttons above canvas: `[Select] [Crop]`
- Canvas container gets `cursor: move` in select mode, `cursor: crosshair` in crop mode

### 2. Draggable Crop on Source Video

Port the cut page's `CropOverlay` class into compose for per-layer crop editing.

**Current state:** `drawCropOverlay()` in `compose-page.js` renders rectangles with `innerHTML` — no interaction.

**New behavior:**
- When a layer is selected and interaction mode is `crop`, the source video panel shows the crop rectangle with draggable handles.
- Drag the rectangle body to move `crop.x`/`crop.y`.
- Drag corner handles to resize `crop.width`/`crop.height`.
- Center-based coordinates: as the user drags, x/y shift with the center, width/height resize from center.
- Changes update the DataStar signals, which re-renders the canvas preview in real time.

**Source video per layer:** With multi-source support, each layer can reference a different video/export. The source video panel should show the active layer's source. When the user selects a layer whose source differs from the current `<video>` element's `src`, update the video element's source:

```js
function updateSourceVideoForLayer(layer) {
  const video = getSourceVideo();
  const src = resolveLayerStreamURL(layer);
  if (video.src !== src) {
    video.src = src;
    video.load();
  }
}
```

### 3. Per-Layer Filter Stack

Bring the cut page's filter system into compose, applied per-layer.

**Data model change:** Add `filters` array to each layer in the timeline JSON:

```json
{
  "crop": { "x": 0.5, "y": 0.5, "width": 1.0, "height": 1.0 },
  "position": { "x": 0, "y": 0, "width": 1080, "height": 1920 },
  "z": 0,
  "filters": [
    { "type": "brightness", "params": { "value": 0.2 } },
    { "type": "saturation", "params": { "value": 1.4 } }
  ]
}
```

**UI in layer inspector (right panel):**

When a layer is selected, show below the crop/position numeric inputs:
- **Filter stack:** Ordered list of applied filters with reorder/remove controls
- **Add filter dropdown:** Grouped by category (Color, Spatial, Audio, Temporal, Overlay), each item shows icon + label from `pkg/filters` definitions
- **Per-filter card:** Expand to show parameter controls (range sliders, selects, color pickers) — same UI pattern as the cut page's filter cards, rendered server-side via SSE

**Live preview:** Apply lightweight canvas-based approximations for visual filters:
- Brightness/contrast/saturation: CSS `filter` property on a temporary canvas or `ctx.filter` string
- Color temperature: color matrix approximation
- Grayscale/sepia: `ctx.filter = 'grayscale(1)'` / sepia tint
- Non-previewable filters (LUT, denoise, sharpen): show badge "render-only"

**FFmpeg pipeline change:** In `pkg/ffmpeg/compose.go`, after crop → scale per layer, insert the filter chain before overlay:

```
[inputIdx:v] trim → crop → scale → [filter1] → [filter2] → ... → [layerN_out]
```

The filter chain uses the same FFmpeg filter mappings as the clip encoder. Reuse `pkg/filters` for resolving filter type → FFmpeg filter string.

**Backend handler:** New SSE endpoint `GET /api/compose/filter-cards` that accepts `layerIdx` + `filterStack` signals, renders the filter parameter cards using the same `pkg/filters.ParamsForFilterType()` and templ components as the cut page.

### 4. Multi-Track Timeline

Replace the flat segment bar row with a proper multi-track timeline.

**Layout:**

```
  Time ruler:   |0:00    |0:05    |0:10    |0:15    |0:20    |
  Track 0:      [====== Seg 1 ======][◇][==== Seg 2 ====]
  Track 1:      (future: independent audio-only tracks)
  ------------- waveform area (if audio source available) ----
```

**Track features:**
- **Time ruler:** Horizontal scale with second markers, zoom with scroll wheel or +/- buttons
- **Segment blocks:** Width proportional to `end_time - start_time`. Drag edges to trim. Drag body to move position.
- **Transition diamonds (◇):** Between segments. Click to open transition type/duration popup (existing `composeOpenTransitionPopup`)
- **Playhead:** Vertical line at current video time. Click ruler to seek.
- **Zoom level signal:** `_composeTimelineZoom` (px per second, default 40)

**Drag interactions:**
- **Drag segment edge:** Changes `start_time` or `end_time`, updates duration display in real time
- **Drag segment body:** Reorders segments in the timeline array (equivalent to current `composeMoveSegment`)
- **Drag from source browser:** Drop to insert a new segment at position (future)

**Signals added:**
```
_composeTimelineZoom: 40   // px per second
_composePlayheadTime: 0    // current seek position
```

**Templ changes:**
- Replace bottom bar's `<div id="compose-timeline">` with a taller container (h-24 to h-32)
- Add zoom controls: `[−] [+]` buttons, zoom level readout
- Add time ruler div above track divs

**JS implementation:**
- `renderComposeTimeline()` rewritten to calculate segment widths based on duration × zoom
- Segment divs get `mousedown` handlers for drag-to-trim and drag-to-reorder
- `requestAnimationFrame` loop moves playhead div to match video currentTime

### 5. Layer Source Picker

Complete the UI for the universal source types data layer.

**Per-layer source indicator:** In the layer list (left panel), each layer shows its source below the z-order label:

```
  L0  100% × 100%   [↓] [↑] [✕]
  src: Main Video
  
  L1  40% × 40%     [↓] [↑] [✕]
  src: Compose: "Podcast Layout"
```

**Source picker popup:** Click the source label to open a popup with the same unified search from the stitch source browser:
- Filter tabs: All | Videos | Clips | Compose | Stitch
- Search input
- Results list with type badges
- Click to set `layer.source = { type, video_id/clip_id/export_job_id }`
- "Default (project video)" option to clear override

**Handler:** Reuse `SearchSourcesForStitch` query (rename to `SearchSources` in a future cleanup, or use as-is). New SSE endpoint `GET /api/compose/layer-sources` that renders the browser results.

**Multi-video canvas preview:** When layers reference different sources, maintain a map of `<video>` elements (one per unique input path). The canvas `drawImage()` reads from the correct video for each layer:

```js
const _sourceVideos = new Map(); // streamURL → <video> element

function getOrCreateVideo(streamURL) {
  if (_sourceVideos.has(streamURL)) return _sourceVideos.get(streamURL);
  const v = document.createElement('video');
  v.src = streamURL;
  v.preload = 'metadata';
  v.muted = true;
  v.style.display = 'none';
  document.body.appendChild(v);
  _sourceVideos.set(streamURL, v);
  return v;
}
```

The `renderComposeCanvas()` draw loop resolves each layer's stream URL and calls `drawImage()` from the correct video element.

### 6. Keyboard Shortcuts

Add keyboard shortcuts for common compose operations, consistent with the cut page's shortcut patterns.

| Key | Action |
|-----|--------|
| `Space` | Play/pause source video |
| `Delete` / `Backspace` | Remove selected layer or segment |
| `Ctrl+Z` | Undo (signal-level snapshot stack) |
| `Ctrl+Shift+Z` | Redo |
| `Ctrl+D` | Duplicate selected layer |
| `[` / `]` | Select previous / next layer |
| `←` / `→` | Nudge selected layer position by 1px (10px with Shift) |
| `Ctrl+←` / `Ctrl+→` | Select previous / next segment |
| `+` / `-` | Zoom timeline in / out |
| `V` | Switch to Select mode |
| `C` | Switch to Crop mode |
| `Ctrl+S` | Force save (bypass debounce) |

**Implementation:** Single `keydown` listener on `[data-compose-page]` container, dispatching by key. Check `e.target.tagName` to avoid intercepting input/textarea typing.

### 7. Undo/Redo

Implement undo for compose via a timeline snapshot stack.

**Signal-level approach:** Before each mutation (add/remove/move segment/layer, update field, apply preset), push the current `_composeTimeline` JSON to an undo stack. Ctrl+Z pops and restores, pushing the current state to a redo stack.

```js
const _undoStack = [];
const _redoStack = [];
const MAX_UNDO = 50;

function pushUndo() {
  const timeline = getTimeline();
  _undoStack.push(JSON.stringify(timeline));
  if (_undoStack.length > MAX_UNDO) _undoStack.shift();
  _redoStack.length = 0;
}

function undo() {
  if (_undoStack.length === 0) return;
  _redoStack.push(JSON.stringify(getTimeline()));
  const prev = JSON.parse(_undoStack.pop());
  const a = ds();
  if (a) a.mergePatch({ _composeTimeline: prev, _composeDirty: true });
}
```

Wrap all `setTimeline()` calls in `pushUndo()` calls.

---

## Migration Path

### Phase 1: Interactive Canvas + Crop Overlay

**Goal:** Visual layer manipulation. No backend or schema changes.

1. Add hit-test + drag-to-move on canvas layers
2. Add resize handles on selected layer
3. Port CropOverlay drag interaction from cut page
4. Add Select/Crop mode toggle buttons
5. Add snap guides (center, edges)

All changes are in `compose-page.js` and `compose.templ`. No database migration, no encoder changes, no new endpoints.

### Phase 2: Per-Layer Filter Stack

**Goal:** Color grading, text, audio filters per layer.

1. Add `filters` array to layer JSON (backward compatible — empty array = no filters)
2. Add filter dropdown + card UI in layer inspector (right panel)
3. New SSE endpoint for rendering filter parameter cards
4. Live canvas preview for previewable filters
5. Extend `pkg/ffmpeg/compose.go` to insert filter chain after crop → scale
6. Extend encoder to read and apply `layer.filters`

Schema: No migration needed (filters live in timeline JSON column).
Backend: New handler endpoint, FFmpeg pipeline modification.

### Phase 3: Multi-Track Timeline

**Goal:** Visual timeline with proportional widths, drag trim, zoom.

1. Redesign timeline container from h-8 to h-24+
2. Time ruler + zoom controls
3. Segment widths proportional to duration × zoom level
4. Drag segment edges to trim
5. Playhead synced to video currentTime

All changes in JS + templ. No backend changes.

### Phase 4: Layer Source Picker + Multi-Video Preview

**Goal:** Complete the multi-source UI.

1. Source indicator per layer in the layer list
2. Source picker popup (reuse unified search query)
3. Multi-`<video>` element pool for canvas preview
4. Source video panel switches to show selected layer's source
5. New SSE endpoint for layer source browser

Backend: New handler. Data layer already supports this from universal source types implementation.

### Phase 5: Keyboard Shortcuts + Undo/Redo

**Goal:** Power user workflow.

1. Keydown listener with shortcut dispatch
2. Undo/redo stack wrapping timeline mutations
3. Undo indicator in top bar (undo count or Ctrl+Z hint)

All changes in JS. No backend changes.

---

## Files Modified Per Phase

### Phase 1
- `static/js/compose-page.js` — hit-test, drag, resize handles, crop overlay interaction, snap guides
- `cmd/web/templates/compose.templ` — mode toggle buttons, cursor styles

### Phase 2
- `static/js/compose-page.js` — filter stack rendering, live canvas filter preview
- `cmd/web/templates/compose.templ` — filter section in inspector panel
- `cmd/web/templates/components/compose_filter_cards.templ` — SSE-rendered filter parameter cards (new)
- `cmd/web/handlers/api/compose_api/filters.go` — SSE endpoint for filter cards (new)
- `cmd/web/internal/web/server.go` — route registration
- `pkg/ffmpeg/compose.go` — insert filter chain in per-layer pipeline
- `cmd/encoder/compose.go` — read layer.filters, pass to FFmpeg builder

### Phase 3
- `static/js/compose-page.js` — timeline rewrite, zoom, drag-trim, playhead
- `cmd/web/templates/compose.templ` — taller timeline container, zoom controls, time ruler

### Phase 4
- `static/js/compose-page.js` — multi-video pool, source switching, source picker popup
- `cmd/web/templates/compose.templ` — source indicator in layer list
- `cmd/web/templates/components/compose_source_browser.templ` — source picker results (new)
- `cmd/web/handlers/api/compose_api/sources.go` — SSE endpoint for source browser (new)
- `cmd/web/internal/web/server.go` — route registration

### Phase 5
- `static/js/compose-page.js` — keyboard listener, undo/redo stack

---

## Open Questions

1. **Canvas filter preview fidelity:** CSS `filter` and `ctx.filter` cover brightness/contrast/saturation/grayscale/sepia. Filters like LUT, curves, denoise, sharpen have no lightweight canvas equivalent. Should we show a "render-only" badge, or attempt shader-based approximations via OffscreenCanvas/WebGL?

2. **Audio waveform display:** Decoding audio for waveform rendering in the browser requires `AudioContext.decodeAudioData()`. This works for cached/streamed video files but adds complexity. Is waveform display a priority, or is the time ruler + duration labels sufficient?

3. **Per-layer opacity:** Common NLE feature. Would add `opacity` field to layer JSON and `format=rgba` overlay in FFmpeg. Should this be included in Phase 1 (canvas) or Phase 2 (filters)?

4. **Text overlay editor:** The cut page filter system includes a `text` filter type. In an NLE context, text overlays typically have direct-on-canvas editing (click to type, drag to position, font/size/color controls). Should compose text be a filter or a dedicated layer type?

5. **Timeline snapping:** When dragging segments, should they snap to other segment boundaries? This helps with precise alignment but can be annoying for free-form positioning.

6. **Performance with many video sources:** The multi-video pool creates `<video>` elements per unique source. With 10+ sources, browser memory and decoding may become a concern. Should we limit concurrent video elements and swap sources on demand?
