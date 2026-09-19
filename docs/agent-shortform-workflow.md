# Short-form teaser workflow

The teaser tools keep source selection evidence based and the resulting edit
canonical. `suggest_teasers` searches the same indexed transcript path as
`find_clip_candidates`; it returns the matching transcript evidence, a spoken
beat range, transcript coverage, and a continuation offset. Its duration
filter is bounded to Rewind's 180-second short-form work limit and does not turn a search result into a claim that every
episode or occurrence was searched.

After reviewing candidates, call `create_teaser` with selected `video_id`,
`start`, and `end` ranges. Supply a stable `operation_key`. Creation returns an
owned editable Stitch project and revision. Retrying the same key and inputs
returns the same project; reusing it with different inputs is rejected.

Use `set_teaser_layout` with the current revision and another operation key.
The default canvas is 1080x1920. Segment crops are normalized to 0..1 and use
`single_speaker`, `two_speakers`, or `preserve_scene`. Layout changes are
revisioned and atomic with the existing Stitch store.

`style_teaser_captions` may attach caption styling after captions are present.
Finish with `check_teaser` to inspect document validation, timing, captions,
and source media readiness. Structural checks do not perform visual speaker
detection or audio quality analysis. Queue or inspect a Stitch preview/frame
when those questions matter, and report catalog-only or missing media as a
blocking issue for preview/export.

## Tool sequence

1. `get_shortform_workflow({})` describes the current workflow. For a known
   episode, inspect `get_context_windows` and its nested `shorts` first.
2. `suggest_teasers({"video_id":"<video UUID>","limit":10})` returns those nested
   shorts when a v4 context generation exists. With a topic `query` it searches
   transcripts instead. Scope-only suggestions without shorts sample transcript
   positions; they are heuristic candidates, not a complete ranking of an episode.
3. `create_teaser({"title":"Teaser","operation_key":"teaser-01","segments":[{"video_id":"<video UUID>","start":12.5,"end":35}]})`
   creates a 1080x1920 project. Replace example times with verified source bounds.
4. `set_teaser_layout` takes `project_id`, `expected_revision`, `operation_key`,
   `segment_id`, and `layout`. For example, a full-frame crop is
   `{"mode":"single_speaker","crops":[{"x":0,"y":0,"width":1,"height":1}]}`.
   Two-speaker mode needs two crop rectangles; these are explicit crops, not
   automatic speaker tracking. Preserve-scene mode needs no crop.
5. `style_teaser_captions` takes `project_id`, `revision`, `operation_key`, and
   `style` (`basic`, `bold`, `outlined`, or `karaoke`). To import and style in one
   transaction, also pass `import_captions:true`, `segment_id`, and `language`.
   Word highlighting requires verified word timing; otherwise the tool retains
   cue timing and returns alignment follow-ups. Margins are normalized, while
   stored font sizes and caption positions are canvas pixels.
6. `check_teaser({"project_id":"<project UUID>"})` returns structural findings
   and review calls. Optional `queue_preview:true` requires write permission and
   an operation key. Inspect the preview, then use the existing `stitch_export`.

Use the revision returned by each edit for the next edit. Retrying an operation
must use the same key and payload; use a new key for a deliberate new change.
