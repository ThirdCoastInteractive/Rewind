# Cut / Stitch: inspection and proposed editor

Inspected September 8, 2026. This is a design proposal, not an implemented editor upgrade.

## What I inspected

- The supplied screenshot: a Stitch project, beginning with a title card.
- The running app at `http://localhost:9115`: opened Cut for the September 3rd show and selected Chapter 1. Selection revealed the clip inspector, metadata, crop presets, export options, and source in/out range. I did not change clip content or export media.
- The Stitch library in this browser session showed no projects, so I could not reproduce the exact saved project from the screenshot. Its behavior below is based on source inspection, not a claim of testing that project.
- Current templates, timeline and playback JavaScript, caption rendering code, and Rewind's MCP documentation and tool registrations. There was no connected Rewind MCP tool in this session; I inspected its implementation rather than minting credentials.
- Existing design proposals were historical context, not requirements that override the request.

## Why it feels difficult

1. **The editor is split across product names.** Cut selects ranges from a source; Stitch arranges ranges and rendered media into a sequence. A user who wants to “edit this clip” should not need to understand that architecture first.
2. **Selection hides its consequences.** In Stitch, the selected segment's details appear below the long source browser. In Cut, no inspector appears until a clip is selected.
3. **Trim is too subtle.** Stitch hit testing exposes trim handles only for the selected segment, within six pixels of its edges. A long project fitted into a small timeline makes this particularly hard to discover and control.
4. **Export decisions compete with editing.** Container, quality, loudness and caption burn-in are prominent before the user has assembled a cut.
5. **The preview does not explain the composition.** A black title card and a nearly solid timeline give little indication of what follows, which part is selected, or how to shorten it.
6. **Captions and overlays need more than new buttons.** Export caption burn-in exists, but the inspected Stitch preview does not expose a live caption authoring surface. Sequential title cards are not a general system of timed text and graphic overlays.

## Existing capability versus proposal

| Area | Evidence in current implementation | Proposed experience |
| --- | --- | --- |
| Source clip editing | Cut has in/out setting, frame nudges, clip selection, metadata and crop presets | Direct “Edit clip” entry; obvious trim fields; source time and project time labeled separately |
| Sequence editing | Stitch has selection, trim dragging, reorder, duplicate, removal and transitions | Persistent selected-item inspector, split at playhead, undo/redo, readable filmstrip and zoom-to-selection |
| Captions | Transcript data plus FFmpeg burn-in controls and font selection | Editable timed cues, preview while scrubbing, style controls, safe areas, missing-caption state |
| Titles | Styled sequential title cards with live title preview | Keep title cards, add timed text over video as a distinct object |
| Layers | No general timed overlay authoring system found in inspected Cut/Stitch paths | Ordered layers for text, imported graphics, callouts, arrows and freehand strokes; visibility, lock and duration |
| Audio | Stitch per-segment mix controls and global loudness matching | Clear volume/fade inspector, waveform, mute and contextual loudness options |
| Agent assistance | MCP transcript search, suggested boundaries, clip creation and project segment operations | Optional “Find a moment” and “Suggest a cleaner boundary” actions that propose visible edits |

## Recommended interaction rules

- One project, three focused views: Edit, Captions, Layers. Keep preview position, playhead and selection stable when switching in the eventual product.
- A single wider left panel with Edit / Captions / Layers workspace tabs, selected-item controls first, and supporting lists in collapsible sections. User feedback on the first mockup explicitly rejected tools on both sides of the preview.
- Show a zoomed work area plus a compact full-project overview. Long chapters must not become indistinguishable rectangles.
- “Remove from project” means remove a timeline instance, never delete archived video. Undo should cover trims, splits, timing, layer changes and caption edits.
- Keep archive transcript corrections separate from project-only caption edits. The default should affect this project only.
- Caption timing maps source cues into project time after trims and rearrangement. Word highlighting requires word timings; cue-only sources need a plain caption fallback.
- Preview and export must use the same layout, fonts, timing and layer ordering. Browser text overlay alone is not enough to guarantee exported parity.
- Layers need stable IDs, project-time start/end, stacking order, transforms, styling and asset references. Persist those properties and teach the export compositor to render them.
- Display source shortages, missing caption data and unavailable media explicitly. Never show “ready” based solely on a successful metadata save.
- Before adding agent editing to this workspace, preserve project settings: the inspected `update_stitch_project` MCP implementation replaces the segment list and writes an empty `GlobalFilters` array. That can clear existing caption/loudness settings. Prefer revision-checked, atomic trim/split/layer operations and shared undo history.

## Useful additions, in priority order

1. Split, undo/redo, snapping, precise trim and zoom to selection.
2. Caption correction, cue navigation, caption style preview and portrait safe areas.
3. Text, image, callout, arrow, rectangle, ellipse and freehand overlay tools.
4. Audio fades, waveform and per-segment gain; mute a range without removing video.
5. Aspect-ratio presets and duplicate project variants for landscape / portrait output.
6. Reviewable silence-boundary suggestions and transcript search, using capabilities Rewind already has. Avoid a permanent assistant panel that competes with editing.

## Source map

- `cmd/web/templates/video_cut.templ`, `video_cut_components.templ`, `clip_inspector.templ`: source clip workspace.
- `static/js/cut-page.js`: range and clip editing behavior.
- `cmd/web/templates/stitch.templ`: toolbar, source browser and conditional detail placement.
- `cmd/web/templates/components/stitch_detail.templ`: segment-specific inspector and title fields.
- `static/js/stitch-page.js`: trims, keyboard shortcuts, sequence state and autosave.
- `static/js/lib/nle-timeline.js`: timeline rendering and selected-edge trim hit testing.
- `pkg/ffmpeg/stitch_subs.go`: caption source selection and export burn-in.
- `docs/mcp.md`, `internal/mcp/clipping.go`: clipping and Stitch MCP operations.

## Scope of the HTML

The linked mockups illustrate the proposed interface with sample media and browser-local interactions. They do not process video, save projects, or call the Rewind API. Implementing the full proposal requires editing-state, persistence, preview and rendering changes beyond these static files.
