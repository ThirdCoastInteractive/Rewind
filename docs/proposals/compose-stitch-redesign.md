# Proposal: Compose & Stitch UI Redesign

## Summary

A comprehensive redesign of the Compose and Stitch pages addressing layout problems, missing visual affordances, and usability gaps. The core change is replacing numerical-only crop/position inputs with the visual crop overlay already built for the Cut page, adopting a Source/Program dual-monitor layout inspired by NLE editors, and making both pages responsive across Tailwind breakpoints.

---

## Current State Assessment

### Screenshots (Current UI)

| Screenshot                               | Description                                                   |
| ---------------------------------------- | ------------------------------------------------------------- |
| `screenshots/compose-full-inspector.png` | Full compose page — 3-column layout with layer inspector open |
| `screenshots/compose-768px.png`          | Compose at 768px — broken layout, unusable canvas             |
| `screenshots/stitch-library.png`         | Stitch project library — empty state                          |
| `screenshots/stitch-editor-empty.png`    | Stitch editor — empty timeline, clip browser                  |
| `screenshots/cut-page-overview.png`      | Cut page — video player with clip list                        |
| `screenshots/cut-page-with-clip.png`     | Cut page — clip selected, inspector + crop rows visible       |
| `screenshots/cut-page-crop-overlay.png`  | Cut page — active 9:16 crop overlay on video                  |

### Compose Page — Current Layout

```
┌────────────────────────────────────────────────────────────────┐
│  w-80 (320px)  │     flex-1 (canvas)      │  w-72 (288px)     │
│                │                           │                    │
│  Source Video   │   <canvas> preview       │  Layer Inspector   │
│  (muted,        │   (draws crops from      │  (crop scrub       │
│   no toggle)    │    source video)         │   inputs, pos,     │
│                │                           │   size, transition)│
│  Layer List    │                           │                    │
│  (add/remove   │                           │  Export Panel      │
│   crop layers) │                           │  (format, quality) │
│                │                           │                    │
├────────────────┴───────────────────────────┴────────────────────┤
│  Timeline: [+ Segment]  [Seg 1] [Seg 2] ...                   │
└────────────────────────────────────────────────────────────────┘
```

**Problems identified:**

1. **No responsive breakpoints** — Fixed `w-80` + `w-72` sidebars steal 608px, leaving almost nothing for the canvas at `md` (768px). At any viewport under ~1100px the layout is broken.

2. **Inspector is opposite the source video** — The layer inspector (right column) controls crops on the source video (left column). The user's eyes must travel across the entire screen to see the effect of each scrub input change. This is the #1 usability complaint.

3. **Numerical-only crop inputs** — Crop position (x, y) and size (w, h) are controlled exclusively via scrub inputs that display normalized 0.000–1.000 values. There is no visual feedback on the source video showing where the crop region is. The Cut page already has a full `CropOverlay` class that provides drag-to-move, resize handles, and snap guides.

4. **Source video is muted with no toggle** — The `<video>` tag has `muted` hardcoded. `compose-page.js` has zero audio/mute/volume code. Users cannot preview audio while editing, leading to the impression that "compose doesn't support audio" (it does — the ffmpeg pipeline includes full audio with `atrim`, `asetpts`, `aresample=48000`, `aformat`, and `acrossfade`).

5. **Segment timeline is minimal** — A single horizontal strip at the bottom with small clickable blocks. No visual indication of segment duration, transition type, or layer count per segment. Hard to understand at a glance.

6. **No multi-segment preview** — Preview only shows the current segment's layers drawn on canvas. Cannot play across segment boundaries or preview transitions.

7. **No undo/redo** — No state history. Any accidental change to layer properties is permanent.

8. **Canvas preview aspect ratio** — The canvas maintains the output aspect ratio but its size is whatever `flex-1` gives it, which can be tiny.

### Stitch Page — Current Layout

```
┌─────────────────────────────────────────────────┐
│  w-64 (256px)       │  flex-1                   │
│                     │                            │
│  Clip Browser       │  Preview Video             │
│  (search, add)      │  (plays selected segment)  │
│                     │                            │
│  Title Card Panel   │  Controls (play, seek,     │
│  (text, duration,   │   audio toggle, speed)     │
│   bg color)         │                            │
│                     ├────────────────────────────│
│  Segment Detail     │  Timeline                  │
│  (trim, transition) │  (h-20, proportional       │
│                     │   segment blocks)           │
│  Export Panel       │                            │
│  (format, quality)  │                            │
└─────────────────────┴────────────────────────────┘
```

**Problems identified:**

1. **No responsive breakpoints** — Fixed `w-64` sidebar. Layout breaks below ~900px.

2. **Transition controls are non-functional** — The UI renders transition type and duration fields for each segment, and the outgoing clip dropdown. However, `stitch-page.js` does NOT send transition audio type or outgoing clip to the backend. These values are silently ignored in the save/export request. The ffmpeg pipeline supports full `xfade` + `acrossfade` combinations, but the JS never transmits the user's choices.

3. **Title cards excluded from preview** — `_buildSeqSegments()` in `stitch-page.js` skips title card segments entirely. Users can add title cards but cannot preview them — they only appear in the final export.

4. **Inline pixel values in JS** — `stitch-page.js` uses hardcoded `font-size:9px`, `font-size:10px` etc. in dynamically created elements, violating the design system rule against arbitrary pixel values.

5. **No clip existence validation** — Adding a clip by ID doesn't verify the clip's video file exists on disk. If the source video was deleted, the stitch will fail at export time with no early warning.

6. **Single encoder worker** — Both compose and stitch exports run through the same encoder service (scale: 1). A long compose export blocks all stitch exports and vice versa. No priority queue.

### Audio Pipeline — Confirmed Working

Both stitch and compose include full audio processing:

```
Per-segment:  [0:a] → atrim → asetpts → aresample=48000 → aformat(fltp, stereo)
Transitions:  acrossfade (paired with video xfade)
Title cards:  anullsrc=r=48000:cl=stereo (silent audio)
Hard cuts:    2-frame acrossfade (0.066667s)
Output:       -map [finalV] -map [finalA]
Codecs:       MP4 → AAC 192k | WebM → Opus 128k
```

The user's impression that audio was missing stems from:
- Compose: source `<video>` is hardcoded `muted`, no toggle exists
- Stitch: starts muted but DOES have an audio toggle button (volume icon)
- Neither page clearly communicates "audio will be included in export"

### CropOverlay — Already Reusable

The Cut page's crop overlay (`static/js/lib/crop-overlay.js`, 375 lines) is already well-modularized:

- `CropOverlay` class with clean constructor / destroy lifecycle
- HTML/CSS overlay using `box-shadow` mask trick (no canvas needed)
- Center-point + normalized dimensions coordinate system (matches compose's data model exactly)
- Drag-to-move entire region
- Resize handle (bottom-right corner)
- Snap guides: rule-of-thirds, golden ratio, halves, ninths (8px threshold)
- Aspect ratio lock support

**Changes needed to reuse in Compose:**

1. Replace `this.editor.video` reference with a direct video element parameter
2. Replace `this.editor.selectedClipId` with a generic "active layer" callback
3. Replace `document.querySelector('[data-cut-*]')` calls with a callback/event pattern
4. Add optional `enabled` flag (compose may have multiple layers, only one editable at a time)

Estimated effort: ~30 lines changed in `crop-overlay.js`, plus a new thin adapter in `compose-page.js`.

---

## Requested Improvements

### 1. Source/Program Dual-Monitor Layout

Replace the current 3-column layout with an NLE-inspired dual-pane design:

```
┌──────────────────────────────────────────────────────────────┐
│  SOURCE MONITOR              │  PROGRAM MONITOR              │
│  ┌────────────────────────┐  │  ┌────────────────────────┐   │
│  │                        │  │  │                        │   │
│  │  Source video with      │  │  │  Composed output       │   │
│  │  CropOverlay showing   │  │  │  preview (canvas)      │   │
│  │  active layer's crop   │  │  │                        │   │
│  │                        │  │  │                        │   │
│  └────────────────────────┘  │  └────────────────────────┘   │
│  [Layer Props] [Crop Lock]   │  [Play] [Seek] [Audio] [Speed]│
├──────────────────────────────┴───────────────────────────────┤
│  Inspector Bar (collapsible)                                  │
│  [Layers: + Add] [L1 selected] [L2] │ Pos X/Y  Size W/H     │
│  [Canvas: 1080×1920  #000]          │ Transition: xfade 0.5s │
├───────────────────────────────────────────────────────────────┤
│  Timeline                                                     │
│  ┌──────────┬──────────┬──────────┬──────────┐               │
│  │ Seg 1    │⟷ xfade  │ Seg 2    │⟷ fade   │ Seg 3  ...    │
│  │ 0:00-0:15│  0.5s    │ 0:15-0:45│  0.3s   │               │
│  │ 3 layers │          │ 2 layers │         │               │
│  └──────────┴──────────┴──────────┴──────────┘               │
└───────────────────────────────────────────────────────────────┘
```

**Key principles:**

- **Source monitor** (left): Shows the original video with the `CropOverlay` drawn for the currently selected layer. Dragging the crop region updates the layer's crop values in real time. The video plays audio here (with mute toggle).
- **Program monitor** (right): Shows the composed canvas output. All layers rendered in their positions. This is the "what you'll get" preview.
- **Inspector bar** (below monitors): Horizontal strip with layer list + selected layer properties. Always visible, always next to the video. No more scrub inputs on the opposite side of the screen.
- **Timeline** (bottom): Redesigned with clear segment blocks showing duration, layer count per segment, and transition type/duration between segments.

### 2. Visual Crop Editing

Replace numerical scrub inputs as the PRIMARY crop editing method:

- Clicking a layer in the inspector selects it → CropOverlay appears on the Source Monitor showing that layer's crop region
- Drag the crop region to reposition → updates layer crop x/y in real time
- Drag resize handle → updates layer crop w/h in real time
- Aspect ratio lock toggle per layer
- Snap guides (thirds, golden ratio) visible during drag
- Scrub inputs remain as SECONDARY precise controls in the inspector bar, synced bidirectionally with the overlay

### 3. Remove/Redesign the Segment Timeline

Replace the current minimal timeline strip with a proper segment timeline:

- Each segment rendered as a block with width proportional to its duration
- Segment blocks show: duration label, number of layers, thumbnail preview
- Transition indicators between segments showing type + duration
- Click segment to select it → Source/Program monitors update
- Drag segment edges to adjust in/out points
- Right-click context menu: duplicate segment, delete, insert before/after
- `+ Segment` button creates a new segment extending from the end of the last one

### 4. Source Video Audio Toggle

- Remove hardcoded `muted` from the source `<video>` tag
- Add mute/unmute button with volume icon (matching stitch's existing pattern)
- Show a clear indicator: "Audio will be included in export" in the export panel
- Consider: play audio from source monitor only (not program canvas, which is visual-only)

---

## Additional Issues to Fix

### Critical

| #   | Issue                                                                                                                                                   | Location                   | Impact             |
| --- | ------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------- | ------------------ |
| 1   | **No audio stream check** — Compose ffmpeg pipeline assumes source video has audio. Videos without audio tracks will cause the pipeline to crash.       | `pkg/ffmpeg/compose.go`    | Export crashes     |
| 2   | **Stitch transition data not sent** — Transition audio type and outgoing clip selection are rendered in UI but never included in save/export API calls. | `static/js/stitch-page.js` | Silent data loss   |
| 3   | **Stitch title cards not in preview** — `_buildSeqSegments()` skips title card segments, so users cannot preview them before export.                    | `static/js/stitch-page.js` | Misleading preview |

### High

| #   | Issue                                                                                                                                                                    | Location                        | Impact                      |
| --- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------- | --------------------------- |
| 4   | **No responsive layout** — Both compose and stitch use fixed-width sidebars with no breakpoint-aware classes. Below ~1100px (compose) or ~900px (stitch), layouts break. | `compose.templ`, `stitch.templ` | Unusable on smaller screens |
| 5   | **No undo/redo** — No state history in compose. Accidental changes to layer properties are permanent.                                                                    | `compose-page.js`               | User frustration            |
| 6   | **No multi-segment preview** — Compose preview shows only current segment. Cannot play across segment boundaries or preview transitions.                                 | `compose-page.js`               | Incomplete preview          |
| 7   | **Transition popup overflow** — Compose transition popup may render off-screen when canvas is small.                                                                     | `compose.templ`                 | UI clipping                 |

### Medium

| #   | Issue                                                                                                                                   | Location             | Impact                  |
| --- | --------------------------------------------------------------------------------------------------------------------------------------- | -------------------- | ----------------------- |
| 8   | **Inline pixel values** — `stitch-page.js` uses `font-size:9px`, `font-size:10px` in dynamic DOM elements, violating the design system. | `stitch-page.js`     | Design system violation |
| 9   | **No clip validation** — Stitch doesn't verify clip source video exists on disk before adding to timeline.                              | `stitch-page.js`     | Late failure at export  |
| 10  | **Single encoder worker** — Compose and stitch exports share one encoder service. Long exports block everything.                        | `docker-compose.yml` | Export queue bottleneck |
| 11  | **No export progress in UI** — Compose export status is polled but there's no real-time progress bar or percentage.                     | `compose.templ`      | UX gap                  |

---

## Proposed Responsive Layouts

### Breakpoint Definitions

From `tailwind.config.js`:

| Breakpoint | Width  | Target                          |
| ---------- | ------ | ------------------------------- |
| `sm`       | 640px  | Mobile landscape                |
| `md`       | 768px  | Tablet portrait                 |
| `lg`       | 1024px | Tablet landscape / small laptop |
| `xl`       | 1280px | Standard laptop                 |
| `2xl`      | 1536px | Desktop                         |
| `ultra`    | 2400px | Ultra-wide monitors             |

### Compose Page Layouts

#### `sm` / `md` (640–1023px) — Stacked Single-Pane

At narrow viewports, dual panes don't fit. Stack everything vertically with tabs:

```
┌──────────────────────┐
│  [Source] [Program]  │ ← tab toggle
│  ┌──────────────────┐│
│  │                  ││
│  │  Video/Canvas    ││
│  │  (full width)    ││
│  │                  ││
│  └──────────────────┘│
│  [▶] [⏮] [🔊] [1x]  │
├──────────────────────┤
│  Inspector (accordion)│
│  ▸ Layers  ▸ Canvas  │
│  ▸ Export             │
├──────────────────────┤
│  Timeline (h-16)     │
│  [S1][⟷][S2][⟷][S3] │
└──────────────────────┘
```

- Source/Program toggle via tabs — only one visible at a time
- CropOverlay works at full width when Source tab is active
- Inspector sections collapse into accordions to save vertical space
- Timeline shrinks to `h-16` with minimal segment blocks

#### `lg` (1024–1279px) — Side-by-Side with Compact Inspector

Dual panes fit, but tightly. Inspector is a collapsible horizontal bar:

```
┌─────────────────────────────────────────┐
│  SOURCE (50%)       │  PROGRAM (50%)    │
│  ┌────────────────┐ │ ┌───────────────┐ │
│  │ Video +        │ │ │ Canvas        │ │
│  │ CropOverlay    │ │ │ output        │ │
│  └────────────────┘ │ └───────────────┘ │
│  [🔊] [Crop Lock]  │ [▶] [Seek] [1x]  │
├─────────────────────┴───────────────────┤
│  ▸ Inspector [expand]                    │
│  Layers: [L1•] [L2] [+] │ Pos/Size     │
├──────────────────────────────────────────┤
│  Timeline (h-20)                         │
└──────────────────────────────────────────┘
```

- Source and Program monitors each get 50% width
- Inspector bar starts collapsed (one-line summary), expands on click
- Scrub inputs for precise values in expanded inspector
- Timeline gets full width

#### `xl` / `2xl` (1280–2399px) — Full Dual-Monitor

The primary layout. Everything visible, no collapsing needed:

```
┌──────────────────────────────────────────────────────────────┐
│  SOURCE MONITOR              │  PROGRAM MONITOR              │
│  ┌────────────────────────┐  │  ┌────────────────────────┐   │
│  │                        │  │  │                        │   │
│  │  Source video           │  │  │  Composed canvas       │   │
│  │  + CropOverlay         │  │  │  output preview        │   │
│  │                        │  │  │                        │   │
│  └────────────────────────┘  │  └────────────────────────┘   │
│  [🔊 Mute] [📐 Aspect Lock] │  [▶ Play] [⏮⏭] [🔊] [1x]   │
├──────────────────────────────┴───────────────────────────────┤
│  Inspector Bar                                                │
│  Layers: [+ Add] [L1 ●] [L2] [L3] │ X ▸ 0.250  Y ▸ 0.333  │
│  Canvas: 1080×1920 #000000         │ W ▸ 0.500  H ▸ 0.500  │
│                                     │ Transition: xfade 0.5s │
├───────────────────────────────────────────────────────────────┤
│  Timeline (h-24)                                              │
│  ┌───────────┬─────┬──────────────┬─────┬──────────┐         │
│  │ Segment 1 │xfade│ Segment 2    │fade │ Segment 3│         │
│  │ 0:00-0:15 │0.5s │ 0:15-0:45   │0.3s │ 0:45-1:00│         │
│  │ 3 layers  │     │ 2 layers    │     │ 1 layer  │         │
│  └───────────┴─────┴──────────────┴─────┴──────────┘         │
└───────────────────────────────────────────────────────────────┘
```

- Full side-by-side monitors
- Inspector always expanded with all controls visible
- Scrub inputs for precision, CropOverlay for visual editing
- Timeline shows segment metadata (duration, layers, transition type)

#### `ultra` (2400px+) — Extra-Wide with Floating Panels

Ultra-wide monitors get more canvas space with optional floating panels:

```
┌────────────────────────────────────────────────────────────────────────┐
│    SOURCE MONITOR (45%)           │     PROGRAM MONITOR (45%)     │   │
│    ┌──────────────────────────┐   │    ┌─────────────────────────┐│   │
│    │                          │   │    │                         ││ P │
│    │  Source + CropOverlay    │   │    │   Composed output       ││ r │
│    │  (large)                 │   │    │   (large)               ││ e │
│    │                          │   │    │                         ││ s │
│    └──────────────────────────┘   │    └─────────────────────────┘│ e │
│    Controls                       │    Controls                   │ t │
├───────────────────────────────────┴───────────────────────────────┤ s │
│  Inspector Bar (same as xl)                                       │   │
├───────────────────────────────────────────────────────────────────┤   │
│  Timeline (h-28, extra detail)                                    │   │
└───────────────────────────────────────────────────────────────────┘   │
```

- Optional right sidebar for canvas presets / recent projects
- Monitors get more breathing room
- Timeline can show waveform or thumbnail strip per segment

### Stitch Page Layouts

#### `sm` / `md` (640–1023px) — Stacked

```
┌──────────────────────┐
│  Preview Video       │
│  (full width)        │
│  [▶] [🔊] [1x]      │
├──────────────────────┤
│  ▸ Clips  ▸ Title    │ ← accordion sections
│  ▸ Segment ▸ Export   │
├──────────────────────┤
│  Timeline (h-16)     │
└──────────────────────┘
```

#### `lg`+ (1024px+) — Side Panel

```
┌──────────────────────────────────────────┐
│  w-72 (sidebar)     │  flex-1            │
│  Clip Browser       │  Preview Video     │
│  Title Card         │  Controls          │
│  Segment Detail     ├────────────────────│
│  Export Panel       │  Timeline (h-24)   │
└─────────────────────┴────────────────────┘
```

- Sidebar widens from `w-64` to `w-72` for better form element sizing
- Responsive collapse at `lg` breakpoint
- Timeline gets proportional segment blocks with metadata

---

## Implementation Plan

### Phase 1: Fix Critical Bugs (no UI changes)

1. **Audio stream detection** — Check if source video has audio before building audio filter chain in `pkg/ffmpeg/compose.go` and `pkg/ffmpeg/stitch.go`. If no audio, use `anullsrc` to generate silence.
2. **Stitch transition data** — Fix `stitch-page.js` to include transition audio type and outgoing clip in save/export API calls.
3. **Stitch title card preview** — Implement title card rendering in `_buildSeqSegments()` using canvas text drawing.

### Phase 2: Compose Layout Redesign

1. **Refactor `CropOverlay`** — Decouple from Cut page specifics. Accept video element and callbacks as constructor parameters. Export as ES module.
2. **New `compose.templ` layout** — Implement dual-monitor Source/Program layout with responsive breakpoints.
3. **Integrate CropOverlay into compose** — Show overlay on Source Monitor for selected layer. Bidirectional sync with scrub inputs.
4. **Source audio toggle** — Remove hardcoded `muted`, add volume button.
5. **Inspector bar** — Horizontal layout below monitors with layer list + selected layer properties.
6. **Responsive breakpoints** — Implement `sm`/`md` stacked, `lg` compact, `xl`/`2xl` full layouts.

### Phase 3: Timeline Redesign

1. **New timeline component** — Proportional segment blocks with duration, layer count, transition indicators.
2. **Segment edge dragging** — Adjust in/out points by dragging segment edges.
3. **Context menu** — Duplicate, delete, insert segment.
4. **Multi-segment preview** — Play across segment boundaries in Program Monitor.

### Phase 4: Stitch Improvements

1. **Responsive stitch layout** — Collapse sidebar at `md` breakpoint.
2. **Fix transition controls** — Wire up all transition fields to API.
3. **Title card preview** — Canvas-based rendering in preview player.
4. **Remove inline pixel values** — Replace with Tailwind classes.
5. **Clip validation** — Verify source video exists before adding.

### Phase 5: Quality of Life

1. **Undo/redo** — State history stack for compose layer operations.
2. **Encoder scaling** — Allow multiple encoder workers, add priority queue.
3. **Export progress** — Real-time progress bar via SSE.
4. **Audio indicator** — Clear "Audio included" badge in export panels.

---

## Open Questions

1. **Should compose accept multiple source videos?** Currently limited to one source video with multiple crop regions. Multi-source would require significant data model changes.

2. **Should stitch accept compose outputs as clips?** A compose export could be used as a segment in a stitch timeline, enabling a workflow: compose shorts → stitch compilation.

3. **Keyboard shortcuts for compose?** The Cut page has a comprehensive shortcut system. Compose would benefit from: `1-9` select layer, `[`/`]` prev/next segment, `Space` play/pause, `Ctrl+Z` undo.

4. **Should the CropOverlay support all four corners for resize?** Currently only bottom-right. Four-corner + edge resize would be more intuitive but adds complexity.

5. **Preview quality vs performance** — The canvas preview redraws every frame via `requestAnimationFrame`. At high resolution or with many layers, this could be slow. Should we offer a quality/performance toggle?
