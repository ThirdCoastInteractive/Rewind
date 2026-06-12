# Multi-Crop Compose

## Overview

Allow users to take multiple crop regions from a single video and compose them into a new layout — typically converting a widescreen (16:9) podcast into a portrait (9:16) short with multiple close-ups arranged on a canvas.

A **timeline** lets users define multiple compositions that transition between each other (e.g., both hosts → tweet screenshot → single host reaction).

## Example Use Case

A podcast recorded at 1920×1080 (wide shot of two hosts + content between them):

**Segment 1** (0:00–0:15): Two close-ups stacked vertically in 9:16
```
┌─────────────┐
│  Host 1     │  ← crop from left side of source
│  (close-up) │
├─────────────┤
│  Host 2     │  ← crop from right side of source
│  (close-up) │
└─────────────┘
```

**Segment 2** (0:15–0:45): Host 1 small + tweet screenshot
```
┌─────────────┐
│  Host 1     │  ← small crop, top corner
│  (reaction) │
├─────────────┤
│             │
│  Tweet      │  ← crop from center of source
│  content    │
│             │
└─────────────┘
```

Transitions (xfade) between segments, reusing the same 38+ transition types from the stitch system.

## Data Model

### ComposeProject (saved for editing)

```sql
CREATE TABLE compose_projects (
    id UUID PRIMARY KEY,
    video_id UUID NOT NULL REFERENCES videos(id),
    created_by UUID NOT NULL REFERENCES users(id),
    title TEXT DEFAULT 'Untitled Compose',
    canvas JSONB,       -- {"width": 1080, "height": 1920, "color": "#000000"}
    timeline JSONB,     -- ordered timeline segments with layers
    format TEXT DEFAULT 'mp4',
    quality TEXT DEFAULT 'high',
    created_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ
);
```

### ComposeJob (export queue)

```sql
CREATE TABLE compose_jobs (
    id UUID PRIMARY KEY,
    project_id UUID REFERENCES compose_projects(id),
    video_id UUID NOT NULL REFERENCES videos(id),
    created_by UUID NOT NULL REFERENCES users(id),
    canvas JSONB NOT NULL,
    timeline JSONB NOT NULL,
    format TEXT, quality TEXT,
    status export_status DEFAULT 'queued',
    progress_pct INT,
    file_path TEXT, size_bytes BIGINT,
    last_error TEXT, attempts INT,
    locked_at TIMESTAMPTZ, locked_by TEXT,
    started_at TIMESTAMPTZ, finished_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ
);
```

### Timeline JSONB Structure

```json
[
  {
    "id": "uuid",
    "start_time": 0.0,
    "end_time": 15.0,
    "layers": [
      {
        "id": "uuid",
        "label": "Host 1",
        "crop": { "x": 0.25, "y": 0.5, "width": 0.5, "height": 0.7 },
        "position": { "x": 0, "y": 0, "width": 1080, "height": 960 },
        "z": 0
      },
      {
        "id": "uuid",
        "label": "Host 2",
        "crop": { "x": 0.75, "y": 0.5, "width": 0.5, "height": 0.7 },
        "position": { "x": 0, "y": 960, "width": 1080, "height": 960 },
        "z": 1
      }
    ],
    "transition": { "type": "fade", "duration": 0.5 }
  }
]
```

## FFmpeg Strategy

Single `ffmpeg` command with `filter_complex`, using one input (`-i source.mp4`):

1. **Per-segment trim**: `[0:v]trim=start:end,setpts=PTS-STARTPTS`
2. **Split** for N layers: `split=N[raw0][raw1]...`
3. **Per-layer crop+scale**: `crop=...,scale=W:H`
4. **Canvas + overlay chain**: `color=c=black:s=WxH:d=dur` → sequential `overlay=x:y`
5. **Normalize**: `fps=30,format=yuv420p,setsar=1`
6. **Xfade chain** between composed segments (reusing stitch pattern)
7. **Audio**: `atrim` per segment from `[0:a]`, `acrossfade` between segments

Example for 2 segments, 2 layers each:
```
[0:v]trim=0:15,setpts=PTS-STARTPTS,split=2[s0r0][s0r1];
[s0r0]crop=...,scale=1080:960[s0l0];
[s0r1]crop=...,scale=1080:960[s0l1];
color=c=black:s=1080x1920:d=15:r=30[s0bg];
[s0bg][s0l0]overlay=0:0[s0o0];
[s0o0][s0l1]overlay=0:960[s0v];
[s0v]fps=30,format=yuv420p,setsar=1[seg0v];

[0:v]trim=15:30,setpts=PTS-STARTPTS,split=2[s1r0][s1r1];
... (same pattern)
[s1v]fps=30,format=yuv420p,setsar=1[seg1v];

[seg0v][seg1v]xfade=transition=fade:duration=0.5:offset=14.5[finalv];

[0:a]atrim=0:15,asetpts=PTS-STARTPTS,aresample=48000[seg0a];
[0:a]atrim=15:30,asetpts=PTS-STARTPTS,aresample=48000[seg1a];
[seg0a][seg1a]acrossfade=d=0.5[finala]
```

## Implementation Plan

### New Files

| File | Purpose |
|------|---------|
| `internal/db/sql/migrations/00029_compose.sql` | Tables |
| `internal/db/sql/queries/compose_queries.sql` | CRUD + job queries |
| `pkg/ffmpeg/compose.go` | ComposeCommand builder |
| `cmd/encoder/compose.go` | Compose job worker |
| `cmd/web/handlers/api/compose_api/*.go` | API handlers |
| `cmd/web/templates/compose.templ` | Editor page |
| `cmd/web/templates/compose_library.templ` | Project grid |
| `static/js/compose-page.js` | Canvas preview + timeline |

### Modified Files

| File | Change |
|------|--------|
| `cmd/web/internal/web/server.go` | Add compose routes |
| `cmd/encoder/main.go` | Add compose worker goroutine |
| `cmd/web/handlers/content/*.go` | Compose page content handlers |

### Canvas Presets

| Name | Dimensions | Aspect Ratio |
|------|-----------|--------------|
| Portrait (9:16) | 1080 × 1920 | TikTok/Reels/Shorts |
| Square (1:1) | 1080 × 1080 | Instagram |
| Landscape (16:9) | 1920 × 1080 | YouTube |
| Ultrawide (21:9) | 2560 × 1080 | Cinema |

### UI Layout

```
┌──────────────────────────────────────────────────────────┐
│ [Title] [Canvas: 9:16 ▾] [Format: MP4 ▾] [Export]       │
├──────────────┬───────────────────────┬───────────────────┤
│ Source Video  │   Canvas Preview      │  Layer Inspector  │
│ (reference)  │   ┌──────────┐        │  ┌─────────────┐  │
│              │   │          │        │  │ Label: ___   │  │
│              │   │ composed │        │  │ Crop: drag   │  │
│              │   │ preview  │        │  │ Pos: x,y,w,h │  │
│              │   │          │        │  │ Z-order: ↑↓  │  │
│              │   └──────────┘        │  └─────────────┘  │
├──────────────┴───────────────────────┴───────────────────┤
│ Timeline                                                 │
│ [Seg 1: 0:00–0:15 ][◇ fade][Seg 2: 0:15–0:45 ][+ Add]  │
└──────────────────────────────────────────────────────────┘
```
