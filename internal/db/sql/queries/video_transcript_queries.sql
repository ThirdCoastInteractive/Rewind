-- UpsertVideoTranscript stores (or updates) a transcript for a video+lang.
-- name: UpsertVideoTranscript :exec
INSERT INTO video_transcripts (
    video_id,
    lang,
    format,
    text,
    search,
    raw,
    cues,
    updated_at
)
VALUES (
    sqlc.arg(video_id),
    sqlc.arg(lang)::language_tag,
    sqlc.arg(format),
    sqlc.arg(text),
    to_tsvector('simple'::regconfig, coalesce(sqlc.arg(text), '')),
    sqlc.arg(raw),
    COALESCE(sqlc.arg(cues)::jsonb, '[]'::jsonb),
    NOW()
)
ON CONFLICT (video_id, lang)
DO UPDATE SET
    format = EXCLUDED.format,
    text = EXCLUDED.text,
    search = EXCLUDED.search,
    raw = EXCLUDED.raw,
    cues = EXCLUDED.cues,
    updated_at = NOW();

-- GetVideoTranscript returns one transcript for a video (any language).
-- name: GetVideoTranscript :one
SELECT * FROM video_transcripts
WHERE video_id = sqlc.arg(video_id)
ORDER BY CASE WHEN lang::text = 'en' THEN 0 ELSE 1 END, lang
LIMIT 1;

-- SearchTranscripts finds videos whose cleaned transcript matches tsquery.
-- name: SearchTranscripts :many
SELECT vt.video_id, v.title, v.uploader, vt.lang, vt.text, vt.cues,
       ts_rank_cd(vt.search, to_tsquery('simple', sqlc.arg(tsquery))) AS rank
FROM video_transcripts vt
JOIN videos v ON v.id = vt.video_id
WHERE sqlc.arg(tsquery)::text <> ''
  AND vt.search @@ to_tsquery('simple', sqlc.arg(tsquery))
ORDER BY rank DESC
LIMIT sqlc.arg(page_limit);
