# Proposal: Universal Source Types for Stitch & Compose

## Summary

Extend Stitch and Compose to accept multiple source types beyond their current single-purpose models. Today, Stitch can only sequence **clips** and **title cards**; Compose can only crop a **single source video**. This proposal introduces a shared "source type" abstraction so that:

- **Stitch** can include: clips, full videos, compose exports, stitch exports, and title cards
- **Compose** can include: full videos, stitch exports, and compose exports as layer sources

This creates bidirectional interoperability between the two systems.

---

## Problem Statement

### Stitch is clip-only

The Stitch clip browser (`SearchClipsForStitch`) queries the `clips` table exclusively. To add a full video to a stitch sequence, users must first create a clip spanning the entire video on the Cut page, then find it in the Stitch clip browser. There is no way to directly add a video.

There is also no way to use the output of a Compose project (a multi-crop composition) as a segment in a Stitch sequence. Users must export the compose project, download the file, re-upload it as a new video, and then create a clip from it.

### Compose is single-video

A Compose project references exactly one `video_id`. All timeline segments pull crop regions from that single source. There is no way to bring in footage from other videos, stitch exports, or other compose exports as layer sources.

### No cross-system composition

There is no concept of using a stitch output inside compose (e.g., a compiled highlight reel as one layer of a PIP layout), or using a compose output inside stitch (e.g., a portrait-format podcast segment inserted into a landscape stitch sequence).

---

## Current Architecture

### Stitch Segment JSON

```json
{
  "type": "clip",
  "clip_id": "uuid",
  "video_id": "uuid",
  "duration": 12.5,
  "start_ts": 5.0,
  "end_ts": 17.5,
  "title": "Clip name",
  "transition": { "type": "fade", "duration": 0.5 },
  "filters": []
}
```

The encoder resolves `clip_id` → `clips` table → `video_id` → `/downloads/{video_id}/{video_id}.video{ext}`. Only two types exist: `"clip"` and `"title"`. The encoder hard-errors on any unknown type.

### Compose Timeline JSON

```json
{
  "id": "seg-id",
  "start_time": 0.0,
  "end_time": 15.0,
  "layers": [
    {
      "crop": { "x": 0.5, "y": 0.5, "width": 1.0, "height": 1.0 },
      "position": { "x": 0, "y": 0, "width": 1080, "height": 1920 },
      "z": 0
    }
  ],
  "transition": { "type": "fade", "duration": 0.5 }
}
```

The entire project references one `video_id` column on `compose_projects` / `compose_jobs`. The encoder resolves that single video to disk and uses it for all segments.

### Encoder file resolution

Both systems use `findVideoFile(dir, videoID)` which looks for `/downloads/{videoID}/{videoID}.video{.webm|.mp4|.mkv|.mov|.avi}`. Exported stitch/compose files live at `/exports/stitch/{jobID}{ext}` and `/exports/compose/{jobID}{ext}` respectively.

---

## Proposed Design

### Unified segment struct with `omitempty`

Today `stitchSegmentJSON` is a flat struct where every field is always present regardless of segment type. This means a title card segment carries empty `clip_id`, `video_id`, and `filters` fields, and a clip segment carries empty `text`, `bg_color`, etc. Adding more types (video, compose, stitch) would make this worse.

Use `json:",omitempty"` on all type-specific fields so only relevant data is serialized. This keeps the on-the-wire JSON clean and makes it obvious which fields belong to which type.

**Updated encoder struct:**

```go
type stitchSegmentJSON struct {
    Type     string  `json:"type"`
    Duration float64 `json:"duration"`
    Title    string  `json:"title,omitempty"`

    // Common media fields (clip, video, compose, stitch)
    StartTs float64             `json:"start_ts,omitempty"`
    EndTs   float64             `json:"end_ts,omitempty"`
    Filters []ffmpeg.FilterSpec `json:"filters,omitempty"`

    // Clip-only
    ClipID  string `json:"clip_id,omitempty"`
    VideoID string `json:"video_id,omitempty"`

    // Video-only (also uses VideoID)
    // (no additional fields — VideoID + StartTs/EndTs/Duration is sufficient)

    // Export references (compose/stitch exports)
    ExportJobID string `json:"export_job_id,omitempty"`

    // Title card fields
    BgColor   string `json:"bg_color,omitempty"`
    Text      string `json:"text,omitempty"`
    Subtitle  string `json:"subtitle,omitempty"`
    TextColor string `json:"text_color,omitempty"`
    FontSize  int    `json:"font_size,omitempty"`
    Position  string `json:"position,omitempty"`

    // Transition
    RawTransition json.RawMessage       `json:"transition,omitempty"`
    Transition    *stitchTransitionJSON  `json:"-"`
}
```

**Key design decisions:**

- `ExportJobID` is a single field used by both `type: "compose"` and `type: "stitch"` segments. The `type` field itself tells the encoder which table to look up (`compose_jobs` vs `stitch_jobs`). No need for separate `compose_job_id` / `stitch_job_id` fields.
- `VideoID` is reused: for `type: "clip"` it's the clip's source video (used for preview); for `type: "video"` it's the video itself.
- `Duration` is shared across all types — everything has a duration.
- `StartTs`/`EndTs` apply to clip, video, compose, and stitch segments (optional trimming).

**Example segment JSONs with `omitempty`:**

Clip:
```json
{ "type": "clip", "clip_id": "abc", "video_id": "def", "duration": 12.5, "start_ts": 5.0, "end_ts": 17.5, "title": "My Clip" }
```

Video:
```json
{ "type": "video", "video_id": "def", "duration": 3600.0, "title": "Full Video" }
```

Compose export:
```json
{ "type": "compose", "export_job_id": "ghi", "duration": 45.0, "title": "Podcast Cut" }
```

Stitch export:
```json
{ "type": "stitch", "export_job_id": "jkl", "duration": 120.0, "title": "Highlight Reel v2" }
```

Title card:
```json
{ "type": "title", "duration": 3, "text": "Chapter 1", "bg_color": "#000000", "text_color": "#ffffff", "font_size": 72, "position": "center" }
```

### Unified source browser with combined search

Instead of separate tabs per source type, use a **single combined search** that returns results from all source types at once. The codebase already uses `UNION ALL` patterns in `search_queries.sql`.

**Single SQL query — `SearchSourcesForStitch`:**

```sql
-- name: SearchSourcesForStitch :many
-- Combined search across clips, videos, and completed exports.
-- Returns a unified result set with a "source_type" discriminator.
WITH params AS (
    SELECT sqlc.arg(query)::text AS q,
           sqlc.arg(sort_by)::text AS sort,
           sqlc.arg(lim)::int AS lim,
           sqlc.arg(off)::int AS off,
           sqlc.arg(source_filter)::text AS src_filter
)
SELECT * FROM (
    -- Clips
    SELECT 'clip'::text AS source_type,
           c.id AS source_id,
           c.video_id,
           c.title,
           v.title AS parent_title,
           c.duration,
           c.start_ts,
           c.end_ts,
           c.color,
           c.created_at,
           ''::text AS file_path
    FROM clips c
    JOIN videos v ON c.video_id = v.id, params p
    WHERE (p.src_filter = '' OR p.src_filter = 'clip')
      AND (p.q = '' OR c.title ILIKE '%' || p.q || '%' OR v.title ILIKE '%' || p.q || '%')

    UNION ALL

    -- Videos
    SELECT 'video'::text AS source_type,
           v.id AS source_id,
           v.id AS video_id,
           v.title,
           v.uploader AS parent_title,
           v.duration_seconds::float8 AS duration,
           0::float8 AS start_ts,
           v.duration_seconds::float8 AS end_ts,
           ''::text AS color,
           v.created_at,
           ''::text AS file_path
    FROM videos v, params p
    WHERE (p.src_filter = '' OR p.src_filter = 'video')
      AND (p.q = '' OR v.search @@ websearch_to_tsquery('english', p.q))

    UNION ALL

    -- Compose exports (ready only)
    SELECT 'compose'::text AS source_type,
           cj.id AS source_id,
           NULL::uuid AS video_id,
           COALESCE(cp.title, 'Untitled Compose') AS title,
           cj.format AS parent_title,
           cj.duration_seconds::float8 AS duration,
           0::float8 AS start_ts,
           cj.duration_seconds::float8 AS end_ts,
           ''::text AS color,
           cj.created_at,
           cj.file_path
    FROM compose_jobs cj
    LEFT JOIN compose_projects cp ON cj.project_id = cp.id, params p
    WHERE cj.status = 'ready' AND cj.file_path != ''
      AND (p.src_filter = '' OR p.src_filter = 'compose')
      AND (p.q = '' OR cp.title ILIKE '%' || p.q || '%')

    UNION ALL

    -- Stitch exports (ready only)
    SELECT 'stitch'::text AS source_type,
           sj.id AS source_id,
           NULL::uuid AS video_id,
           sj.title,
           sj.format AS parent_title,
           sj.duration_seconds::float8 AS duration,
           0::float8 AS start_ts,
           sj.duration_seconds::float8 AS end_ts,
           ''::text AS color,
           sj.created_at,
           sj.file_path
    FROM stitch_jobs sj
    LEFT JOIN stitch_projects sp ON sj.project_id = sp.id, params p
    WHERE sj.status = 'ready' AND sj.file_path != ''
      AND (p.src_filter = '' OR p.src_filter = 'stitch')
      AND (p.q = '' OR sj.title ILIKE '%' || p.q || '%')
) AS combined
ORDER BY
    CASE WHEN (SELECT sort FROM params) = 'alpha'    THEN combined.title    END ASC,
    CASE WHEN (SELECT sort FROM params) = 'duration' THEN combined.duration END DESC,
    combined.created_at DESC
LIMIT (SELECT lim FROM params) + 1
OFFSET (SELECT off FROM params);
```

**Filter buttons instead of tabs:** The browser panel gets a row of compact toggle buttons: **ALL** | **CLIPS** | **VIDEOS** | **EXPORTS**. These set a `_stitchBrowserFilter` signal that maps to the `source_filter` query param. "ALL" sends `""`, "EXPORTS" sends both compose and stitch. This is a single endpoint, single SSE patch.

**Each result row shows a source-type badge** (small colored label: `CLIP`, `VIDEO`, `COMP`, `STCH`) so users can distinguish results at a glance.

### Migration: `duration_seconds` on export tables

The combined search query requires a `duration_seconds` column on `stitch_jobs` and `compose_jobs`. Today these tables don't store duration — the encoder just writes the output file and records `file_path` + `size_bytes`.

**New migration:**

```sql
-- +goose Up
ALTER TABLE stitch_jobs ADD COLUMN duration_seconds FLOAT8;
ALTER TABLE compose_jobs ADD COLUMN duration_seconds FLOAT8;

-- +goose Down
ALTER TABLE stitch_jobs DROP COLUMN IF EXISTS duration_seconds;
ALTER TABLE compose_jobs DROP COLUMN IF EXISTS duration_seconds;
```

The encoder already probes the output file after encoding (to validate it's ≥0.5s). Store `info.Duration` in `duration_seconds` at the same time it writes `file_path` and `size_bytes`.

**Encoder change** (both `stitchWorker` and `composeWorker`):

```go
// After successful probe:
_ = q.FinishStitchJobReady(ctx, &db.FinishStitchJobReadyParams{
    ID:              jobRow.ID,
    FilePath:        outputPath,
    SizeBytes:       stat.Size(),
    DurationSeconds: &info.Duration.Seconds(),  // NEW
})
```

Requires updating `FinishStitchJobReady` and `FinishComposeJobReady` queries to accept and store the new column.

### Stitch encoder: new segment types

Add three new cases to the `switch raw.Type` block in `cmd/encoder/stitch.go`:

```go
case "video":
    // Resolve video file directly by VideoID — no clip table lookup
    videoID := raw.VideoID
    if videoID == "" {
        return fmt.Errorf("segment %d: video segment missing video_id", i)
    }
    videoDir := filepath.Join(downloadsDir, videoID)
    inputPath := findVideoFile(videoDir, videoID)
    if inputPath == "" {
        return fmt.Errorf("video file not found for video %q in %s", videoID, videoDir)
    }
    start, dur := resolveTimestamps(raw, inputPath)
    segments = append(segments, ffmpeg.Segment{
        Type: ffmpeg.SegmentClip, Input: inputPath, Start: start, Duration: dur,
    })

case "compose":
    inputPath, dur, err := resolveExportFile(ctx, q, "compose", raw)
    if err != nil { return err }
    start := time.Duration(raw.StartTs * float64(time.Second))
    if raw.EndTs > raw.StartTs { dur = time.Duration((raw.EndTs - raw.StartTs) * float64(time.Second)) }
    segments = append(segments, ffmpeg.Segment{
        Type: ffmpeg.SegmentClip, Input: inputPath, Start: start, Duration: dur,
    })

case "stitch":
    inputPath, dur, err := resolveExportFile(ctx, q, "stitch", raw)
    if err != nil { return err }
    start := time.Duration(raw.StartTs * float64(time.Second))
    if raw.EndTs > raw.StartTs { dur = time.Duration((raw.EndTs - raw.StartTs) * float64(time.Second)) }
    segments = append(segments, ffmpeg.Segment{
        Type: ffmpeg.SegmentClip, Input: inputPath, Start: start, Duration: dur,
    })
```

Both compose and stitch export segments use the same helper:

```go
// resolveExportFile looks up a completed export job and returns its file path and duration.
func resolveExportFile(ctx context.Context, q *db.Queries, kind string, raw stitchSegmentJSON) (string, time.Duration, error) {
    if raw.ExportJobID == "" {
        return "", 0, fmt.Errorf("%s segment missing export_job_id", kind)
    }
    var filePath string
    var status db.ExportStatus
    var durSec *float64

    switch kind {
    case "compose":
        job, err := q.GetExportJobFile(ctx, raw.ExportJobID)  // new query
        if err != nil { return "", 0, fmt.Errorf("compose job %q not found: %w", raw.ExportJobID, err) }
        filePath, status, durSec = job.FilePath, job.Status, job.DurationSeconds
    case "stitch":
        job, err := q.GetExportJobFile(ctx, raw.ExportJobID)
        if err != nil { return "", 0, fmt.Errorf("stitch job %q not found: %w", raw.ExportJobID, err) }
        filePath, status, durSec = job.FilePath, job.Status, job.DurationSeconds
    }

    if status != db.ExportStatusReady {
        return "", 0, fmt.Errorf("%s job %q is not ready (status: %s)", kind, raw.ExportJobID, status)
    }
    if _, err := os.Stat(filePath); err != nil {
        return "", 0, fmt.Errorf("%s export file missing: %s", kind, filePath)
    }

    dur := time.Duration(raw.Duration * float64(time.Second))
    if dur <= 0 && durSec != nil {
        dur = time.Duration(*durSec * float64(time.Second))
    }
    if dur <= 0 {
        info, err := ffmpeg.Probe(filePath)
        if err != nil { return "", 0, fmt.Errorf("failed to probe %s export: %w", kind, err) }
        dur = info.Duration
    }
    return filePath, dur, nil
}
```

**New queries** (or a shared one if sqlc supports it — see below):

```sql
-- name: GetStitchExportFile :one
SELECT id, status, file_path, duration_seconds FROM stitch_jobs WHERE id = sqlc.arg(id);

-- name: GetComposeExportFile :one
SELECT id, status, file_path, duration_seconds FROM compose_jobs WHERE id = sqlc.arg(id);
```

Note: sqlc doesn't support cross-table polymorphic queries, so we need one query per table. The Go helper abstracts over both.

### Export preview streaming

Exports are regular video files on disk but have no streaming endpoint today. Add a generic export stream handler:

```
GET /api/exports/:kind/:id/stream   (kind = "stitch" | "compose")
```

The handler looks up the job row, verifies `status = ready`, and serves the `file_path` with HTTP range request support (same pattern as `HandleStream` for videos). The JS preview uses this URL for compose/stitch segment types.

### Multi-source Compose

**Goal:** Compose layers can reference different source files instead of being locked to the project's single `video_id`.

#### Data model

Add an optional `source` field to each compose layer:

```json
{
  "source": {
    "type": "video",
    "id": "uuid"
  }
}
```

When `source` is null/absent, the layer uses the project's default `video_id` (backward compatible). Supported source types:

| `source.type` | `source.id` resolves to | File location                     |
| ------------- | ----------------------- | --------------------------------- |
| `"video"`     | `videos.id`             | `/downloads/{id}/{id}.video{ext}` |
| `"stitch"`    | `stitch_jobs.id`        | `stitch_jobs.file_path`           |
| `"compose"`   | `compose_jobs.id`       | `compose_jobs.file_path`          |

**Updated compose layer JSON struct with `omitempty`:**

```go
type composeLayerJSON struct {
    ID       string            `json:"id,omitempty"`
    Label    string            `json:"label,omitempty"`
    Source   *layerSourceJSON  `json:"source,omitempty"` // nil = use project video_id
    Crop     cropJSON          `json:"crop"`
    Position positionJSON      `json:"position"`
    Z        int               `json:"z"`
}

type layerSourceJSON struct {
    Type string `json:"type"` // "video", "clip", "stitch", "compose"
    ID   string `json:"id"`   // UUID of the source
}
```

#### Encoder changes

The compose encoder currently uses one input (`[0:v]`, `[0:a]`). Multi-source requires:

1. **Collect unique source files:** Build a map of source → input index. The project's default `video_id` is always input `[0]`.
2. **Multiple `-i` args:** Each unique source file becomes a separate ffmpeg input. Clips resolve to their parent video file (two clips from the same video share one input).
3. **Per-layer input selection:** Each layer's filter chain references `[N:v]` where N is the input index for that layer's source. Clip layers apply their own trim (`start_ts`/`end_ts`) independently even if sharing an input with another clip from the same video.

**Changes to `pkg/ffmpeg/compose.go`:**

The existing `ComposeCommand()` signature adds a new parameter:

```go
func ComposeCommand(
    inputs []ComposeInput,    // NEW: array of input files (replaces single inputPath)
    canvas ComposeCanvas,
    segments []ComposeSegment,
    transitions []*ComposeTransition,
    // ... rest unchanged
) *Command
```

```go
type ComposeInput struct {
    Path string
    // No start/end here — each layer trims independently
}

type ComposeLayer struct {
    // Existing fields...
    InputIndex int  // which input this layer uses (0 = default)
}
```

The per-segment filter chain changes from:

```
[0:v] trim → split N → crop/scale each → overlay
```

to:

```
per-layer: [InputIndex:v] trim(segStart, segEnd) → crop/scale → overlay
```

This removes the single `split` approach but is actually simpler per-layer since each layer independently produces its own stream.

#### Database changes

**Migration:** Make `video_id` nullable on both compose tables.

```sql
-- +goose Up
ALTER TABLE compose_projects ALTER COLUMN video_id DROP NOT NULL;
ALTER TABLE compose_jobs ALTER COLUMN video_id DROP NOT NULL;

-- +goose Down
ALTER TABLE compose_projects ALTER COLUMN video_id SET NOT NULL;
ALTER TABLE compose_jobs ALTER COLUMN video_id SET NOT NULL;
```

Projects created from `/compose?video_id=...` still get a default `video_id`. Projects created without one (new "blank canvas" flow) start with `video_id = NULL` and every layer must specify its own source.

#### UI changes

The layer inspector gains a **Source** dropdown at the top of each layer's properties. Options:

- **Project Video** (default — uses the project's `video_id`)
- **Choose Video...** → opens a video picker popover (reuses the unified search)
- **Choose Clip...** → opens a clip picker popover (reuses the unified search filtered to clips)
- **Choose Export...** → opens an export picker popover

When a clip is selected as a source, the layer's time range auto-populates from the clip's `start_ts`/`end_ts`, and the clip's crop region is offered as an initial crop preset (the user can override it).

The source video player (left sidebar) switches to show whichever source the currently selected layer references.

Canvas preview loads a `<video>` element per unique source and draws them through the crop/position transforms. Limit to 6 simultaneous source videos to keep browser memory reasonable.

---

## Complexity & Risk Assessment

| Area                                     | Effort | Risk   | Notes                                                        |
| ---------------------------------------- | ------ | ------ | ------------------------------------------------------------ |
| Unified search query                     | Low    | Low    | Follows existing `UNION ALL` pattern in `search_queries.sql` |
| `duration_seconds` migration             | Low    | Low    | Additive column, backfill via probe on next export           |
| Segment struct + `omitempty`             | Low    | Low    | Backward compatible — existing JSON still deserializes       |
| Stitch encoder: `video` type             | Low    | Low    | ~20 lines, reuses `ffmpeg.SegmentClip`                       |
| Stitch encoder: `compose`/`stitch` types | Low    | Low    | Same pattern as `video`, resolves export file instead        |
| Export streaming endpoint                | Low    | Low    | Same pattern as video streaming                              |
| Source browser UI + templ components     | Medium | Low    | New component, filter buttons, source-type badges            |
| JS: `addStitchVideo/Compose/Stitch`      | Low    | Low    | Follows existing `addStitchClip` pattern                     |
| JS: preview for new segment types        | Medium | Low    | Video type reuses existing preview; exports need stream URL  |
| Compose: `layerSourceJSON` + `omitempty` | Low    | Low    | Additive field, `null` = backward compatible                 |
| Compose encoder: multi-input             | High   | Medium | Rearchitects single-input assumption in ffmpeg builder       |
| Compose UI: source picker per layer      | Medium | Medium | New popover, multi-video canvas preview                      |
| Compose: nullable `video_id` migration   | Low    | Low    | Additive change, existing projects keep their `video_id`     |

**Overall: Medium effort.** The stitch-side changes are straightforward. The compose multi-source is the only complex piece but it's architecturally contained in the ffmpeg builder.

---

## Decisions

1. **Circular references:** The UI excludes the current project's own exports from the source browser results (filter by `project_id != current`). The encoder also checks `stitch_jobs.project_id` and refuses to encode a segment that references an export from the same project. Clear error message: "Cannot use an export from the same project as a source."

2. **Export file cleanup:** Fail gracefully. If an export file is missing at encode time, the encoder marks the job as `error` with a clear message: "Source export file not found (may have been cleaned up). Re-export the source project first." No reference counting — the complexity isn't worth it for a self-hosted tool.

3. **Duration:** Solved by the `duration_seconds` migration. Stored at encode time, available at browse time. For existing exports without the column populated, the browser shows "?" and the encoder falls back to `ffmpeg.Probe()`.

4. **Format compatibility:** No warnings. FFmpeg handles transcoding transparently. The small extra encoding time is not worth a UI warning for a self-hosted tool.

---

## Affected Files

| File                                                          | Change                                                                                                            |
| ------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------- |
| `internal/db/sql/migrations/000XX_export_duration.sql`        | Add `duration_seconds` to `stitch_jobs` and `compose_jobs`                                                        |
| `internal/db/sql/migrations/000XX_compose_nullable_video.sql` | Make `compose_projects.video_id` and `compose_jobs.video_id` nullable                                             |
| `internal/db/sql/queries/stitch_queries.sql`                  | Add `SearchSourcesForStitch`, `GetStitchExportFile`; update `FinishStitchJobReady`                                |
| `internal/db/sql/queries/compose_queries.sql`                 | Add `GetComposeExportFile`; update `FinishComposeJobReady`, `CreateComposeProject` (nullable video_id)            |
| `cmd/encoder/stitch.go`                                       | Update `stitchSegmentJSON` with `omitempty`, add `video`/`compose`/`stitch` cases, add `resolveExportFile` helper |
| `cmd/encoder/compose.go`                                      | Update `composeLayerJSON` with `source` field + `omitempty`, multi-input resolution                               |
| `pkg/ffmpeg/compose.go`                                       | Rearchitect `ComposeCommand()` for `[]ComposeInput`, per-layer `InputIndex`                                       |
| `cmd/web/handlers/api/stitch_api/browser.go`                  | Replace clip-only handler with unified `HandleSourceBrowser`                                                      |
| `cmd/web/handlers/api/stitch_api/stream.go`                   | New: generic export streaming handler                                                                             |
| `cmd/web/internal/web/server.go`                              | Register `GET /api/stitch/sources`, `GET /api/exports/:kind/:id/stream`                                           |
| `cmd/web/templates/components/stitch_clip_browser.templ`      | Rename to `stitch_source_browser.templ`; unified results with source-type badges, filter buttons                  |
| `cmd/web/templates/stitch.templ`                              | Rename panel "CLIP BROWSER" → "SOURCE BROWSER", add filter signal                                                 |
| `cmd/web/templates/compose.templ`                             | Add source picker to layer inspector                                                                              |
| `static/js/stitch-page.js`                                    | Add `addStitchVideo`, `addStitchCompose`, `addStitchExport`; update preview + detail renderers                    |
| `static/js/compose-page.js`                                   | Layer source selector, multi-video canvas preview                                                                 |
| `cmd/web/handlers/api/compose_api/projects.go`                | Support nullable `video_id` in create/save                                                                        |
