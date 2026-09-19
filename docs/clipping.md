# Clipping

Rewind keeps the original file. Clips, Stitch projects, and exports are derived from it.

## Watch

Open a video to play it with a synced transcript. Click a cue to jump to that moment. Search the transcript to find a line, then mark in/out points or create a clip from the result.

Other panels on the watch page:

- **Chapters & markers** — timestamped markers, including SponsorBlock segments on YouTube
- **Clips** — clip bank for this video
- **Comments** — imported comments with clickable timestamps
- **Context** — generated chapter windows and nested shorts (8–45s spoken beats)

Captions come from [whisper.cpp](https://github.com/ggml-org/whisper.cpp) in `rewind-ml`. Context windows are optional and use the local Ollama model configured in Admin.

Keyboard shortcuts for the player and watch page are in [Keyboard shortcuts](keyboard-shortcuts.md).

## Cut

**Cut** (`/videos/{id}/cut`) is the single-video editor: zoomable timeline, in/out markers, subtitle alignment, color filters, crop presets, and export.

Typical path:

1. Set in/out on the work timeline (or press `I` / `O`)
2. Create a clip from the range
3. Optionally stack FFmpeg filters (brightness, contrast, blur, color balance, …) with live preview
4. Optionally add crop variants (16:9, 9:16, 1:1, …)
5. Export from the sidebar (format, quality, crop)

Cut never rewrites the archived original. Filters and crops apply to the export.

## Stitch

**Stitch** is the multi-source editor. Projects live at `/stitch` and `/stitch/{id}`.

A project is an ordered sequence of clips, title cards, gaps, and transitions, plus timed overlays (text, images, shapes). Captions follow their source segment. Export queues a render of an explicit revision.

Agents can create a Stitch project from spoken-moment search (see [MCP](mcp.md)) without downloading extra media. Human edits after that are ordinary Stitch edits; rendering an agent compilation does not overwrite those edits.

## Compilations

**Compilations** are saved search plans: ordered segments with a revision. Creating a Stitch project from a plan snapshots the current revision into an editable timeline. Rendering a compilation is a separate, immutable export of that snapshot.
