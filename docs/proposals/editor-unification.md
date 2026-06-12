# Proposal: Editor Unification

## Summary

Cut, Stitch, and Compose are three views of the same creative process — selecting source material, transforming it, and exporting a result. Yet their implementations share almost no UI infrastructure. Each editor has its own top bar layout, panel system, button styles, timeline implementation, signal naming conventions, auto-save logic, export status UI, and project list page — all built independently with extensive code duplication.

This proposal unifies them into three layouts of one editor shell, sharing a common toolbar, panel system, timeline architecture, and component library. The Cut page is the reference design — it's the most complete, most polished, and has the best patterns. Stitch and Compose should converge toward Cut's conventions, not the other way around.

---

## Problem Statement

### Structural Divergence

| Component         | Cut                                             | Stitch                                     | Compose                                     |
| ----------------- | ----------------------------------------------- | ------------------------------------------ | ------------------------------------------- |
| Top bar           | `LinkButton`/`FormButton` components            | Inline `<a>` + input + toggle markup       | Copy-pasted from Stitch                     |
| Sidebar panels    | `SidebarPanel` component (4 panels)             | `SidebarPanel` (3 panels)                  | No shared component, raw `<div>`s           |
| Button styling    | `CutButton`/`CutIconButton` with variant system | Raw inline Tailwind, repeated per button   | Same as Stitch                              |
| Timeline          | Dual JS canvases (overview + work area)         | SSR proportional blocks                    | SSR bottom strip, simpler than Stitch       |
| Auto-save         | Manual toggle via `_localAutoSave` signal       | Always-on, `data-effect` → JS function     | Copy-pasted from Stitch                     |
| Export controls   | Sidebar panel with format/quality/variant       | Top bar with format/quality toggles        | Copy-pasted from Stitch                     |
| Export status     | `ClipExportStatus` component                    | `StitchExportStatus` (identical structure) | `ComposeExportStatus` (identical structure) |
| Signal prefix     | `_local*`, `_clip*`, `_filter*`                 | `_stitch*`                                 | `_compose*`                                 |
| Transition list   | N/A                                             | 38 transitions in JS constant              | 16 transitions in JS constant (subset)      |
| Library/list page | N/A (videos list is separate)                   | `stitch_library.templ`                     | `compose_library.templ` (identical layout)  |

### What This Costs

1. **Triple maintenance**: Every UI improvement (a better toggle button, a clearer export status) must be implemented three times.
2. **Inconsistent UX**: Users switching between editors must learn three different control layouts for the same operations (format toggle, quality toggle, save, export).
3. **Wasted effort**: Stitch and Compose have near-identical top bars, export status displays, transition popups, and library pages — all copy-pasted and diverging.
4. **Missing features**: Good patterns in Cut (keyboard shortcuts, button variant system, scrub-to-adjust, dual timeline) aren't available in Stitch/Compose because there's no shared infrastructure to carry them.

---

## Design: Unified Editor Shell

### Core Principle

The editor shell is a shared layout wrapper that provides:
1. A **toolbar** (replaces per-editor top bars)
2. A **panel system** (collapsible sidebar panels, consistent everywhere)
3. A **command bar** (bottom status/action bar)
4. A **timeline slot** (pluggable per-editor timeline implementation)

Each editor fills these slots with its specific content. The shell handles chrome; the editor handles domain logic.

### Shell Layout

```
┌─ Toolbar ─────────────────────────────────────────────────────────┐
│ ← [context]  [TITLE]  [status]  │  [editor-specific tools]  │ [export] │
├───────────────────────────────────────────────────────────────────┤
│ LEFT PANELS          │ MAIN AREA (flex, editor-owned)              │
│ ┌─ Panel 1 ────────┐ │                                            │
│ │ (SidebarPanel)    │ │  Video preview / canvas / dual monitor     │
│ └──────────────────┘ │  (provided by each editor)                 │
│ ┌─ Panel 2 ────────┐ │                                            │
│ │ (SidebarPanel)    │ │                                            │
│ └──────────────────┘ │                                            │
│ ...                  │                                            │
├──────────────────────┴────────────────────────────────────────────┤
│ TIMELINE AREA (editor-specific content inside shared container)    │
├───────────────────────────────────────────────────────────────────┤
│ Command Bar: [zoom ← →] [transport] [status text] [save/export]  │
└───────────────────────────────────────────────────────────────────┘
```

### Toolbar Unification

The toolbar replaces `CutTopBar`, the stitch inline top bar, and the compose inline top bar. It has three zones:

**Left zone — Navigation context:**
- Back-link: `← BACK TO WATCH` (Cut), `← STITCH` (Stitch), `← COMPOSE` (Compose)
- Breadcrumb-style context showing what you're editing

**Center zone — Project identity:**
- Project title: read-only for Cut (video title), editable `data-bind` for Stitch/Compose
- Save indicator: unified component — green ✓ Saved / yellow spinner / red error
- Auto-save always on (remove Cut's manual toggle, align with Stitch/Compose pattern)

**Right zone — Editor-specific tools + export:**
- Cut: "ALL VIDEOS", "OPEN IN PRODUCER", "COMPOSE" action buttons
- Stitch: Format toggle (MP4/WebM), Quality toggle (High/Max)
- Compose: Canvas presets (9:16/1:1/16:9), Format toggle, Quality toggle
- Export button (unified style) — always far right

**Implementation:**

```templ
templ EditorToolbar(config EditorToolbarConfig) {
    <div class="flex items-center justify-between h-10 px-4 border-b-2 border-white/10 bg-black shrink-0">
        <div class="flex items-center gap-3">
            // Left zone: back link + context
            @components.LinkButton(config.BackLink, config.BackLabel, "ghost", "sm")
            // Additional context links
            for _, link := range config.ContextLinks {
                @components.LinkButton(link.Href, link.Label, "ghost", "sm")
            }
        </div>
        <div class="flex items-center gap-3">
            // Center zone: title + save
            if config.TitleEditable {
                <input type="text" data-bind={ config.TitleSignal }
                    class="bg-transparent border-0 border-b border-white/20 text-white font-mono text-sm
                           focus:border-white focus:outline-none text-center w-48"/>
            } else {
                <span class="font-mono text-sm text-white/80 truncate max-w-xs">{ config.Title }</span>
            }
            <div id="editor-save-indicator">
                @components.SaveIndicator(config.SaveState)
            </div>
        </div>
        <div class="flex items-center gap-2">
            // Right zone: editor-specific tools (passed as children or slot)
            { children... }
        </div>
    </div>
}
```

### Button Component Unification

Cut already has the `CutButton` and `CutIconButton` components with a variant system (`default`, `primary`, `danger`). These should be promoted to shared components:

| Current                                    | Unified                                      |
| ------------------------------------------ | -------------------------------------------- |
| `CutButton(label, variant, size)`          | `components.Button(label, variant, size)`    |
| `CutIconButton(icon, variant, size)`       | `components.IconButton(icon, variant, size)` |
| Inline Stitch/Compose buttons              | Replaced with `components.Button(...)` calls |
| `LinkButton(href, label, variant, size)`   | Already shared — keep as-is                  |
| `FormButton(action, label, variant, size)` | Already shared — keep as-is                  |

**Toggle button pattern** — All three editors use `data-class="{'border-white/60 bg-white/10': $signal === 'value'}"` inline. Extract to a component:

```templ
templ ToggleButton(signal string, value string, label string) {
    <button class={ viewtypes.GhostButtonSm }
        data-on:click={ "$" + signal + " = '" + value + "'" }
        data-class={ "{'border-white/60 bg-white/10': $" + signal + " === '" + value + "'}" }>
        { label }
    </button>
}
```

### SidebarPanel Everywhere

Compose currently doesn't use `SidebarPanel`. All its panels (source video, layer list, layer inspector, export) should be wrapped in `SidebarPanel` components with the same collapse/expand behavior as Cut and Stitch.

This gives Compose users the same ability to hide panels they don't need, reclaiming screen space for the canvas preview.

### Export Status Unification

`ClipExportStatus`, `StitchExportStatus`, and `ComposeExportStatus` are structurally identical — same icons, same colors, same states (queued/processing/complete/error). Replace all three with one:

```templ
templ ExportStatus(status string, progress int, downloadURL string) {
    // Single shared component with queued/processing/complete/error states
}
```

### Export History Unification

Same pattern — the export history list in all three editors uses the same card layout with download buttons. One shared `ExportHistoryItem` component.

### Transition Popup Unification

Stitch has 38 transitions, Compose has 16 (a subset). Both are JS constants duplicated between files. The transition data should be:

1. **Server-provided** — a Go constant or database-backed list, served via SSE or embedded in the page
2. **One shared transition popup component** — parameterized by which transitions to show
3. **One transition preview renderer** — the canvas-based preview logic is duplicated; share it

### Project Library Unification

`stitch_library.templ` and `compose_library.templ` are structurally identical: heading + "+ NEW PROJECT" button + grid of project cards with metadata badges. Extract to a shared library component:

```templ
templ ProjectLibrary(config ProjectLibraryConfig, projects []ProjectCard) {
    // Shared header + grid + card layout
    // config.NewProjectURL, config.EditorType, config.Icon
}
```

---

## Phase Plan

### Phase 1: Shared Components (Foundation)

Extract and unify shared UI components without changing any editor's layout or behavior.

**Components to extract:**
- `components.Button(label, variant, size)` — promote `CutButton` to shared
- `components.IconButton(icon, label, variant, size)` — promote `CutIconButton` to shared
- `components.ToggleButton(signal, value, label)` — new, replaces inline toggle patterns
- `components.SaveIndicator(state)` — extract from Stitch/Compose inline markup
- `components.ExportStatus(status, progress, downloadURL)` — merge three identical components
- `components.ExportHistoryItem(export)` — merge three identical list patterns
- `components.TransitionPopup(transitions, selectedSignal, durationSignal, targetID)` — merge two popups

**Signal naming convention:**
- Adopt `_ed` prefix for shared editor signals: `_edTitle`, `_edFormat`, `_edQuality`, `_edDirty`, `_edSaving`, `_edExporting`
- Editor-specific signals keep their prefixes: `_clip*`, `_stitch*`, `_compose*`

**Estimated scope:** ~15 files touched, mostly extracting existing code into shared components and then replacing inline markup with component calls. No behavioral changes.

### Phase 2: Toolbar + Command Bar

Replace per-editor top bars with the unified `EditorToolbar`.

**Cut:**
- Toolbar left: "← BACK TO WATCH"
- Toolbar center: Video title (read-only), save indicator
- Toolbar right: "ALL VIDEOS", "OPEN IN PRODUCER", "COMPOSE" buttons
- Remove auto-save toggle (always on, matching Stitch/Compose)
- Bottom command bar: transport controls, zoom, SET IN/OUT, SAVE, CREATE

**Stitch:**
- Toolbar left: "← STITCH"
- Toolbar center: Project title (editable), save indicator
- Toolbar right: Format toggle, Quality toggle, EXPORT button
- Bottom command bar: segment count, PLAY ALL, DELETE PROJECT

**Compose:**
- Toolbar left: "← COMPOSE"
- Toolbar center: Project title (editable), save indicator
- Toolbar right: Canvas presets, Format toggle, Quality toggle, EXPORT button
- Bottom command bar: segment/layer info, transport controls

**Estimated scope:** ~8 files. `EditorToolbar` templ component + refactoring each editor's top bar markup to use it.

### Phase 3: Panel System

Adopt `SidebarPanel` in Compose (currently absent) and standardize panel behavior.

**Compose changes:**
- Wrap "Layer List" in `SidebarPanel`
- Wrap "Layer Inspector" in `SidebarPanel`
- Wrap "Export" section in `SidebarPanel`
- Add consistent collapse/expand behavior matching Cut and Stitch

**All editors:**
- Panel collapse state saved to `localStorage` per-editor, per-panel
- Panels can be reordered via drag (future, not in this proposal)
- Keyboard shortcut to toggle all panels (like Cut's existing keyboard system)

**Estimated scope:** ~4 files. Compose template changes + SidebarPanel parameter additions.

### Phase 4: Timeline Convergence

This phase does NOT unify timeline implementations (they solve fundamentally different problems). Instead, it provides a shared timeline container and consistent visual language.

**Shared timeline container:**
- Consistent border, background, height allocation
- Shared label style ("OVERVIEW", "WORK AREA", "TIMELINE")
- Shared bottom-of-viewport positioning for all editors

**Visual consistency:**
- Segment blocks use the same color-coding system:
  - Blue = clip source
  - Green = video source
  - Amber = stitch source
  - Purple = compose source
  - Rose = title card
- Transition indicators use the same visual style (small diamond-shaped connector between segments)
- Time/duration labels use the same format (`0:00.000` for sub-minute, `0:00:00` for longer)

**Cut-specific:**
- Keep dual canvas timeline (overview + work area) — this is battle-tested and correct for frame-accurate clip work
- Marker blocks on overview timeline use the same color-coding as above

**Stitch/Compose:**
- Keep SSR proportional-block timelines — these are correct for sequence editing
- Apply consistent visual language (colors, transitions, segment blocks)
- Add duration labels inside segment blocks (currently only in Compose)

**Estimated scope:** ~6 files. CSS standardization + template adjustments.

### Phase 5: Keyboard Shortcuts

Cut has a full keyboard shortcut system via the `Controls` class. Extend it to Stitch and Compose.

**Shared shortcuts (all editors):**
| Key            | Action                         |
| -------------- | ------------------------------ |
| `Space`        | Play/pause                     |
| `S`            | Save                           |
| `Ctrl+Z`       | Undo (when implemented)        |
| `Ctrl+Shift+Z` | Redo (when implemented)        |
| `?`            | Show keyboard shortcut overlay |

**Cut-specific shortcuts:**
| Key          | Action                                |
| ------------ | ------------------------------------- |
| `I`          | Set in point                          |
| `O`          | Set out point                         |
| `J/K/L`      | Reverse/pause/forward (JKL transport) |
| `Left/Right` | Step frame backward/forward           |
| `C`          | Create clip from in/out               |

**Stitch-specific shortcuts:**
| Key       | Action                                 |
| --------- | -------------------------------------- |
| `Delete`  | Remove selected segment                |
| `D`       | Duplicate selected segment             |
| `Up/Down` | Move segment up/down in order          |
| `N`       | Add new segment (opens source browser) |

**Compose-specific shortcuts:**
| Key       | Action                        |
| --------- | ----------------------------- |
| `Delete`  | Remove selected layer/segment |
| `Up/Down` | Move layer z-order            |
| `Tab`     | Cycle through layers          |
| `N`       | Add new segment               |

**Estimated scope:** Import `Controls` class into stitch-page.js and compose-page.js, register shortcuts.

---

## Ableton-Style Overlay Rendering

The user referenced Ableton's automation overlay as inspiration. In Ableton, automation lanes are semi-transparent overlays drawn on top of existing clip regions in the arrangement view, showing parameter curves (volume, pan, filter cutoff) directly in context with the audio they affect.

### How This Applies to Rewind

**Cut page — Filter automation overlays:**

When a filter is applied to a clip, the filter's parameter values could be visualized as an overlay on the clip's region in the WORK AREA timeline:

```
WORK AREA
┌──────────────────────────────────────────────┐
│ [clip region with waveform]                  │
│ ├── brightness: ═══▀▀▀═══ (value curve)     │  ← overlay on clip
│ ├── contrast:   ═════════ (flat line)        │
│ ├── volume:     ▄▄▀▀▀▄▄▄ (envelope)         │
│ └────────────────────────────────────────────│
└──────────────────────────────────────────────┘
```

This is aspirational and depends on filter automation (keyframing) which doesn't exist yet. When keyframing is added, the overlay rendering pattern is the right way to visualize it — showing the automation curve directly on the clip region, not in a separate panel.

**Stitch page — Transition/segment overlays:**

Show transition type and duration as graphical overlays between segments in the timeline:

```
┌────────────┐╲╱┌────────────┐──┌────────────┐
│  Segment 1 │XX│  Segment 2 │  │  Segment 3 │
│  0:00-0:15 │XX│  0:15-0:45 │  │  0:45-1:00 │
└────────────┘╱╲└────────────┘──└────────────┘
              ↑ xfade 0.5s      ↑ hard cut
```

The "XX" region shows the overlap zone where both segments contribute to the output, with a visual indicator of the transition curve shape. This replaces the current approach of showing transition info only in the detail panel.

**Compose page — Layer position overlays:**

Show layer crop regions as colored outlines directly on the canvas preview — already partially implemented via the crop overlay. Extend to show all layers simultaneously with muted outlines for non-selected layers:

```
Canvas Preview
┌──────────────────────────────┐
│  ┌─── Layer 1 (selected) ──┐│
│  │  ╔══ Layer 2 ═══╗       ││
│  │  ║              ║       ││
│  │  ╚══════════════╝       ││
│  └──────────────────────────┘│
└──────────────────────────────┘
Selected layer: solid white border, drag handles
Other layers: dashed white/40 border, non-interactive
```

### Implementation Note

The automation overlay concept is a visual rendering technique, not a data model change. It layers on top of whatever timeline renderer each editor uses. The key constraint is that the overlay must be:
1. Semi-transparent (alpha ~0.3) so the underlying content (waveform, clip thumbnail) remains visible
2. Color-coded to the parameter being automated
3. Drawn in the same coordinate space as the clip region (not a separate lane)

This aligns with the graphical filter controls proposal, which will add the per-filter graphical widgets. Once those exist, the overlay rendering becomes a natural extension — the same curve visualization drawn in the filter panel can be drawn at a smaller scale as an overlay on the timeline.

---

## Relationship to Other Proposals

### Graphical Filter Controls
The filter controls proposal adds bespoke widgets per filter type. Editor unification extracts those widgets into shared components usable across all editors. When Stitch/Compose add filter support (currently Cut-only), the same widget library works.

### Compose & Stitch Redesign
The existing redesign proposal focuses on layout improvements specific to each editor (Source/Program dual-monitor for Compose, CropOverlay reuse, responsive breakpoints). Editor unification handles the shared infrastructure; the redesign handles per-editor layout. They are complementary:
- Unification provides: shared toolbar, button components, panel system, export status
- Redesign provides: Source/Program layout, CropOverlay integration, audio toggle, responsive breakpoints

### Producer v2
Producer already shares some patterns (WebSocket hub, session management). As the editor shell matures, Producer's scene editing UI could adopt the same toolbar + panel system for consistency.

---

## Migration Path

The phased approach allows incremental adoption without breaking any editor:

1. **Phase 1 (Shared Components)**: Extract components, update imports. Each editor still uses its own layout. Zero visual change — this is pure refactoring.

2. **Phase 2 (Toolbar)**: Replace top bars one editor at a time. Start with Stitch (simplest top bar), then Compose (nearly identical), then Cut (most complex). Each editor can be deployed independently.

3. **Phase 3 (Panels)**: Only affects Compose (adding SidebarPanel). Cut and Stitch already use it.

4. **Phase 4 (Timeline)**: CSS-only changes for visual consistency. Timeline implementations stay as-is.

5. **Phase 5 (Keyboard)**: Additive. Import Controls class, register shortcuts. No existing behavior changes.

Each phase is independently shippable and testable. A phase can be reverted without affecting the others.

---

## What This Does NOT Change

- **Editor-specific domain logic**: Cut's frame-accurate clip creation, Stitch's sequence building, Compose's multi-layer crop composition — all stay as-is.
- **DataStar signal architecture**: Each editor keeps its signal-driven SSR pattern. Signals are renamed for consistency but the architecture is unchanged.
- **Timeline rendering engines**: Cut keeps its JS canvas timelines. Stitch/Compose keep their SSR block timelines. No timeline rewrite.
- **Backend routes and handlers**: SSE endpoints, signal reading, template rendering — all stay as-is. Only the templates change.
- **Export pipeline**: ffmpeg command generation, encoder service, job queuing — untouched.

---

## Success Criteria

1. A developer can add a new button style, export status state, or panel behavior in ONE place and it works in all three editors.
2. Users switching between Cut, Stitch, and Compose find the same controls in the same places for the same operations.
3. The total lines of templ code across all three editors decreases by ≥20% through component extraction and deduplication.
4. Adding a new editor type (e.g., a future "Mix" page for audio-only editing) requires only filling the shell's slots, not rebuilding chrome from scratch.
