# Proposal: Align Stitch & Compose with Cut Page Layout

## Summary

Restructure the Stitch and Compose editors to follow the Cut page's proven layout pattern: **left sidebar with collapsible tool panels | right main content area with video and timeline**. The Cut page is the most polished, most usable editor in the app. Stitch and Compose should converge toward its conventions — not to make them identical, but to make them *feel* like siblings rather than unrelated apps.

This proposal covers **layout restructuring** and **shared component extraction**. Right now we have enormous amounts of duplicated code between the three editors — duplicated templ markup, duplicated JS helpers, duplicated Go handler logic. The layout restructure is the right time to fix this: extract shared components first, then rebuild the layouts on top of them.

This is the foundation that the existing proposals (compose-nle-redesign, compose-stitch-redesign, editor-unification) build on top of.

---

## What Cut Gets Right

The Cut page layout works because of four structural decisions:

1. **Two-panel split:** Left sidebar (tools) + right main area (video + timelines). No three-column layout, no horizontal eye-travel.

2. **Collapsible `SidebarPanel` components:** Each tool section (Clip Bank, Inspector, Color/Filters, Export) is a named, collapsible panel. Users collapse what they don't need. Panel state persists via `_local*` signals.

3. **Video is king:** The video player and timelines take all remaining space. The left sidebar is narrow (`w-full md:w-1/3 lg:w-1/4 xl:w-1/5`) and scrollable. The video viewport is never squeezed.

4. **Bottom-anchored timelines and controls:** Overview + Work Area timelines are fixed-height at the bottom. Transport controls sit centered below the video. Action buttons (SET IN, SET OUT, SAVE, CREATE) are in a bottom bar. Everything has a predictable position.

```
┌──────────────────────────────────────────────────────────┐
│ ← BACK TO WATCH          ALL VIDEOS | PRODUCER | COMPOSE│
├──────────┬───────────────────────────────────────────────┤
│ CLIP BANK│                                               │
│ (list)   │             VIDEO PLAYER                      │
│          │             (flex-1, dominant)                 │
├──────────┤                                               │
│ INSPECTOR│        ┌─────────────────────────┐            │
│ (context)│        │ ⏮ ◁ ▶ ▷ ⏭  ↻  00:00/00:00│            │
├──────────┤        └─────────────────────────┘            │
│ COLOR /  ├───────────────────────────────────────────────┤
│ FILTERS  │ OVERVIEW  ▐░░█░░░▐░░░░░░░░░░░░░░░░░░░░░░░░▐ │
│          ├───────────────────────────────────────────────┤
├──────────┤ WORK AREA  [filmstrip thumbnails............] │
│ EXPORT   ├───────────────────────────────────────────────┤
│          │ ◁◁ ▷▷ 🔍- 🔍+ 📷 ≡  │ SET IN SET OUT  SAVE CREATE│
└──────────┴───────────────────────────────────────────────┘
```

---

## How Stitch and Compose Diverge

### Stitch — Current Layout

```
┌─────────────────────────────────────────────────┐
│ ← STITCH  [title]            MP4/WebM High/Max EXPORT│
├──────────────┬──────────────────────────────────┤
│ SOURCE       │                                   │
│ BROWSER      │    PREVIEW VIDEO                  │
│ (search,     │    (black until segment selected) │
│  type filter,│                                   │
│  sort,       │    "Select a segment to preview"  │
│  paginated   │                                   │
│  results —   │                                   │
│  fills most  │                                   │
│  of sidebar) │                                   │
├──────────────┤                                   │
│ TITLE CARD   │                                   │
│ (text,       ├──────────────────────────────────-┤
│  duration)   │ TIMELINE  ~0:06 (-2.6s overlaps)  │
├──────────────┤ [Hello Test ][Second Card][Third ] │
│ SEGMENT      │ 3 segment(s)     Delete Project   │
│ DETAIL       │                                   │
│ (hidden below│                                   │
│  scroll)     │                                   │
├──────────────┤                                   │
│ EXPORTS      │                                   │
└──────────────┴──────────────────────────────────-┘
```

**Problems:**

1. **Source browser dominates the sidebar.** It fills most of the 256px-wide left column with search, type filters, sort buttons, and a long scrollable result list. Everything below it (title card, segment detail, exports) is pushed off-screen on most viewports.

2. **Segment detail is invisible.** When you click a segment in the timeline, the detail panel appears *below* the source browser in the sidebar. Users must scroll past the entire source browser to see or edit it. This is the worst UX in the app — the inspector for the *selected* thing is hidden.

3. **Title card creation is a separate panel** instead of being part of the segment inspector. To add a title card, you open the "ADD TITLE CARD" panel, fill in text and duration, click add. But once added, editing it happens in the segment detail panel (if you can find it). Split UX.

4. **Fixed `w-64` sidebar.** No responsive breakpoints. Below ~900px the layout breaks.

5. **Timeline is 80px tall** (`h-20`). Segments are proportional blocks but too short to show useful information. Transition diamonds between segments are tiny.

6. **Transition editing** uses a floating popup that appears contextually. Hard to discover, hard to position.

### Compose — Current Layout

```
┌──────────────────────────────────────────────────────────────┐
│ ← COMPOSE  [title]        9:16 1:1 16:9  MP4/WebM  EXPORT  │
├───────────────┬──────────────────────────┬───────────────────┤
│ w-80 (320px)  │    flex-1 (canvas)       │  w-72 (288px)    │
│               │                          │                   │
│ Source Video  │  ┌──────────┐            │ LAYER INSPECTOR  │
│ (reference    │  │          │            │ (crop x/y/w/h    │
│  + crop       │  │ composed │  1080×1920 │  position x/y/   │
│  overlay)     │  │ canvas   │            │  w/h, opacity,   │
│               │  │ preview  │            │  z-order)        │
│ ▶ 0:00/9:16  │  │          │            │                   │
│ ──────────── │  └──────────┘            │ EXPORTS          │
│               │                          │ (status, history)│
│ LAYERS        │                          │                   │
│ (list, +Add)  │                          │                   │
│               │                          │                   │
│ SOURCE        │                          │                   │
│ BROWSER       │                          │                   │
├───────────────┴──────────────────────────┴───────────────────┤
│ TIMELINE    [Seg 1     ][◇][Seg 2          ]  60px/s + Seg  │
└──────────────────────────────────────────────────────────────┘
```

**Problems:**

1. **Three-column layout.** Left sidebar (320px) + right inspector (288px) = 608px stolen from the canvas. On a 1280px screen, the canvas preview gets 672px for a portrait (9:16) image — most of that is black letterboxing. The actual composed output preview is tiny.

2. **Inspector is on the *opposite side* from the source video.** You adjust crop positions in the right column. The source video showing those crops is in the left column. Your eyes cross the entire screen width for every edit. This is the #1 usability complaint identified by the existing redesign proposal.

3. **Layer list and layer inspector are separated.** The layer list is in the left sidebar; the layer inspector is in the right sidebar. To select a layer and edit it, you interact with two panels 900+ pixels apart.

4. **Source browser is buried** at the bottom of the already-tall left sidebar, collapsed by default. Finding and adding sources requires scrolling down the left sidebar and expanding a panel.

5. **No responsive breakpoints.** Fixed pixel-width sidebars.

---

## Proposed Layout: Both Editors

### Principle: Follow Cut's Pattern

Both Stitch and Compose adopt the same two-panel split as Cut:

- **Left sidebar:** Scrollable column of collapsible `SidebarPanel` components. Narrow by default, responsive with Tailwind breakpoints (`w-full md:w-1/3 lg:w-1/4 xl:w-1/5`).
- **Right main area:** Video/canvas preview (flex-1, dominant), transport controls, timeline, and a bottom action bar.

The sidebar panels change per editor, but the structural pattern is identical across all three.

---

### Stitch — Proposed Layout

```
┌──────────────────────────────────────────────────────────────┐
│ ← STITCH  [title input]           ✓ Saved  MP4/WebM  EXPORT│
├──────────────┬───────────────────────────────────────────────┤
│ SEQUENCE ▾   │                                               │
│ ┌──────────┐ │                                               │
│ │1. Intro  │ │        PREVIEW VIDEO / TITLE CARD             │
│ │  T 3.0s  │ │        (flex-1, fills available space)        │
│ ├──────────┤ │                                               │
│ │◇ fade 0.5│ │        Shows:                                 │
│ ├──────────┤ │        - Selected segment's video/clip        │
│ │2. Clip A │ │        - Title card render                    │
│ │  🎬 12.5s│ │        - Transition preview (canvas)          │
│ ├──────────┤ │        - "Select a segment" empty state       │
│ │◇ wipe 0.3│ │                                               │
│ ├──────────┤ │                                               │
│ │3. Full V │ │                                               │
│ │  📹 45.0s│ │   ┌──────────────────────────────────────┐    │
│ └──────────┘ │   │ ⏮ ◁ ▶ ▷ ⏭  🔊  00:00 / 00:45      │    │
│ [+ Source]   │   └──────────────────────────────────────┘    │
│ [+ Title]    │                                               │
├──────────────┼───────────────────────────────────────────────┤
│ INSPECTOR ▾  │ TIMELINE                            ▢ Clear  │
│              │ ┌───────┬──┬────────────┬──┬────────────────┐ │
│ Type: Title  │ │T Intro│◇ │🎬 Clip A   │◇ │📹 Full Video  │ │
│ Text: [____] │ │ 3.0s  │  │  12.5s     │  │    45.0s      │ │
│ Duration: 3s │ └───────┴──┴────────────┴──┴────────────────┘ │
│ BG: [#000]   │                                               │
│ Text: [#fff] │ 3 segment(s)  ~58s total     Delete Project  │
│              ├───────────────────────────────────────────────┤
│ Transition → │                                               │
│ Type: [fade] │                                               │
│ Duration: 0.5│                                               │
│              │                                               │
│ Filters:     │                                               │
│ (filter stack│                                               │
│  for segment)│                                               │
├──────────────┤                                               │
│ SOURCES ▸    │                                               │
│ (collapsed)  │                                               │
├──────────────┤                                               │
│ EXPORTS ▾    │                                               │
│ No exports   │                                               │
└──────────────┴───────────────────────────────────────────────┘
```

**Key changes from current:**

#### 1. Sequence Panel replaces Source Browser as the top panel

The "SEQUENCE" panel is the primary panel — a vertical ordered list of all segments in the project. Each entry shows:
- Segment number and title
- Type icon (T = title, 🎬 = clip, 📹 = video, 🧵 = stitch, 🎨 = compose)
- Duration
- Between segments: transition indicator showing type and duration

Click a segment to select it → preview updates, inspector updates. This is the equivalent of Cut's "CLIP BANK" — it shows *what you have* in your project, not what you could add.

Below the list: two buttons — `[+ Source]` and `[+ Title Card]`.

**`[+ Source]` behavior:** Clicking this button auto-expands the SOURCES panel if it's currently collapsed (sets `$_localStitchBrowserOpen = true`), scrolls the sidebar to bring the source browser into view, and focuses the search input. This gives new/empty projects a one-click path to the source browser without it permanently stealing sidebar space. Once the user selects a source to add, they can collapse the browser to recover space for the Sequence and Inspector panels. The DataStar expression is straightforward: `data-on:click="$_localStitchBrowserOpen = true"` plus a small JS scroll-into-view call.

**`[+ Title Card]` behavior:** Appends a new title card segment to the end of the sequence with default values (3s duration, white text, black background), selects it, and the Inspector updates to show the title card fields for immediate editing. No panel expansion needed.

#### 2. Inspector shows context for the selected segment

The "INSPECTOR" panel is the second panel — equivalent to Cut's Inspector. When a segment is selected, it shows fields appropriate to the segment type:

**For clips/videos/exports:**
- Source name (read-only)
- Duration (editable, trims end)
- Start/end timestamps (for trimming within the source)
- Transition to next: type dropdown, duration field
- Filter stack (reusing the same `FilterStack` component as Cut)

**For title cards:**
- Text (editable)
- Subtitle (optional)
- Duration
- Background color
- Text color
- Font size
- Position (center/top/bottom)
- Transition to next

This merges the current "SEGMENT DETAIL" panel and "ADD TITLE CARD" panel into one context-sensitive inspector — exactly how Cut merges clip properties into its single Inspector panel.

#### 3. Source Browser is collapsed by default

The "SOURCES" panel contains the same search/filter/sort/results UI as today. But it's a collapsible `SidebarPanel`, **collapsed by default**. You open it when you want to browse and add sources. Once you've added what you need, you collapse it to get more space for the sequence and inspector.

The source browser keeps its full power — type filters (All/Clips/Videos/Stitch/Compose), search, sort (Recent/A-Z/Long), pagination. It just doesn't dominate the sidebar anymore.

#### 4. Timeline gets taller and richer

The timeline grows from `h-20` (80px) to `h-28` (112px). Segment blocks show:
- Type icon (colored: blue=clip, green=video, amber=stitch, purple=compose, rose=title)
- Title text (truncated)
- Duration label
- Width proportional to duration

Transition diamonds between segments are larger and show the transition type on hover. Click a transition diamond → inspector switches to showing the transition editing UI.

#### 5. Responsive sidebar

Replace `w-64` with responsive classes matching Cut: `w-full md:w-1/3 lg:w-1/4 xl:w-1/5`.

---

### Compose — Proposed Layout

```
┌──────────────────────────────────────────────────────────────┐
│ ← COMPOSE  [title input]    ✓ Saved  9:16 1:1 16:9  EXPORT │
├──────────────┬───────────────────────────────────────────────┤
│ LAYERS ▾     │                                               │
│ ┌──────────┐ │                                               │
│ │ L0 100%  │ │       CANVAS PREVIEW                         │
│ │ Host 1   │ │       ┌──────────────┐                        │
│ │ ↑ ↓ 👁 ✕ │ │       │              │    1080 × 1920         │
│ ├──────────┤ │       │   composed   │                        │
│ │ L1 100%  │ │       │   layers     │                        │
│ │ Host 2   │ │       │   preview    │                        │
│ │ ↑ ↓ 👁 ✕ │ │       │              │                        │
│ └──────────┘ │       │              │                        │
│ [+ Add Layer]│       └──────────────┘                        │
├──────────────┤                                               │
│ INSPECTOR ▾  │   ┌──────────────────────────────────────┐    │
│              │   │ ⏮ ◁ ▶ ▷ ⏭  🔊  00:00 / 09:16      │    │
│ Layer: Host 1│   └──────────────────────────────────────┘    │
│              ├───────────────────────────────────────────────┤
│ Source:      │ TIMELINE                         60px/s + Seg │
│ [Main Video] │ ┌────────────────┬──┬────────────────────────┐│
│              │ │ Seg 1          │◇ │ Seg 2                  ││
│ ┌──────────┐ │ │ 0:00–0:15     │  │ 0:15–0:45             ││
│ │ source   │ │ │ 2 layers      │  │ 1 layer               ││
│ │ video    │ │ └────────────────┴──┴────────────────────────┘│
│ │ with     │ │                                               │
│ │ crop     │ │ Segment 1 of 2  │  Undo  Redo               │
│ │ overlay  │ │                                               │
│ └──────────┘ │                                               │
│              │                                               │
│ Crop:        │                                               │
│  x ▸ 0.250  │                                               │
│  y ▸ 0.333  │                                               │
│  w ▸ 0.500  │                                               │
│  h ▸ 0.667  │                                               │
│ [📐 Lock AR]│                                               │
│              │                                               │
│ Position:    │                                               │
│  x: 0  y: 0 │                                               │
│  w: 1080     │                                               │
│  h: 960      │                                               │
│              │                                               │
│ Opacity: 1.0 │                                               │
│              │                                               │
│ Transition → │                                               │
│ Filters:     │                                               │
├──────────────┤                                               │
│ SOURCES ▸    │                                               │
│ (collapsed)  │                                               │
├──────────────┤                                               │
│ EXPORTS ▾    │                                               │
└──────────────┴───────────────────────────────────────────────┘
```

**Key changes from current:**

#### 1. Kill the three-column layout — go to two panels

The right inspector column is eliminated. The canvas preview now gets **all the space** right of the sidebar, producing a much larger composed output preview. On a 1280px screen with a 256px sidebar, the canvas area gets ~1024px instead of the current ~672px.

#### 2. Layer list moves to top of left sidebar

The LAYERS panel is the primary panel — equivalent to Cut's Clip Bank. It shows all layers for the currently selected segment with:
- Layer label
- Scale indicator
- Z-order controls (↑ ↓)
- Visibility toggle (👁)
- Delete (✕)
- [+ Add Layer] button

Click a layer to select it → inspector updates, crop overlay appears on the source preview within the inspector.

#### 3. Inspector is directly below layers — with inline source video

The INSPECTOR panel is the second panel. When a layer is selected, it shows *everything* about that layer in one scrollable area:

1. **Source label** — which video/clip/export this layer pulls from (click to change via source browser)
2. **Inline source video preview** — a small video player *inside the inspector panel* showing the source video with the `CropOverlay` drawn on it. This is the critical change: **the crop overlay and the crop number inputs are in the same panel, right next to each other.** No more cross-screen eye travel.
3. **Crop fields** — x/y/w/h scrub inputs, synced bidirectionally with the overlay
4. **Aspect ratio lock** toggle
5. **Position fields** — x/y/w/h on the canvas
6. **Opacity** slider
7. **Transition to next** — type dropdown + duration (shown only for the layer's parent segment)
8. **Filter stack** — per-layer filters using the same `FilterStack` component as Cut

The source video preview inside the inspector is small (~240px wide at `xl` breakpoint) but functional for drag-based crop editing. The CropOverlay class already handles small containers. For precise work, the scrub inputs are right there.

#### 4. Canvas preview becomes the dominant right-side content

With both sidebars eliminated, the canvas preview gets all the space. The aspect ratio is maintained, but within a much larger bounding box. Letterboxing (black bars) is dramatically reduced.

The canvas shows:
- All layers composited in z-order
- Selected layer highlighted with a border
- Unselected layers with subtle dashed outlines (future: draggable positioning on canvas per the NLE redesign proposal)

#### 5. Source browser is collapsed by default

Same as stitch — the Sources panel is powerful but collapsed. Open it when you need to set a layer's source to something other than the default video.

#### 6. Timeline stays at bottom, gets wider

With no right sidebar, the timeline spans the full width right of the left sidebar. Segment blocks show duration, layer count, and transition indicators.

#### 7. Responsive sidebar

Replace `w-80` and `w-72` fixed widths with responsive classes: `w-full md:w-1/3 lg:w-1/4 xl:w-1/5`.

---

## Panel Inventory

### Shared across all three editors

| Panel                           | Purpose                                                            | Notes                                                   |
| ------------------------------- | ------------------------------------------------------------------ | ------------------------------------------------------- |
| `EditorToolbar`                 | Top bar with back link, title, save status, format/quality, export | Already partially shared via `components.EditorToolbar` |
| `SidebarPanel`                  | Collapsible panel wrapper                                          | Already shared, used by Cut and Stitch                  |
| `ExportButton`                  | Disabled/loading-aware export trigger                              | Already shared                                          |
| `FilterStack`                   | Ordered filter list with add/remove/reorder                        | Already shared between Cut and Compose                  |
| `ExportPanel` / `ExportHistory` | Export status and history cards                                    | Nearly identical across all three — should be unified   |

### Cut panels

| Panel           | Signal                | Default |
| --------------- | --------------------- | ------- |
| CLIP BANK       | `_localClipBankOpen`  | open    |
| INSPECTOR       | `_localInspectorOpen` | open    |
| COLOR / FILTERS | `_localFiltersOpen`   | closed  |
| EXPORT          | `_localExportOpen`    | open    |

### Stitch panels (proposed)

| Panel     | Signal                      | Default    |
| --------- | --------------------------- | ---------- |
| SEQUENCE  | `_localStitchSequenceOpen`  | open       |
| INSPECTOR | `_localStitchInspectorOpen` | open       |
| SOURCES   | `_localStitchBrowserOpen`   | **closed** |
| EXPORTS   | `_localStitchExportsOpen`   | open       |

### Compose panels (proposed)

| Panel     | Signal                     | Default    |
| --------- | -------------------------- | ---------- |
| LAYERS    | `_localComposeLayers`      | open       |
| INSPECTOR | `_localComposeInspector`   | open       |
| SOURCES   | `_localComposeBrowserOpen` | **closed** |
| EXPORTS   | `_localComposeExportsOpen` | open       |

---

## What Changes Per Editor

### Stitch

| Area                  | Current                                    | Proposed                                                           |
| --------------------- | ------------------------------------------ | ------------------------------------------------------------------ |
| Sidebar width         | `w-64` fixed                               | `w-full md:w-1/3 lg:w-1/4 xl:w-1/5`                                |
| Primary panel         | Source Browser (always open, dominates)    | Sequence list (ordered segments)                                   |
| Selected item editing | "SEGMENT DETAIL" panel hidden below scroll | INSPECTOR panel, always 2nd position                               |
| Title card creation   | Separate "ADD TITLE CARD" panel            | `[+ Title]` button in Sequence panel + inline editing in Inspector |
| Transition editing    | Floating popup                             | Inline section in Inspector when segment/transition selected       |
| Source browser        | Always open, top of sidebar                | Collapsible panel, closed by default                               |
| Timeline height       | `h-20` (80px)                              | `h-28` (112px)                                                     |
| Timeline segments     | Bare colored blocks                        | Blocks with type icon + title + duration                           |
| Transition indicators | Small diamonds between blocks              | Larger diamonds, clickable, show type                              |

**Files modified:**
- `cmd/web/templates/stitch.templ` — Full layout restructure, switch to shared `SourceBrowserUI`, `InspectorPanel`, `TransitionEditor`
- `cmd/web/templates/components/stitch_sequence.templ` — New: ordered segment list panel
- `cmd/web/templates/components/stitch_timeline.templ` — Richer segment blocks with type icons
- `static/js/stitch-page.js` — Import from `lib/transitions.js`, `lib/datastar-helpers.js`, `lib/auto-save.js`; update DOM selectors
- `cmd/web/handlers/api/stitch_api/render.go` — Update SSE render targets for new panel IDs
- `cmd/web/handlers/api/stitch_api/browser.go` — Use `common.ParseSourceBrowserParams()`
- `cmd/web/handlers/api/stitch_api/enqueue.go` — Use `common.ValidateFormat/Quality()`

**Files deleted (replaced by shared components):**
- `cmd/web/templates/components/stitch_source_browser.templ` → replaced by `source_browser_results.templ`
- `cmd/web/templates/components/stitch_transition_popup.templ` → replaced by `transition_editor.templ`

**Files NOT modified:**
- Database schema — no changes
- SQL queries — no changes
- Encoder pipeline — no changes

### Compose

| Area               | Current                                          | Proposed                                                        |
| ------------------ | ------------------------------------------------ | --------------------------------------------------------------- |
| Layout             | 3-column (left sidebar + canvas + right sidebar) | 2-column (left sidebar + canvas)                                |
| Right sidebar      | w-72 fixed, Layer Inspector + Exports            | **Eliminated** — moved into left sidebar                        |
| Left sidebar width | `w-80` fixed                                     | `w-full md:w-1/3 lg:w-1/4 xl:w-1/5`                             |
| Primary panel      | Source Video reference                           | Layers list (for selected segment)                              |
| Layer inspector    | Right sidebar (opposite source video)            | Left sidebar INSPECTOR panel (below layers)                     |
| Source video       | Separate panel in left sidebar                   | Inline in Inspector panel (small, with CropOverlay)             |
| Crop editing       | Numeric scrub inputs only, right sidebar         | CropOverlay on inline source video + numeric inputs, same panel |
| Source browser     | Bottom of left sidebar, collapsed                | Collapsible panel, closed by default (same position)            |
| Canvas preview     | Squeezed between two sidebars                    | Full width right of sidebar                                     |
| Export panel       | Right sidebar                                    | Left sidebar EXPORTS panel                                      |

**Files modified:**
- `cmd/web/templates/compose.templ` — Full layout restructure (3-col → 2-col), switch to shared `SourceBrowserUI`, `InspectorPanel`, `TransitionEditor`
- `cmd/web/templates/components/compose_layers.templ` — Updated layer list panel
- `cmd/web/templates/components/compose_timeline.templ` — Full-width timeline (no right sidebar)
- `static/js/compose-page.js` — Import from `lib/transitions.js`, `lib/datastar-helpers.js`, `lib/auto-save.js`; adapt CropOverlay to inline inspector; update canvas sizing
- `cmd/web/handlers/api/compose_api/render.go` — Update SSE render targets
- `cmd/web/handlers/api/compose_api/sources.go` — Use `common.ParseSourceBrowserParams()`
- `cmd/web/handlers/api/compose_api/enqueue.go` — Use `common.ValidateFormat/Quality()`

**Files deleted (replaced by shared components):**
- `cmd/web/templates/components/compose_source_browser.templ` → replaced by `source_browser_results.templ`
- `cmd/web/templates/components/compose_transition_popup.templ` → replaced by `transition_editor.templ`

**Files NOT modified:**
- Database schema — no changes
- SQL queries — no changes
- Encoder pipeline — no changes

---

## CropOverlay Reuse in Compose Inspector

The Cut page's `CropOverlay` class (`static/js/lib/crop-overlay.js`, 375 lines) is already modular with a clean constructor/destroy lifecycle. The compose-stitch-redesign proposal identified 4 changes needed to reuse it in Compose. With the inspector-inline approach, the adaptation is:

1. **Container:** The crop overlay target is the small source `<video>` inside the Inspector panel (~240px wide at xl) instead of a full-width video. CropOverlay already handles arbitrary container sizes.
2. **Coordinate system:** CropOverlay uses center-point + normalized dimensions — this *already matches* compose's layer crop data model exactly (`{x: 0.5, y: 0.5, width: 1.0, height: 1.0}`).
3. **Callbacks:** Replace Cut-specific `this.editor.selectedClipId` with a compose layer callback that updates `$_composeTimeline[segIdx].layers[layerIdx].crop`.
4. **Multi-layer:** Only the selected layer's crop overlay is interactive. Non-selected layers show static outlines on the source video (read-only).

---

## Signal Consolidation

### Stitch — Current: 18+ signals

```
_stitchProjectId, _stitchSegments, _stitchTitle, _stitchFormat, _stitchQuality,
_stitchExporting, _stitchSaving, _stitchDirty,
_stitchBrowserQuery, _stitchBrowserSort, _stitchBrowserSourceFilter, _stitchBrowserOffset, _stitchBrowserLimit,
_localStitchBrowserOpen, _localStitchTitleCardOpen, _localStitchExportsOpen,
_stitchSelectedIdx, _stitchTrIdx
```

### Stitch — Proposed: 16 signals

```
_stitchProjectId, _stitchSegments, _stitchTitle, _stitchFormat, _stitchQuality,
_stitchExporting, _stitchSaving, _stitchDirty,
_stitchBrowserQuery, _stitchBrowserSort, _stitchBrowserSourceFilter, _stitchBrowserOffset, _stitchBrowserLimit,
_stitchSelectedIdx,
_localStitchSequenceOpen, _localStitchInspectorOpen, _localStitchBrowserOpen, _localStitchExportsOpen
```

Removed: `_stitchTrIdx` (transitions edit inline in inspector, no separate popup index), `_localStitchTitleCardOpen` (no separate panel).

### Compose — Current: 20+ signals

```
_composeProjectId, _composeVideoId, _composeTitle, _composeFormat, _composeQuality,
_composeCanvas, _composeTimeline, _composeSelectedSegIdx, _composeSelectedLayerIdx,
_composeDirty, _composeSaving, _composeExporting,
_composeVideoDuration, _composeVideoWidth, _composeVideoHeight,
_localComposeExportsOpen, _composeTrIdx, _localComposeBrowserOpen,
_composeBrowserQuery, _composeBrowserSort, _composeBrowserSourceFilter, _composeBrowserOffset, _composeBrowserLimit,
_localComposeLayers, _localComposeInspector, _filterStack, _composeTimelineZoom
```

### Compose — Proposed: 18 signals

```
_composeProjectId, _composeVideoId, _composeTitle, _composeFormat, _composeQuality,
_composeCanvas, _composeTimeline, _composeSelectedSegIdx, _composeSelectedLayerIdx,
_composeDirty, _composeSaving, _composeExporting,
_composeVideoDuration, _composeVideoWidth, _composeVideoHeight,
_filterStack, _composeTimelineZoom,
_localComposeLayers, _localComposeInspector, _localComposeBrowserOpen, _localComposeExportsOpen
```

Removed: `_composeTrIdx` (transitions edit inline), `_composeBrowserQuery/Sort/Filter/Offset/Limit` (kept but moved to local JS state since they don't need to be DataStar signals — the source browser is self-contained).

---

## Shared Component Architecture

The three editors currently duplicate a huge amount of code. The layout restructure is the right time to extract shared components — because we're rewriting the layout anyway, we should build the new layouts from shared pieces instead of copy-pasting a third time.

### Current Duplication Inventory

| What                                                                              | Where                                                             | Severity                                                                 |
| --------------------------------------------------------------------------------- | ----------------------------------------------------------------- | ------------------------------------------------------------------------ |
| Transition type list (40+ items)                                                  | `stitch-page.js` L12-57, `compose-page.js` L9-34                  | **Identical** copy-paste                                                 |
| `ds()` DataStar helper + signal get/set wrappers                                  | `stitch-page.js`, `compose-page.js`                               | **Identical** pattern                                                    |
| Auto-save function (`window.xxxAutoSave`)                                         | `stitch-page.js`, `compose-page.js`                               | **Identical** pattern, different signal names                            |
| Source browser filter buttons (All/Clips/Videos/Stitch/Compose + Recent/A-Z/Long) | `stitch.templ` L85-107, `compose.templ` L168-199                  | **Identical** HTML, different signal names                               |
| Source browser results rendering + pagination                                     | `stitch_source_browser.templ`, `compose_source_browser.templ`     | ~80% identical                                                           |
| Transition popup UI                                                               | `stitch_transition_popup.templ`, `compose_transition_popup.templ` | ~70% similar (Compose uses `ScrubField`, Stitch uses inline range input) |
| Source browser query param parsing in Go                                          | `stitch_api/browser.go` L20-50, `compose_api/sources.go` L18-48   | **Identical** 40 lines                                                   |
| Format/quality validation in Go                                                   | `stitch_api/enqueue.go`, `compose_api/enqueue.go`                 | **Identical** 20 lines                                                   |
| SSE render handler boilerplate in Go                                              | `stitch_api/render.go`, `compose_api/render.go`                   | **Identical** pattern                                                    |
| Export panel + export history cards                                               | `stitch_export.templ`, `compose_export.templ`                     | Already mostly shared ✓                                                  |
| `EditorToolbar`, `SidebarPanel`, `ExportButton`                                   | `shared_editor.templ`, `sidebar_panel.templ`                      | Already shared ✓                                                         |

### Extraction Plan

#### JavaScript: `static/js/lib/`

**1. `lib/transitions.js` — Shared transition type registry**

The 40+ item transition list (fade, dissolve, wipeLeft, wipeRight, circleOpen, etc.) is copy-pasted identically between `stitch-page.js` and `compose-page.js`. Extract to a shared module:

```js
// static/js/lib/transitions.js
export const TRANSITIONS = [
  { value: 'fade', label: 'Fade', hasDirection: false },
  { value: 'dissolve', label: 'Dissolve', hasDirection: false },
  { value: 'wipeLeft', label: 'Wipe Left', hasDirection: true },
  // ... 40+ entries
];

export function getTransitionLabel(value) {
  return TRANSITIONS.find(t => t.value === value)?.label ?? value;
}
```

Both `stitch-page.js` and `compose-page.js` import from here instead of maintaining their own copies.

**2. `lib/datastar-helpers.js` — Signal accessor factory**

Both editors duplicate the same `ds()` helper and signal getter/setter pattern:

```js
// static/js/lib/datastar-helpers.js
export function ds() {
  return document.querySelector('[data-signals]')?.__datastar;
}

export function createSignalAccessors(prefix) {
  return {
    get: (name) => ds()?.signal(`${prefix}${name}`)?.value,
    set: (name, value) => {
      const s = ds()?.signal(`${prefix}${name}`);
      if (s) s.value = value;
    },
  };
}
```

Stitch uses `createSignalAccessors('_stitch')`, Compose uses `createSignalAccessors('_compose')`. Eliminates ~20 lines of boilerplate per editor.

**3. `lib/auto-save.js` — Shared auto-save manager**

Both editors define `window.xxxAutoSave` with identical logic: check dirty flag → throttle → POST to `/api/xxx/projects/{id}`. Extract:

```js
// static/js/lib/auto-save.js
export function createAutoSave(endpoint, debounceMs = 500) {
  let timeout = null;
  return function autoSave(dirty, projectId, ...data) {
    if (!dirty || !projectId) return;
    clearTimeout(timeout);
    timeout = setTimeout(() => {
      fetch(`${endpoint}/${projectId}`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(data),
      });
    }, debounceMs);
  };
}
```

Stitch: `window.stitchAutoSave = createAutoSave('/api/stitch/projects')`
Compose: `window.composeAutoSave = createAutoSave('/api/compose/projects')`

#### Templ Components: `cmd/web/templates/components/`

**4. `source_browser.templ` — Unified source browser component**

The source browser filter buttons (~50 lines of HTML) are copy-pasted between `stitch.templ` and `compose.templ` with only signal names changed. Extract:

```templ
templ SourceBrowserUI(cfg SourceBrowserConfig) {
  // Search input
  <input data-bind={ cfg.QuerySignal } placeholder="Search sources..." />
  // Type filter buttons: All, Clips, Videos, Stitch, Compose
  for _, f := range sourceFilters {
    <button
      data-class={ fmt.Sprintf("{'border-white/60 bg-white/10': $%s === '%s'}", cfg.FilterSignal, f.Value) }
      data-on:click={ fmt.Sprintf("$%s = '%s'; $%s = 0; @get('%s')", cfg.FilterSignal, f.Value, cfg.OffsetSignal, cfg.Endpoint) }
    >{ f.Label }</button>
  }
  // Sort buttons: Recent, A-Z, Long
  // Results container with pagination
  <div id={ cfg.ResultsID }>
    @cfg.ResultsComponent
  </div>
}
```

This replaces the inline HTML in both `stitch.templ` and `compose.templ`. The `SourceBrowserConfig` struct carries the signal names, endpoint URL, and results container ID. Each editor passes its own config:

```go
type SourceBrowserConfig struct {
  QuerySignal  string // "_stitchBrowserQuery" or "_composeBrowserQuery"
  FilterSignal string
  SortSignal   string
  OffsetSignal string
  Endpoint     string // "/api/stitch/sources" or "/api/compose/sources"
  ResultsID    string // "stitch-source-results" or "compose-source-results"
}
```

**5. `source_browser_results.templ` — Shared results rendering**

The results rendering (`StitchSourceBrowserResults` / `ComposeSourceBrowserResults`) is ~80% identical: pagination controls, result cards with title/duration/type badge, "Add" buttons. Extract:

```templ
templ SourceBrowserResults(results []SourceResult, pagination PaginationState, onAdd string) {
  // Pagination: Previous / Next / "Showing X-Y of Z"
  // Result cards: thumbnail, title, duration, type badge
  // Add button per result
}
```

The difference is the "Add" action — stitch adds a segment to the sequence, compose sets a layer's source. This is parameterized via the `onAdd` callback string (a DataStar action expression).

**6. `transition_editor.templ` — Unified transition editing**

The transition popup templates are ~70% similar. Compose's version is better (uses `ScrubField` for duration). Unify into one component:

```templ
templ TransitionEditor(selectedType string, duration float64, onChangeAction string) {
  // Transition type dropdown (populated from shared list)
  // Duration input using ScrubField
  // Preview hint
}
```

Both stitch and compose render this in their inspector panel with their own `onChangeAction` that writes to the correct signal.

**7. `editor_inspector.templ` — Shared inspector panel skeleton**

All three editors have an "Inspector" panel. The content differs, but the shell is the same:

```templ
templ InspectorPanel(title string, openSignal string, isEmpty bool, emptyMessage string) {
  @SidebarPanel(title, openSignal) {
    if isEmpty {
      <div class="text-xs text-white/40 p-3">{ emptyMessage }</div>
    } else {
      { children... }
    }
  }
}
```

Cut: `@InspectorPanel("Inspector", "_localInspectorOpen", noClipSelected, "Select a clip")`
Stitch: `@InspectorPanel("Inspector", "_localStitchInspectorOpen", noSegSelected, "Select a segment")`
Compose: `@InspectorPanel("Inspector", "_localComposeInspector", noLayerSelected, "Select a layer")`

#### Go Handlers: `cmd/web/handlers/api/common/`

**8. `common/source_browser.go` — Shared query param parsing**

Extract the identical 40 lines of query parameter parsing:

```go
// cmd/web/handlers/api/common/source_browser.go
type SourceBrowserParams struct {
  Query        string
  Sort         string
  SourceFilter string
  Offset       int32
  Limit        int32
}

func ParseSourceBrowserParams(c echo.Context) SourceBrowserParams {
  p := SourceBrowserParams{
    Sort:         "recent",
    SourceFilter: "all",
    Limit:        20,
  }
  p.Query = c.QueryParam("q")
  if v := c.QueryParam("sort"); v != "" { p.Sort = v }
  if v := c.QueryParam("source"); v != "" { p.SourceFilter = v }
  if v := c.QueryParam("offset"); v != "" {
    if n, err := strconv.Atoi(v); err == nil && n >= 0 { p.Offset = int32(n) }
  }
  return p
}
```

Both `stitch_api/browser.go` and `compose_api/sources.go` call `common.ParseSourceBrowserParams(c)` instead of duplicating the parsing.

**9. `common/export_validation.go` — Shared format/quality validation**

Extract the identical validation:

```go
// cmd/web/handlers/api/common/export_validation.go
func ValidateFormat(format string) (string, error) {
  format = strings.TrimSpace(format)
  if format == "" { format = "mp4" }
  if format != "mp4" && format != "webm" {
    return "", fmt.Errorf("invalid format: must be mp4 or webm")
  }
  return format, nil
}

func ValidateQuality(quality string) (string, error) {
  quality = strings.TrimSpace(quality)
  if quality == "" { quality = "high" }
  if quality != "high" && quality != "max" {
    return "", fmt.Errorf("invalid quality: must be high or max")
  }
  return quality, nil
}
```

Both `stitch_api/enqueue.go` and `compose_api/enqueue.go` call these instead of inlining the same checks. Cut's export handler can use them too.

### Component Dependency Graph

The extraction order matters — some shared components depend on others:

```
lib/transitions.js ──────────────────────────────────────── (no deps)
lib/datastar-helpers.js ─────────────────────────────────── (no deps)
lib/auto-save.js ────────────────────────────────────────── (no deps)
common/source_browser.go ────────────────────────────────── (no deps)
common/export_validation.go ─────────────────────────────── (no deps)
TransitionEditor templ ──────────────────── depends on ScrubField
SourceBrowserUI templ ───────────────────── depends on SidebarPanel
SourceBrowserResults templ ──────────────── depends on SourceBrowserUI
InspectorPanel templ ────────────────────── depends on SidebarPanel
```

All the leaf-level extractions (JS libs, Go common, TransitionEditor, InspectorPanel) can be done in parallel. SourceBrowserResults depends on SourceBrowserUI being defined first.

### What's Already Shared (Keep As-Is)

| Component        | File                  | Used By                                             |
| ---------------- | --------------------- | --------------------------------------------------- |
| `EditorToolbar`  | `shared_editor.templ` | All three                                           |
| `ToolbarDivider` | `shared_editor.templ` | All three                                           |
| `ExportButton`   | `shared_editor.templ` | All three                                           |
| `SaveIndicator`  | `shared_editor.templ` | All three                                           |
| `SidebarPanel`   | `sidebar_panel.templ` | All three                                           |
| `FilterStack`    | `filter_stack.templ`  | Cut + Compose (Stitch gains it in this proposal)    |
| `ScrubField`     | `scrub_field.templ`   | Compose (Cut + Stitch gain it via TransitionEditor) |
| `CropOverlay`    | `lib/crop-overlay.js` | Cut (Compose gains it in this proposal)             |
| `TransportMixin` | `lib/transport.js`    | Cut                                                 |
| `ExportStatus`   | `export_panel.templ`  | All three                                           |

### What Gets Extracted (New Shared Components)

| Component                  | File                           | Replaces                                                           |
| -------------------------- | ------------------------------ | ------------------------------------------------------------------ |
| `TRANSITIONS`              | `lib/transitions.js`           | Duplicated arrays in stitch-page.js + compose-page.js              |
| `createSignalAccessors`    | `lib/datastar-helpers.js`      | Duplicated ds()/get/set in stitch-page.js + compose-page.js        |
| `createAutoSave`           | `lib/auto-save.js`             | Duplicated window.xxxAutoSave in stitch-page.js + compose-page.js  |
| `SourceBrowserUI`          | `source_browser.templ`         | Inline HTML in stitch.templ + compose.templ                        |
| `SourceBrowserResults`     | `source_browser_results.templ` | `stitch_source_browser.templ` + `compose_source_browser.templ`     |
| `TransitionEditor`         | `transition_editor.templ`      | `stitch_transition_popup.templ` + `compose_transition_popup.templ` |
| `InspectorPanel`           | `editor_inspector.templ`       | Inline inspector markup in all three editors                       |
| `ParseSourceBrowserParams` | `common/source_browser.go`     | Duplicated parsing in stitch_api + compose_api                     |
| `ValidateFormat/Quality`   | `common/export_validation.go`  | Duplicated validation in stitch_api + compose_api                  |

---

## What This Proposal Does NOT Cover

These are handled by other existing proposals and remain valid as follow-up work:

- **Draggable canvas layers** (compose-nle-redesign.md) — future, builds on this layout
- **Multi-track timeline** (compose-nle-redesign.md) — future, timeline slot is ready
- **Keyboard shortcuts for Stitch/Compose** (editor-unification.md, compose-nle-redesign.md) — future
- **Per-layer filters in Compose** (compose-nle-redesign.md) — future, FilterStack is already in the inspector
- **Multi-source compose layers** UI (universal-source-types.md) — future, source picker is in the inspector
- **Responsive breakpoints below lg** (compose-stitch-redesign.md) — future, stacked mobile layout
- **Transition data not transmitted** (compose-stitch-redesign.md, critical bug) — fix independently
- **Title card preview in stitch** (compose-stitch-redesign.md, critical bug) — fix independently
- **Audio toggle for compose source video** — small fix, do independently

---

## Implementation Order

### Phase 0: Shared component extraction (do first)

This phase happens *before* any layout changes. Extract shared code while the current layouts still work, so each extraction can be tested in isolation.

**JavaScript:**
1. Create `static/js/lib/transitions.js` — move the transition list out of both page scripts
2. Create `static/js/lib/datastar-helpers.js` — extract `ds()`, `createSignalAccessors()`
3. Create `static/js/lib/auto-save.js` — extract `createAutoSave()` factory
4. Update `stitch-page.js` and `compose-page.js` to import from the new shared modules
5. Run `make assets`, verify both editors still work identically

**Go handlers:**
6. Create `cmd/web/handlers/api/common/source_browser.go` — extract `ParseSourceBrowserParams()`
7. Create `cmd/web/handlers/api/common/export_validation.go` — extract `ValidateFormat()`, `ValidateQuality()`
8. Update `stitch_api/browser.go`, `compose_api/sources.go` to use `common.ParseSourceBrowserParams()`
9. Update `stitch_api/enqueue.go`, `compose_api/enqueue.go` to use `common.ValidateFormat/Quality()`
10. `make generate`, rebuild, verify exports and source browsing still work

**Templ components:**
11. Create `source_browser.templ` — extract `SourceBrowserUI()` with configurable signals/endpoints
12. Create `source_browser_results.templ` — extract shared results + pagination rendering
13. Create `transition_editor.templ` — unify transition editing with `ScrubField`
14. Create `editor_inspector.templ` — extract `InspectorPanel()` shell
15. Update `stitch.templ` and `compose.templ` to use the new shared components
16. `make generate`, rebuild, verify everything renders correctly

At the end of Phase 0, the codebase has less duplication and the same UX. No user-visible changes.

### Phase 1: Stitch layout restructure

1. Create `StitchSequencePanel` component — ordered segment list with type icons, durations, transition indicators
2. Restructure `stitch.templ` — Sequence panel top, `InspectorPanel` second, `SourceBrowserUI` collapsed, Exports bottom
3. Merge title card fields into Inspector (context-sensitive based on segment type)
4. Move transition editing inline using shared `TransitionEditor` component
5. Update `stitch-page.js` to render sequence list entries and update them reactively
6. Update SSE render endpoints for new panel structure / DOM IDs
7. Grow timeline to `h-28`, add type icons and title text to segment blocks
8. Switch sidebar to responsive width classes

### Phase 2: Compose layout restructure

1. Remove right sidebar column entirely from `compose.templ`
2. Move layer inspector into left sidebar using shared `InspectorPanel`
3. Add inline source video with CropOverlay inside the Inspector panel
4. Wire CropOverlay bidirectionally with crop scrub inputs
5. Move transition editing to use shared `TransitionEditor` component
6. Update canvas sizing to fill the newly available space
7. Move export panel/history into left sidebar
8. Update `compose-page.js` for new DOM structure and inline CropOverlay
9. Update SSE render endpoints
10. Switch sidebar to responsive width classes

### Phase 3: Visual polish + Cut backport

1. Unify segment color coding across stitch timeline and source browser (blue=clip, green=video, amber=stitch, purple=compose, rose=title)
2. Standardize timeline labels and duration formats
3. Ensure transition diamonds are consistent between stitch and compose timelines
4. Backport shared components into Cut where applicable (Cut's inspector → `InspectorPanel`, Cut's export → shared `SourceBrowserUI` if Cut ever gets source browsing)
5. Test at lg/xl/2xl breakpoints, fix any overflow/squeeze issues

---

## Feasibility Review

### Verdict: Feasible with caveats

The proposal is well-grounded in actual codebase structure. The problem diagnosis is accurate: sidebar widths (`w-64`, `w-80`, `w-72`), panel ordering, the three-column compose layout, the stitch detail panel being buried below scroll — all confirmed. The shared component inventory is correct. The phased approach (extract first, restructure second) is the right strategy.

Below are the issues that need resolution before or during implementation.

### Risks and Concerns

#### 1. Inline Source Video in Compose Inspector — High Risk

The biggest UX gamble. The current compose layout dedicates the full `w-80` (320px) left sidebar to the source video + crop overlay. The proposal moves this into the Inspector panel inside a responsive sidebar (`w-full md:w-1/3 lg:w-1/4 xl:w-1/5`). At `xl` on a 1920px screen that's ~384px; at `lg` on 1440px it's ~360px. Within that panel, the video preview shares space with Crop/Position/Opacity fields, so the actual video might be ~240-280px wide.

`CropOverlay` handles arbitrary container sizes, **but drag-to-crop at 240px on a multi-layer composition is marginal UX.** This is a precision editing tool — users are dragging handles to select fractions of a video frame. At 240px, a 10% crop adjustment is ~24 pixels of mouse movement.

**Mitigations (pick one):**
- Allow the sidebar to be user-resizable (CSS `resize: horizontal` or a drag handle). Wider sidebar = larger source preview when needed.
- Keep a "pop-out" option that opens the source video in a larger floating panel for precise crop editing.
- Accept that the scrub number inputs are the primary editing method at this size, and the visual overlay is for confirmation rather than precise control.

#### 2. Stitch Source Browser Collapsed by Default — Resolved

For stitch, adding sources IS the primary workflow. A brand new stitch project starts empty — the first thing every user does is browse and add sources. Making the source browser collapsed by default optimizes for the editing phase at the cost of the building phase.

**Resolution:** The `[+ Source]` button in the Sequence panel auto-expands the Sources panel when clicked (updated in the Stitch Proposed Layout section above). Flow: new project → click `[+ Source]` → browser opens and search focuses → add sources → collapse when done. This gives immediate access without permanent sidebar dominance.

#### 3. Transition Editing Selection Model — Incomplete

The proposal removes `_stitchTrIdx` and `_composeTrIdx` and says transitions edit "inline in the inspector." But it doesn't define the selection model:
- Does clicking a transition diamond in the timeline select the *preceding segment* and scroll the inspector to the transition section?
- Or is the transition section always visible when a segment is selected?
- If always visible, what happens for the last segment (which has no outgoing transition)?

**Recommendation:** Show the outgoing transition section in the inspector when *any* segment except the last is selected. It should be part of the inspector content, not a separate mode. Timeline transition diamonds, when clicked, select the preceding segment and auto-scroll the inspector to the transition section. This eliminates the need for `_stitchTrIdx`/`_composeTrIdx` as the proposal intends.

#### 4. FilterStack for Stitch — Encoder Dependency

The proposal adds `FilterStack` to the stitch inspector for per-segment filters. The `StitchSegment` struct already has a `Filters []interface{}` field, so the data model is ready. However, the encoder pipeline must actually apply these filters during stitch export rendering. If it doesn't today, this is an encoder change that's out of scope for a "layout restructure" proposal.

**Recommendation:** Add FilterStack to the stitch inspector UI now (it's just a panel), but clearly document that filter *application* during encoding is a separate task. The UI can exist before the encoder supports it, with a disabled state or note.

#### 5. Stitch Sequence Panel — New Non-Trivial Component

The `StitchSequencePanel` is a new component (not a restructure of an existing one). It's essentially a vertical list-view timeline: ordered segments with type icons, durations, and transition indicators between entries. It needs:
- Click to select
- Move up/down buttons (or drag-to-reorder)
- Inline transition indicators between items
- Visual distinction for selected item
- Reactive updates when `_stitchSegments` changes

This is straightforward templ + CSS but it's ~150-200 lines of new template code plus SSE render handler updates. It should be prototyped early in Phase 1.

#### 6. Canvas Interactions Are Unaffected — Should Be Stated

The compose canvas has rich mouse interactions: drag to move layers, 8-handle resize, snap guides, hit testing. The proposal's 3-to-2-column conversion doesn't change the canvas interaction code, but the canvas container *grows significantly* (from ~672px to ~1024px on a 1280px screen), which changes the coordinate mapping. `renderComposeCanvas()` already handles dynamic canvas sizing via `getBoundingClientRect()`, so this should Just Work — but it should be tested.

### Gaps in the Proposal

#### 1. E2E Test Impact

The project has e2e tests in `tests/e2e/`. Layout restructuring changes DOM structure, element IDs, and selectors. Any e2e tests that reference stitch/compose DOM elements will break. The proposal should list e2e test updates as a phase step.

#### 2. `stitch_clip_browser.templ` — Missing Deletion

The file inventory lists `stitch_source_browser.templ` and `stitch_transition_popup.templ` for deletion, but `stitch_clip_browser.templ` also exists as a legacy component that was superseded by the unified source browser. It should be listed for deletion too.

#### 3. Export History Unification

The panel inventory table says ExportHistory is "Nearly identical across all three — should be unified." The extraction plan tables list 9 items but `ExportHistory` isn't one of them. `StitchExportHistory` and `ComposeExportHistory` should be extracted into a shared `ExportHistory` component parameterized by job type. This is low-effort, high-value deduplication.

#### 4. Source Browser Params — Missing `limit` Parsing

The `ParseSourceBrowserParams` example code parses `q`, `sort`, `source`, `offset` but not `limit`. Both current browsers support a `limit` query param. The shared function should parse it too.

#### 5. SSE Render Target IDs — Need Explicit Mapping

The proposal says "Update SSE render targets for new panel IDs" for both editors, but doesn't list the old→new ID mapping. Since the SSE handlers use `datastar.WithSelectorID("target-id")` to patch specific DOM elements, any ID change requires coordinated updates across:
- The templ template (element ID)
- The Go handler (SSE patch target)
- The JS code (any `document.getElementById` calls)

A table of old→new IDs for each editor would prevent bugs during migration.

#### 6. DataStar Effect Triggers

Stitch currently has three `data-effect__debounce` spans that trigger SSE renders on signal changes:
- Timeline render: triggered by `_stitchSegments` + `_stitchSelectedIdx`
- Detail render: triggered by `_stitchSegments` + `_stitchSelectedIdx`
- Transition popup render: triggered by `_stitchSegments` + `_stitchTrIdx`

Removing `_stitchTrIdx` means the transition popup render effect is eliminated. The new Inspector panel needs its own render trigger when `_stitchSelectedIdx` changes (which already triggers the detail render — so these may merge). This should be spelled out.

#### 7. Compose `_composeBrowserQuery/Sort/Filter/Offset/Limit` — Signal Removal

The signal consolidation section says these browser signals move to "local JS state since they don't need to be DataStar signals." But the source browser SSE endpoint currently reads these from `datastar.ReadSignals()` in the Go handler. If they become local JS state, the browser endpoint needs to switch to query parameters (which is how the stitch browser already works). The two browser handlers use different request patterns today — this unification should be explicit.

### What's Well-Handled

- **No database/schema changes** — correct, this is purely frontend/handler restructure
- **No encoder changes** — correct (with the FilterStack caveat above)
- **Phase 0 extraction before layout changes** — right strategy, minimizes risk
- **SidebarPanel reuse** — already works in all three editors, no adaptation needed
- **Signal naming preserved** — no signal renames, just removals of unused ones
- **Responsive width classes** — directly portable from Cut's proven implementation
- **JS extraction scope** — correctly targets only the truly duplicated code (transitions, ds helpers, auto-save) without over-extracting editor-specific logic
- **Dependency graph for extraction order** — clearly laid out, no circular deps

---

## Implementation Todo List

### Phase 0: Shared Component Extraction

- [ ] **0.1** Create `static/js/lib/transitions.js` — extract transition type registry from `stitch-page.js` and `compose-page.js`
- [ ] **0.2** Create `static/js/lib/datastar-helpers.js` — extract `ds()` and `createSignalAccessors()` factory
- [ ] **0.3** Create `static/js/lib/auto-save.js` — extract `createAutoSave()` factory
- [ ] **0.4** Update `stitch-page.js` to import from new shared JS modules; remove duplicated code
- [ ] **0.5** Update `compose-page.js` to import from new shared JS modules; remove duplicated code
- [ ] **0.6** Run `make assets`; manually verify both editors still function (source browsing, transitions, auto-save)
- [ ] **0.7** Create `cmd/web/handlers/common/source_browser.go` — `ParseSourceBrowserParams()` (include `limit`)
- [ ] **0.8** Create `cmd/web/handlers/common/export_validation.go` — `ValidateFormat()`, `ValidateQuality()`
- [ ] **0.9** Update `stitch_api/browser.go` to use `common.ParseSourceBrowserParams()`
- [ ] **0.10** Update `compose_api/sources.go` to use `common.ParseSourceBrowserParams()` (convert from signal-reading to query params if needed)
- [ ] **0.11** Update `stitch_api/enqueue.go` and `compose_api/enqueue.go` to use `common.ValidateFormat/Quality()`
- [ ] **0.12** Create `cmd/web/templates/components/source_browser.templ` — `SourceBrowserUI()` with `SourceBrowserConfig` struct
- [ ] **0.13** Create `cmd/web/templates/components/source_browser_results.templ` — shared results + pagination rendering
- [ ] **0.14** Create `cmd/web/templates/components/transition_editor.templ` — unified transition editing with `ScrubField`
- [ ] **0.15** Create `cmd/web/templates/components/editor_inspector.templ` — `InspectorPanel()` shell
- [ ] **0.16** Create shared `ExportHistory` component from `stitch_export_history.templ` / `compose_export_history.templ`
- [ ] **0.17** Update `stitch.templ` and `compose.templ` to use new shared templ components
- [ ] **0.18** Delete `stitch_source_browser.templ`, `stitch_clip_browser.templ`, `stitch_transition_popup.templ`
- [ ] **0.19** Delete `compose_source_browser.templ`, `compose_transition_popup.templ`
- [ ] **0.20** Run `make generate && make assets`; rebuild; verify no regressions in either editor

### Phase 1: Stitch Layout Restructure

- [ ] **1.1** Create `cmd/web/templates/components/stitch_sequence.templ` — sequence panel with ordered segments, type icons, durations, transition indicators, `[+ Source]` / `[+ Title]` buttons
- [ ] **1.2** Define old→new DOM element ID mapping for stitch SSE render targets
- [ ] **1.3** Restructure `stitch.templ` — Sequence panel top, Inspector second, Sources collapsed, Exports bottom; switch sidebar to `w-full md:w-1/3 lg:w-1/4 xl:w-1/5`
- [ ] **1.4** Merge title card creation into Inspector (segment-type-sensitive fields)
- [ ] **1.5** Move transition editing inline into Inspector using shared `TransitionEditor`; remove `_stitchTrIdx` signal and its debounced data-effect
- [ ] **1.6** Wire `[+ Source]` button to auto-expand Sources panel if collapsed
- [ ] **1.7** Update `stitch-page.js` — sequence panel rendering, selection, move-up/down; update DOM selectors per ID mapping
- [ ] **1.8** Update `stitch_api/render.go` — SSE render targets for new panel structure; merge detail + transition renders
- [ ] **1.9** Grow timeline from `h-20` to `h-28`; add type icons and title text to `stitch_timeline.templ` segment blocks
- [ ] **1.10** Update e2e tests for stitch editor DOM changes
- [ ] **1.11** Run `make generate && make assets`; rebuild; full manual regression test of stitch editor

### Phase 2: Compose Layout Restructure

- [ ] **2.1** Define old→new DOM element ID mapping for compose SSE render targets
- [ ] **2.2** Remove right sidebar column from `compose.templ`; convert to 2-column layout
- [ ] **2.3** Move `compose_inspector.templ` content into left sidebar Inspector panel
- [ ] **2.4** Add inline source video + CropOverlay inside Inspector panel; wire bidirectional crop sync
- [ ] **2.5** Move export panel/history into left sidebar
- [ ] **2.6** Move transition editing inline using shared `TransitionEditor`; remove `_composeTrIdx` signal
- [ ] **2.7** Update `compose-page.js` — canvas sizing for full-width, CropOverlay in inspector, DOM selectors per ID mapping
- [ ] **2.8** Verify canvas drag interactions still work at new larger canvas size
- [ ] **2.9** Switch sidebar to `w-full md:w-1/3 lg:w-1/4 xl:w-1/5`
- [ ] **2.10** Update `compose_api/render.go` — SSE render targets for new panel structure
- [ ] **2.11** Unify compose browser signal handling: convert `_composeBrowser*` signals to query params for consistency with stitch
- [ ] **2.12** Update e2e tests for compose editor DOM changes
- [ ] **2.13** Run `make generate && make assets`; rebuild; full manual regression test of compose editor

### Phase 3: Visual Polish + Cut Backport

- [ ] **3.1** Unify segment color coding across stitch timeline, compose timeline, and source browser type badges
- [ ] **3.2** Standardize timeline labels and duration format (mm:ss vs raw seconds) across both editors
- [ ] **3.3** Ensure transition diamonds are consistent (size, color, click behavior) between stitch and compose timelines
- [ ] **3.4** Backport `InspectorPanel` shell into Cut page where applicable
- [ ] **3.5** Test all three editors at `md`, `lg`, `xl`, and `2xl` breakpoints; fix overflow/squeeze issues
- [ ] **3.6** Final regression test of all three editors end-to-end
