# MCP — connect an agent to Rewind

Rewind serves [Model Context Protocol](https://modelcontextprotocol.io) at **`/mcp`** (Streamable HTTP) so you can point Claude Desktop, Cursor, or similar at your archive.

## Token

1. Sign in and open **Settings**.
2. Under **MCP**, label a token (e.g. “Claude Desktop”) and click **Create token**.
3. Copy the `rw_…` secret. It is shown once.

## Client config

```json
{
  "mcpServers": {
    "rewind": {
      "url": "http://localhost:9115/mcp",
      "headers": {
        "Authorization": "Bearer rw_YOUR_SECRET"
      }
    }
  }
}
```

Use your real Rewind origin instead of `localhost:9115` if you expose it elsewhere.

## Agent-driven clipping

Use an MCP-capable agent host with tool calling; Rewind does not require a particular model or a locally installed skill. The server advertises clipping instructions on connection. Hosts can call `get_clipping_workflow` or load the `compile_spoken_moments` prompt for the full procedure.

For example: “Find every time Jeremy Hambley talks about Adam, Adam Sellers, and hot tubs, and create a new stitch project compiling them together.”

1. Resolve Jeremy using `list_creators` / `get_creator`, or `list_channels` / `get_channel`. Keep the returned creator or channel UUID in the search scope.
2. Call `find_clip_candidates` with `queries: ["Adam", "\"Adam Sellers\"", "hot tub"]`, that scope, and `limit: 10`. Alternatives match **any** query; words inside each query must all match. For a request about these topics together, the agent checks co-occurrence in the returned discussion context.
3. Review evidence and context, disambiguate names, and follow both continuation offsets while `has_more_candidates` is true. Each candidate includes a `segment` whose `start`/`end` is a proposed spoken beat around the evidence timestamp, not the discussion window. Context windows remain citation metadata; never copy `context_windows[].start/end` or `context_start`/`context_end` onto the clip. Widen only if `get_transcript` shows the claim continues, typically still under 25s. Deduplicate overlapping ranges and alternative-language matches before saving.
4. Save the first selected page with `save_compilation_plan`, then use `append_compilation_segments` for later pages, passing the current plan ID and revision each time. This keeps a long compilation out of the model's context. Use sorting/merging options consistently if desired. A stale revision fails without modifying the plan; inspect the plan before retrying an uncertain append. When selection is complete, call `create_stitch_project` with the current ID and revision. The response includes `/stitch/{project_id}`. This creates an editable project with video ranges, without downloading sources or queuing an export. Repeating the same revision returns the same project and preserves human edits; a new revision creates a new snapshot.
5. If a rendered export is requested, use `create_compilation` and inspect its execution through `get_compilation_plan`. Rendering creates a separate snapshot project and may download missing media. It does not render subsequent manual edits to the editable project; those can be exported from the stitch editor.

Search covers available indexed transcripts, not untranscribed videos or an entire creator's publishing history. Pagination ending does not prove exhaustive recall. ASR errors, ambiguous names, and guest speech still require judgment. For related episodes that are not in the archive, `index_url` / `index_channel_catalog` pull titles and English captions without media; poll `get_index_status`, not `get_transcription_status`. Small models receive compact candidates and explicit next steps, but must support reliable tool calls; no particular Qwen model has been validated by the automated tests.

Verification: `make test` checks tool discovery and validation over an MCP connection. `make integration-up` then `make integration-test` exercises search, pagination, language selection, plan creation, project idempotency, ownership, and revision checks against the disposable database on port 15439.

## Short-form teasers

For Shorts, TikToks, Reels, and other vertical teasers, start with `get_shortform_workflow`. Use `suggest_teasers` for transcript-backed candidates, `create_teaser` for an editable portrait Stitch project, `set_teaser_layout` for per-shot framing, and `style_teaser_captions` for readable captions. `check_teaser` reports structural issues and the preview steps needed before export. The existing `stitch_frame`, `stitch_preview`, and `stitch_export` tools render explicit project revisions.

See [the short-form workflow](agent-shortform-workflow.md) for parameters, layouts, and examples. These tools share Stitch ownership, revision history, and undo; archived originals are preserved.

## Archive OSINT desk

This is an observe-and-flag desk over **public posts already in the archive**, not a live community product. Commenters are keyed by `(source, author_id)`. Style matches are hypotheses. Tools do not moderate, enforce, or identify real-world persons. Start with `get_osint_workflow`.

## Tools (read)

- `search_library` — title, uploader, tags, comments, transcripts, context windows. Optional `creator_id`, `channel_id`, `sources` (OR filter; “talked about” = `transcript` + `context_windows`), `limit` (default 25), `include_catalog` (default true)
- `search_transcripts` — timestamped cue hits with ±30s default context (`context_seconds` default 60). Nearby hits in the same Context Window collapse into one candidate. Optional `creator_id`, `channel_id`, `limit` (default 25)
- `get_clipping_workflow` — agent workflow and tool sequence for spoken-moment compilations
- `find_clip_candidates` — compact transcript evidence and suggested plan segments; accepts up to eight alternative `queries`, creator/channel scope, and continuation offsets
- `get_transcript` — cleaned text / cues, optional time range
- `get_video` — metadata
- `get_context_windows` — chapter Context Windows intersecting an optional video range; each includes nested `shorts` (8–45s) when generated
- `list_ml_jobs` — ML queue with priority (lower runs first) and per-kind counts
- `suggest_clip_boundaries` — silence-aware, frame-snapped start/end candidates
- `get_compilation_plan` — persisted plan, segments, media readiness, render state
- `list_compilation_plans` — plans owned by the authenticated user
- `get_video_frames` — inspect 1–8 timestamped JPEGs (preview = seek sprites, detail = archived frame). Inspect images before describing them
- `get_video_contact_sheet` — numbered contact sheet over a range (max 24 cells)
- `search_visual_moments` — CLIP similarities for text, an uploaded `reference_id`, or a `frame_ref`. Scores are similarities, not probabilities; call `get_video_frames` before drawing conclusions
- `visual_index_status` — visual indexing progress and missing-asset errors
- `ml_runtime_health` — per-kind inference cooldown and last error
- `get_related` — same-channel videos, clips, markers, neighbor channels, and other-channel windows sharing a canonical topic
- `list_topic_windows` — playable context windows bound to a topic slug or query (cross-channel; inferred from context windows)
- `library_stats`
- `resolve_uri` — `rewind://video/{id}`
- `list_channels` / `get_channel` — channel stats, creator, harvested outlink/mention edges
- `list_channel_videos` — paginated titles with `media=file|metadata`
- `list_channel_catalog` — recent titles and truncated descriptions (including metadata-only rows)
- `get_channel_neighborhood` — 1-hop in/out (including unresolved URLs)
- `analyze_channel` — views/day, cadence, format mix, engagement vs historical baseline, plus harvested comment engagement (organic vs raid/copypaste/sock-suspect)
- `get_channel_comment_engagement` — comments per 1k views with organic vs campaign/sock-suspect split; observations, not proof
- `compare_channels` — side-by-side analyze of two uploaders
- `analyze_follows` — same report across enabled follows, dying first
- `list_creators` / `get_creator` / `analyze_creator`
- `list_creator_suggestions` — pending Accept/Dismiss nominations
- `get_channel_graph` — full graph dump (prefer neighborhood for one channel)
- `suggest_channel_links`
- `search_comments` — one video, one uploader, or the library
- `get_osint_workflow` — archive OSINT desk rules and tool sequence (observe/flag only)
- `search_commenters` — commenter search; optional `watchlisted` / `flagged`; empty query returns `[]`
- `get_commenter` — dossier JSON (identity, scores, flags, campaigns, style neighbors, evidence URIs); not a citation
- `list_commenter_comments` — paginated `kind=comment` citations for one commenter
- `list_osint_flags` — optional `kind`; open-only by default
- `list_campaigns` / `get_campaign`
- `get_video_comment_tone` — per-video rollup + up to 10 sample scored comments
- `get_video_speech_tone` — window-aligned speech scores
- `list_clips` / `list_markers` / `list_follows`
- `list_show_notes` / `get_show_note` — collaborative Markdown, revision, parsed references, reviews, and room cursor
- `get_stitch_project` — owned project title, format, and segment list
- `stitch_inspect` — owned canonical document, revision, resolved segments/captions/overlays; optional `start_us`/`end_us` range filter

## Tools (write)

Require `mcp:write`. Tokens minted in Settings currently include both `mcp:read` and `mcp:write`.

- `index_url` — index a YouTube video, playlist, channel, channel search, or results URL as titles + English captions without downloading media
- `get_index_status` — poll a caption-index job from `index_url`, `index_channel_catalog`, or `index_creator_catalog`
- `index_channel_catalog` — index or refresh a complete channel catalog (subtitles included)
- `index_creator_catalog` — index or refresh every channel linked to a creator
- `save_compilation_plan` — persist a creator/query compilation plan without rendering. Optional `sort_chronological` / `merge_nearby`; default keeps submitted editorial order
- `update_compilation_plan` — replace ordered segments and bump revision (requires current `revision`)
- `append_compilation_segments` — append a selected page to a plan without resending earlier segments (requires current `revision`)
- `create_compilation` — render one execution per plan revision. Pass `retry` to revive a failed execution. Download and stitch events resume automatically
- `create_clip` — mark an in/out range on a video so it appears in the clip bank
- `create_stitch_project` — create Clip records from an owned plan revision and an editable stitch of those clips; preserves edits on retry; no automatic download or render
- `update_stitch_project` — title-only update on an owned project (`title`, `expected_revision`, `operation_key`). Segment, format, and quality replacement are rejected; use `stitch_apply`
- `stitch_apply` — typed operations on an owned project (`expected_revision`, `operation_key`, `summary`, `operations`). Atomic, revision-checked, and retry-safe under the same key
- `stitch_export` — queue an immutable export of an owned project at an explicit `revision` and `operation_key` (does not edit the document). Poll with `stitch_render_status`; related tools are `stitch_preview` and `stitch_frame`
- `create_context_window` / `update_context_window` — bounds are checked against video duration
- `enqueue_context_windows` — queue generation for the current transcript fingerprint and jump the background line; returns `job_id` and `priority`
- `set_ml_job_priority` — reorder one claimable ML job (lower numbers run first)
- `cancel_ml_job` — cancel one job, including an in-flight run
- `clear_ml_queue` — cancel queued, waiting, and in-flight jobs; `background_only=true` limits to archive-wide priority 200+ work
- `retry_ml_job` — requeue one failed, waiting, cancelled, or superseded ML job
- `index_visual_range` — queue CLIP indexing for a video or range; `dense=true` uses one-second samples
- `enqueue_download`
- `follow_channel` / `unfollow_channel`
- `index_channel_descriptions` — metadata-only crawl (titles/descriptions, no media)
- `create_creator` / `link_channel_to_creator` / `unlink_channel`
- `accept_creator_suggestion` / `dismiss_creator_suggestion`
- `add_tag` / `refresh_video_metadata`
- `join_show_note_room` / `wait_show_note_events` / `leave_show_note_room` — renewable agent presence and cursor-based room events
- `post_show_note_message` — add a message to the shared chronological room
- `add_show_note_comment` — create a Yjs-anchored comment from an exact revision and 1-based range
- `propose_show_note_patch` — submit a validated single-document unified diff for owner/host review; agents cannot approve it
- `watch_commenter` / `unwatch_commenter` — per-user watchlist (observe only)
- `dismiss_osint_flag` — close an open flag (does not enforce)
- `assert_commenter_link` — operator `kind=user` assertion between two ordered commenter ids
- `enqueue_comment_classify` / `enqueue_speech_tone` — queue ML jobs for a video
- `index_x_replies` — index public replies on an archived X status URL; empty extractor output fails visibly (no DMs/firehose)

## Show-note resources

`rewind://show-note/{id}` returns the note title, authoritative Markdown,
revision, parsed references, open and closed reviews, and room cursor. Use
`wait_show_note_events` as the compatibility path for clients without MCP
resource subscriptions. Access is evaluated as the token's owning user against
the note owner/host roster; possessing a note UUID never grants access.

## Prompts

- `find_quote` — search transcripts and cite
- `compile_spoken_moments` — resolve speaker, search all candidate pages, review evidence, save a plan, and create an editable stitch project
- `analyze_channel` — trajectory + relationships for one uploader
- `propose_rundown` — search transcripts/visuals, inspect frames, propose a show-note patch, wait for human review, then `create_compilation` only after an explicit request
