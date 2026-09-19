# Agent UX notes (working log)

Collected while assembling the Bryan Callen Tesla stitch on 2026-09-05.
These are observations, not a commitment that any of this is scheduled.

Implementation update (2026-09-05): single-video transcript search, catalog uploader filtering, required topic groups with explicit ASR alternatives, durable full/range transcription, coverage reporting, and duration repair are now implemented. See [Agent clipping workflow](agent-clipping-workflow.md) for the tool sequence and regression coverage. The original observations below remain as historical context.

## What worked

- Local Rewind MCP at `http://localhost:9115/mcp` is the interface this Grok session can actually call (75 tools once connected).
- `get_clipping_workflow` → `find_clip_candidates` → `save_compilation_plan` → `create_stitch_project` is the right happy path when transcripts exist.
- `suggest_clip_boundaries` gave usable waveform edges once the range was known.
- `get_video_contact_sheet` / `get_video_frames` found the untranscribed Ben/Devan moment by showing the YouTube title and a white Model 3 on screen.

## Gaps that cost time

### Transcription is a dead end for agents

The 2026-09-03 Ben Avery show (`e946a74c-…`) was archived (~4h 10m) with seek sprites and a playable file, but **no captions**. `get_transcript` returned `no transcript`. There is no MCP tool to enqueue ASR. `ml_runtime_health` only reported `context_windows` and `refine_boundaries`.

Fallback used: `ffmpeg` slice + ingest `whisper-cli`. That should be a first-class operation (`enqueue_transcribe` / `transcribe_range`) with a job id the agent can poll.

`get_video` also returned `duration_seconds: null` for that livestream, so `get_video_contact_sheet` failed until duration was probed off the file. Persist duration after ingest.

### Search does not survive ASR or missing transcripts

- Bryan **Callen** is indexed as **Callan** in Ben Avery shows. `"Callen Tesla"` as an ALL query returned nothing on those videos.
- The 2018 TFATK highlight transcribes Tesla as “test lid” / “Testament” in places; title/comments still saved it.
- `find_clip_candidates` cannot take a `video_id`. After contact-sheet localization, the agent still cannot search that one file through MCP.
- Alternative `queries` are ANY-match. Bundling `"Tesla"` with `"how old"` pulled nursing stories and ages, not cars.
- `search_library` with `uploader: "Ben Avery"` still returned catalog hits from unrelated uploaders.

### People vs uploaders

`list_creators("Callen")` and `list_creators("Costa")` were empty. Devan Costa is a co-host, not a linked creator. Guest/co-host speech has no first-class identity, so the workflow’s “do not silently search everyone” rule fights a two-person conversation on someone else’s channel.

### Protocol map is easy to mix up

| Surface | What it is | What this session saw |
| --- | --- | --- |
| MCP `POST /mcp` | Archive tools for external agents | Connected; 401 without bearer |
| ACP | Rewind as a **client** to `grok agent stdio` | No HTTP `/acp`. Not something Grok-in-TUI calls |
| A2A `POST /api/a2a` | Inbound tasks | 401 without bearer; uses MCP read/write scopes |
| `/api/agent/*` | Browser assistant drawer | 401 from a raw HTTP client |

Docs should say: if you are already Grok with Rewind MCP configured, use MCP. ACP is for Rewind’s own assistant UI to spawn Grok, not the reverse.

### Chrome DevTools

`list_pages` / `new_page` failed because a chrome-devtools profile was already running. Isolated context (or documenting `--isolated`) would make UI verification possible in parallel with a user’s browser.

## 2026-09-05 follow-up

`list_clips` existed; **`create_clip` did not**. That is why a compilation was stitched as raw `type:video` ranges instead of clip-bank rows. Direct SQL to insert clips was the wrong workaround. `create_clip` is now an MCP write tool, and `create_stitch_project` should emit `type:clip` segments.

## 2026-09-05 follow-up: fat clips

Fat clips were caused by copying context-window (or 60s cue-window) bounds into `candidate.segment.start/end`. Agents then followed “copy candidate.segment” plus `merge_nearby:true`. The fix is at the source: segment start/end is a spoken beat around the matching cue, not the discussion window. YouTube auto-captions that stamp ~10ms points are treated as lasting until the next cue starts.

Missing episodes: `index_url` indexes a YouTube video, playlist, channel, channel search, or results URL as titles + English captions without downloading media. Then `find_clip_candidates`. Catalog-only rows need `enqueue_download` before they can play in Stitch.

## Skill / docs ideas

1. **Rewind clipping skill** — workflow, ANY vs ALL queries, ASR aliases (Callen/Callan, Tesla/test lid), untranscribed fallback (contact sheet → timed audio → whisper), editorial vs `sort_chronological`, stitch `web_path`.
2. **MCP: `enqueue_transcribe(video_id)`** and optional `start`/`end`. Return job id. Agents should not shell into `/downloads`.
3. **MCP: `find_clip_candidates.video_id`** so a localized file can be searched without a creator/channel scope.
4. **Search: window-level co-occurrence** (“Callen” and “Tesla” in the same context window, not the same cue).
5. **Creator/guest aliases** on people who speak but do not own the channel.
6. **Fix tool schema copy** — `get_creator` advertised as “Video UUID” in the host tool listing.

## 2026-09-06 follow-up: caption ingest vs ASR

`index_url` returns a `metadata-catalog` download-job ID. `get_transcription_status` looks up `ml_jobs` and returns no rows for that ID. Poll `get_index_status` instead. `find_clip_candidates` now includes `duration_seconds` so same-episode copies across uploaders can be dropped.

## This job’s leftover

Sept 3 still has no stored transcript. The stitch used a one-off whisper slice around `11451–11565`. Catchup ASR on that video would make the same moment findable next time without frames + ffmpeg.
