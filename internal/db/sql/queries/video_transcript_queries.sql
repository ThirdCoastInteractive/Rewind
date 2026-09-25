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
    coverage = NULL,
    updated_at = NOW();

-- GetVideoTranscript returns one transcript for a video (any language).
-- name: GetVideoTranscript :one
SELECT * FROM video_transcripts
WHERE video_id = sqlc.arg(video_id)
ORDER BY CASE WHEN lang::text = 'en' THEN 0 ELSE 1 END, lang
LIMIT 1;

-- SearchTranscripts finds videos whose cleaned transcript matches tsquery.
-- Optional uploader restricts to one channel.
-- name: SearchTranscripts :many
SELECT vt.video_id, v.title, v.uploader, v.media, v.duration_seconds, vt.lang,
       ts_rank_cd(vt.search, to_tsquery('simple', sqlc.arg(tsquery))) AS rank,
       COUNT(*) OVER() AS total_video_count
FROM video_transcripts vt
JOIN videos v ON v.id = vt.video_id
LEFT JOIN channels ch ON ch.id = v.channel_row_id
WHERE sqlc.arg(tsquery)::text <> ''
  AND vt.search @@ to_tsquery('simple', sqlc.arg(tsquery))
  AND (sqlc.narg('video_id')::uuid IS NULL OR v.id = sqlc.narg('video_id'))
  AND (sqlc.narg('uploader')::text IS NULL OR v.uploader = sqlc.narg('uploader'))
  AND (sqlc.narg('creator_id')::uuid IS NULL OR ch.creator_id = sqlc.narg('creator_id'))
  AND (sqlc.narg('channel_id')::uuid IS NULL OR ch.id = sqlc.narg('channel_id'))
  AND (sqlc.narg('tenant_id')::uuid IS NULL OR v.tenant_id = sqlc.narg('tenant_id'))
ORDER BY rank DESC,vt.video_id,vt.lang
LIMIT sqlc.arg(page_limit) OFFSET sqlc.arg(page_offset);

-- name: GetVideoTranscriptsBatch :many
SELECT DISTINCT ON(video_id) * FROM video_transcripts WHERE video_id=ANY(sqlc.arg(video_ids)::uuid[])
ORDER BY video_id,CASE WHEN lang::text='en' THEN 0 ELSE 1 END,lang;

-- name: GetVideoTranscriptByLanguage :one
SELECT * FROM video_transcripts WHERE video_id = sqlc.arg(video_id) AND lang = sqlc.arg(lang);

-- name: GetLibraryEvidence :many
SELECT v.id,
COALESCE((SELECT t.cues FROM video_transcripts t WHERE t.video_id=v.id AND t.search @@ to_tsquery('simple',sqlc.arg(tsquery)) ORDER BY CASE WHEN t.lang::text='en' THEN 0 ELSE 1 END,t.lang LIMIT 1),'[]'::jsonb)::jsonb AS cues,
COALESCE((SELECT c.text FROM video_comments c WHERE c.video_id=v.id AND c.search @@ to_tsquery('simple',sqlc.arg(tsquery)) ORDER BY c.id LIMIT 1),'')::text AS comment_text
FROM videos v WHERE v.id=ANY(sqlc.arg(video_ids)::uuid[]);
