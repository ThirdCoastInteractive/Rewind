-- UpsertVideoTranscript stores (or updates) a transcript for a video+lang.
-- name: UpsertVideoTranscript :exec
INSERT INTO video_transcripts (
    video_id,
    lang,
    format,
    text,
    search,
    raw,
    updated_at
)
VALUES (
    sqlc.arg(video_id),
    sqlc.arg(lang)::language_tag,
    sqlc.arg(format),
    sqlc.arg(text),
    to_tsvector('simple'::regconfig, coalesce(sqlc.arg(text), '')),
    sqlc.arg(raw),
    NOW()
)
ON CONFLICT (video_id, lang)
DO UPDATE SET
    format = EXCLUDED.format,
    text = EXCLUDED.text,
    search = EXCLUDED.search,
    raw = EXCLUDED.raw,
    updated_at = NOW();
