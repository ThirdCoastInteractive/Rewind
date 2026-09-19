-- ListDownloadJobsByUser returns all download jobs for a user
-- name: ListDownloadJobsByUser :many
SELECT *
FROM download_jobs
WHERE archived_by = sqlc.arg(archived_by)
  AND archived = FALSE
ORDER BY created_at DESC
LIMIT sqlc.arg(page_limit);

-- ListRecentDownloadJobs returns recent download jobs for all users
-- name: ListRecentDownloadJobs :many
SELECT *
FROM download_jobs
WHERE archived = FALSE
ORDER BY created_at DESC
LIMIT 100;

-- GetDownloadJobByID returns a download job by ID
-- name: GetDownloadJobByID :one
SELECT *
FROM download_jobs
WHERE id = sqlc.arg(id);

-- ListVideosPaginated returns videos with filters, sorting, and pagination.
-- Returns total_count via window function for pagination UI.
-- name: ListVideosPaginated :many
WITH params AS (
    SELECT
        NULLIF(btrim(COALESCE(sqlc.narg('tsquery')::text, '')), '') AS tsq,
        NULLIF(btrim(COALESCE(sqlc.narg('query')::text, '')), '') AS raw
),
field_clauses AS (
    SELECT value->>'field' AS field, value->>'text' AS phrase, row_number() OVER () AS ordinality
    FROM jsonb_array_elements(COALESCE(sqlc.narg('field_clauses')::jsonb, '[]'::jsonb))
),
field_hits AS (
    SELECT fc.ordinality, fv.id AS video_id
    FROM field_clauses fc JOIN videos fv ON to_tsvector('simple', COALESCE(fv.title, '')) @@ phraseto_tsquery('simple', fc.phrase)
    WHERE fc.field = 'title'
    UNION ALL
    SELECT fc.ordinality, fv.id AS video_id
    FROM field_clauses fc JOIN videos fv ON fc.phrase <> '' AND strpos(lower(fv.src), lower(fc.phrase)) > 0
    WHERE fc.field = 'url'
    UNION ALL
    SELECT fc.ordinality, fv.id AS video_id
    FROM field_clauses fc JOIN videos fv ON to_tsvector('simple', COALESCE(fv.description, '')) @@ phraseto_tsquery('simple', fc.phrase)
    WHERE fc.field = 'description'
    UNION ALL
    SELECT fc.ordinality, fv.id AS video_id
    FROM field_clauses fc JOIN videos fv ON to_tsvector('simple', COALESCE(fv.uploader, '')) @@ phraseto_tsquery('simple', fc.phrase)
    WHERE fc.field = 'uploader'
    UNION ALL
    SELECT fc.ordinality, ft.video_id AS video_id
    FROM field_clauses fc JOIN video_transcripts ft ON ft.search @@ phraseto_tsquery('simple', fc.phrase)
    WHERE fc.field = 'transcript'
    UNION ALL
    SELECT fc.ordinality, fw.video_id AS video_id
    FROM field_clauses fc JOIN context_windows fw ON NOT fw.stale AND fw.search @@ phraseto_tsquery('simple', fc.phrase)
    WHERE fc.field = 'context'
),
field_matches AS (
    SELECT video_id FROM field_hits
    GROUP BY video_id
    HAVING count(DISTINCT ordinality) = (SELECT count(*) FROM field_clauses)
),
hits AS (
    SELECT
        v.id AS video_id,
        -- Title matches boost ×4 using the title tsvector, not whole v.search.
        -- Description/tags/uploader stay ×1.
        ts_rank_cd(v.search, to_tsquery('simple', p.tsq))
            * CASE
                WHEN to_tsvector('simple', coalesce(v.title, '')) @@ to_tsquery('simple', p.tsq) THEN 4
                ELSE 1
              END AS rank,
        to_tsvector('simple', coalesce(v.title, '')) @@ to_tsquery('simple', p.tsq) AS title_match,
        to_tsvector('simple', coalesce(v.uploader, '')) @@ to_tsquery('simple', p.tsq) AS uploader_match,
        to_tsvector('simple', coalesce(v.description, '')) @@ to_tsquery('simple', p.tsq) AS description_match,
        to_tsvector('simple', coalesce(array_to_string(v.tags, ' '), '')) @@ to_tsquery('simple', p.tsq) AS tags_match,
        FALSE AS comment_match,
        FALSE AS transcript_match,
        FALSE AS context_window_match,
        NULL::text AS snippet,
        NULL::text AS snippet_source,
        3::smallint AS snippet_priority
    FROM videos v
    CROSS JOIN params p
    WHERE p.tsq IS NOT NULL AND v.search @@ to_tsquery('simple', p.tsq)
    UNION ALL
    SELECT
        vc.video_id,
        max(ts_rank_cd(vc.search, to_tsquery('simple', p.tsq))) * 1.5,
        FALSE,
        FALSE,
        FALSE,
        FALSE,
        TRUE,
        FALSE,
        FALSE,
        NULL::text,
        'comment'::text,
        2::smallint
    FROM video_comments vc
    CROSS JOIN params p
    WHERE p.tsq IS NOT NULL AND vc.search @@ to_tsquery('simple', p.tsq)
    GROUP BY vc.video_id
    UNION ALL
    SELECT
        vt.video_id,
        max(ts_rank_cd(vt.search, to_tsquery('simple', p.tsq))) * 3,
        FALSE,
        FALSE,
        FALSE,
        FALSE,
        FALSE,
        TRUE,
        FALSE,
        NULL::text,
        'transcript'::text,
        1::smallint
    FROM video_transcripts vt
    CROSS JOIN params p
    WHERE p.tsq IS NOT NULL AND vt.search @@ to_tsquery('simple', p.tsq)
    GROUP BY vt.video_id
    UNION ALL
    SELECT
        cw.video_id,
        max(ts_rank_cd(cw.search, to_tsquery('simple', p.tsq))) * 2,
        FALSE,
        FALSE,
        FALSE,
        FALSE,
        FALSE,
        FALSE,
        TRUE,
        NULL::text,
        'context_window'::text,
        2::smallint
    FROM context_windows cw
    CROSS JOIN params p
    WHERE p.tsq IS NOT NULL AND NOT cw.stale AND cw.search @@ to_tsquery('simple', p.tsq)
    GROUP BY cw.video_id
),
ranked AS (
    SELECT
        video_id,
        sum(rank) AS rank,
        bool_or(title_match) AS search_match_title,
        bool_or(uploader_match) AS search_match_uploader,
        bool_or(description_match) AS search_match_description,
        bool_or(tags_match) AS search_match_tags,
        bool_or(comment_match) AS search_match_comment,
        bool_or(transcript_match) AS search_match_transcript,
        bool_or(context_window_match) AS search_match_context_window,
        (array_agg(snippet ORDER BY snippet_priority, rank DESC) FILTER (WHERE snippet IS NOT NULL AND snippet <> ''))[1] AS search_match_snippet,
        (array_agg(snippet_source ORDER BY snippet_priority, rank DESC) FILTER (WHERE snippet IS NOT NULL AND snippet <> ''))[1] AS search_match_snippet_source
    FROM hits
    GROUP BY video_id
)
SELECT
    v.id,
    v.created_at,
    v.title,
    v.uploader,
    v.description,
    v.tags,
    v.media,
    v.format,
    v.upload_date,
    v.duration_seconds,
    v.view_count,
    v.like_count,
    v.comment_count,
    v.thumb_gradient_start,
    v.thumb_gradient_end,
    v.thumb_gradient_angle,
    COUNT(*) OVER() AS total_count,
    COALESCE((SELECT COUNT(*) FROM clips c WHERE c.video_id = v.id), 0) AS clip_count,
    COALESCE((SELECT COUNT(*) FROM markers m WHERE m.video_id = v.id), 0) AS marker_count,
    COALESCE((SELECT MAX(c.created_at) FROM clips c WHERE c.video_id = v.id), '1970-01-01'::timestamptz) AS last_clip_at,
    COALESCE((SELECT MAX(m.created_at) FROM markers m WHERE m.video_id = v.id), '1970-01-01'::timestamptz) AS last_marker_at,
    COALESCE(u.user_name, 'unknown') AS archived_by_username,
    COALESCE(r.search_match_title, FALSE) AS search_match_title,
    COALESCE(r.search_match_uploader, FALSE) AS search_match_uploader,
    COALESCE(r.search_match_description, FALSE) AS search_match_description,
    COALESCE(r.search_match_tags, FALSE) AS search_match_tags,
    COALESCE(r.search_match_comment, FALSE) AS search_match_comment,
    COALESCE(r.search_match_transcript, FALSE) AS search_match_transcript,
    COALESCE(r.search_match_context_window, FALSE) AS search_match_context_window,
    COALESCE(CASE
        -- Expensive headline extraction is deliberately deferred to the rows
        -- that survive ORDER BY/LIMIT instead of running for every library hit.
        WHEN r.search_match_transcript THEN (
            SELECT regexp_replace(regexp_replace(ts_headline('simple', vt.text, to_tsquery('simple', p.tsq), 'MaxWords=24, MinWords=10, MaxFragments=1'), '</?b>', '', 'g'), '\s+', ' ', 'g')
            FROM video_transcripts vt
            WHERE vt.video_id = v.id AND vt.search @@ to_tsquery('simple', p.tsq)
            ORDER BY ts_rank_cd(vt.search, to_tsquery('simple', p.tsq)) DESC
            LIMIT 1
        )
        WHEN r.search_match_context_window THEN (
            SELECT regexp_replace(regexp_replace(ts_headline('simple', concat_ws(': ', cw.title, cw.summary), to_tsquery('simple', p.tsq), 'MaxWords=24, MinWords=10, MaxFragments=1'), '</?b>', '', 'g'), '\s+', ' ', 'g')
            FROM context_windows cw
            WHERE cw.video_id = v.id AND NOT cw.stale AND cw.search @@ to_tsquery('simple', p.tsq)
            ORDER BY ts_rank_cd(cw.search, to_tsquery('simple', p.tsq)) DESC
            LIMIT 1
        )
        WHEN r.search_match_comment THEN (
            SELECT regexp_replace(regexp_replace(ts_headline('simple', concat_ws(': ', vc.author, vc.text), to_tsquery('simple', p.tsq), 'MaxWords=24, MinWords=10, MaxFragments=1'), '</?b>', '', 'g'), '\s+', ' ', 'g')
            FROM video_comments vc
            WHERE vc.video_id = v.id AND vc.search @@ to_tsquery('simple', p.tsq)
            ORDER BY ts_rank_cd(vc.search, to_tsquery('simple', p.tsq)) DESC
            LIMIT 1
        )
        WHEN r.search_match_description THEN regexp_replace(regexp_replace(ts_headline('simple', v.description, to_tsquery('simple', p.tsq), 'MaxWords=24, MinWords=10, MaxFragments=1'), '</?b>', '', 'g'), '\s+', ' ', 'g')
        WHEN r.search_match_tags THEN array_to_string(v.tags, ', ')
        WHEN r.search_match_title THEN v.title
        WHEN r.search_match_uploader THEN v.uploader
    END, '')::text AS search_match_snippet,
    COALESCE(CASE
        WHEN r.search_match_transcript THEN 'transcript'
        WHEN r.search_match_context_window THEN 'context_window'
        WHEN r.search_match_comment THEN 'comment'
        WHEN r.search_match_description THEN 'description'
        WHEN r.search_match_tags THEN 'tags'
        WHEN r.search_match_title THEN 'title'
        WHEN r.search_match_uploader THEN 'uploader'
    END, '')::text AS search_match_snippet_source
FROM videos v
LEFT JOIN users u ON v.archived_by = u.id
LEFT JOIN ranked r ON r.video_id = v.id
CROSS JOIN params p
WHERE
    -- Metadata-only catalog entries are not playable videos and never belong in card feeds.
    v.media <> 'metadata'
    -- Full-text / substring search (optional).
    AND (p.raw IS NULL OR r.video_id IS NOT NULL)
    AND (COALESCE(cardinality(sqlc.narg('sources')::text[]),0)=0
      OR ('title'=ANY(sqlc.narg('sources')::text[]) AND r.search_match_title)
      OR ('description'=ANY(sqlc.narg('sources')::text[]) AND r.search_match_description)
      OR ('tags'=ANY(sqlc.narg('sources')::text[]) AND r.search_match_tags)
      OR ('comments'=ANY(sqlc.narg('sources')::text[]) AND r.search_match_comment)
      OR ('transcript'=ANY(sqlc.narg('sources')::text[]) AND r.search_match_transcript)
      OR ('context_windows'=ANY(sqlc.narg('sources')::text[]) AND r.search_match_context_window)
      OR ('uploader'=ANY(sqlc.narg('sources')::text[]) AND r.search_match_uploader))
    -- Resolve each scope once, then intersect by video (all scopes must match).
    AND (COALESCE(jsonb_array_length(sqlc.narg('field_clauses')::jsonb), 0) = 0
         OR v.id IN (SELECT video_id FROM field_matches))
    -- Require completed assets. Keep predicates separate so unused filters
    -- disappear from the plan instead of adding per-video conditional subplans.
    AND (NOT COALESCE('context' = ANY(sqlc.narg('required_assets')::text[]), FALSE) OR EXISTS (SELECT 1 FROM context_windows ac WHERE ac.video_id = v.id AND NOT ac.stale))
    AND (NOT COALESCE('transcript' = ANY(sqlc.narg('required_assets')::text[]), FALSE) OR EXISTS (SELECT 1 FROM video_transcripts atx WHERE atx.video_id = v.id AND btrim(atx.text) <> ''))
    AND (NOT COALESCE('thumbnail' = ANY(sqlc.narg('required_assets')::text[]), FALSE) OR v.assets_status @> '{"thumbnail":true}'::jsonb)
    AND (NOT COALESCE('preview' = ANY(sqlc.narg('required_assets')::text[]), FALSE) OR v.assets_status @> '{"preview":true}'::jsonb)
    AND (NOT COALESCE('waveform' = ANY(sqlc.narg('required_assets')::text[]), FALSE) OR v.assets_status @> '{"waveform":true}'::jsonb)
    AND (NOT COALESCE('seek' = ANY(sqlc.narg('required_assets')::text[]), FALSE) OR v.assets_status @> '{"seek":true}'::jsonb)
    AND (sqlc.narg('required_assets')::text[] IS NULL OR sqlc.narg('required_assets')::text[] <@ ARRAY['context','transcript','thumbnail','preview','waveform','seek'])
    -- Uploader combobox filter: indexed, case-insensitive prefix; optionally negate it.
    AND (
        sqlc.narg('uploader')::text IS NULL
        OR (
            COALESCE(sqlc.narg('uploader_excluded')::boolean, FALSE) = FALSE
            AND lower(v.uploader) LIKE lower(sqlc.narg('uploader')) || '%'
        )
        OR (
            COALESCE(sqlc.narg('uploader_excluded')::boolean, FALSE) = TRUE
            AND lower(v.uploader) NOT LIKE lower(sqlc.narg('uploader')) || '%'
        )
    )
	-- Channel filter (optional)
	AND (sqlc.narg('channel_id')::text IS NULL OR v.channel_id = sqlc.narg('channel_id'))
    -- First-class creator/channel filters.
    AND (sqlc.narg('creator_id')::uuid IS NULL OR EXISTS (
        SELECT 1 FROM channels ch WHERE ch.id = v.channel_row_id AND ch.creator_id = sqlc.narg('creator_id')
    ))
    AND (sqlc.narg('channel_row_id')::uuid IS NULL OR v.channel_row_id = sqlc.narg('channel_row_id'))
    -- Duration filter: short=<5min, medium=5-30min, long=>30min
    AND (
        sqlc.narg('duration_filter')::text IS NULL
        OR (sqlc.narg('duration_filter') = 'short' AND v.duration_seconds < 300)
        OR (sqlc.narg('duration_filter') = 'medium' AND v.duration_seconds >= 300 AND v.duration_seconds < 1800)
        OR (sqlc.narg('duration_filter') = 'long' AND v.duration_seconds >= 1800)
    )
    -- Scraped tags filter (any tag matches)
    AND (sqlc.narg('tags')::text[] IS NULL OR v.tags && sqlc.narg('tags')::text[])
    -- User tag filter (video has any of the selected tag ids)
    AND (sqlc.narg('tag_ids')::uuid[] IS NULL OR EXISTS (
        SELECT 1 FROM video_tags vt
        WHERE vt.video_id = v.id AND vt.tag_id = ANY(sqlc.narg('tag_ids')::uuid[])
    ))
    -- Date range (archived or published based on date_type)
    AND (
        sqlc.narg('date_from')::date IS NULL
        OR (sqlc.narg('date_type')::text = 'published' AND v.upload_date >= sqlc.narg('date_from'))
        OR (sqlc.narg('date_type')::text IS DISTINCT FROM 'published' AND v.created_at::date >= sqlc.narg('date_from'))
    )
    AND (
        sqlc.narg('date_to')::date IS NULL
        OR (sqlc.narg('date_type')::text = 'published' AND v.upload_date <= sqlc.narg('date_to'))
        OR (sqlc.narg('date_type')::text IS DISTINCT FROM 'published' AND v.created_at::date <= sqlc.narg('date_to'))
    )
    -- Has clips filter
    AND (sqlc.narg('has_clips')::boolean IS NULL OR sqlc.narg('has_clips') = FALSE
         OR EXISTS (SELECT 1 FROM clips c WHERE c.video_id = v.id))
    -- Has markers filter
    AND (sqlc.narg('has_markers')::boolean IS NULL OR sqlc.narg('has_markers') = FALSE
         OR EXISTS (SELECT 1 FROM markers m WHERE m.video_id = v.id))
ORDER BY
    CASE WHEN sqlc.arg(sort_order) = 'relevance' THEN r.rank END DESC NULLS LAST,
    -- Date sorts (archived)
    CASE WHEN sqlc.arg(sort_order) = 'newest' THEN v.created_at END DESC NULLS LAST,
    CASE WHEN sqlc.arg(sort_order) = 'oldest' THEN v.created_at END ASC NULLS LAST,
    -- Date sorts (published)
    CASE WHEN sqlc.arg(sort_order) = 'published-newest' THEN v.upload_date END DESC NULLS LAST,
    CASE WHEN sqlc.arg(sort_order) = 'published-oldest' THEN v.upload_date END ASC NULLS LAST,
    -- Title sorts
    CASE WHEN sqlc.arg(sort_order) = 'alpha' THEN v.title END ASC NULLS LAST,
    CASE WHEN sqlc.arg(sort_order) = 'alpha-desc' THEN v.title END DESC NULLS LAST,
    -- Duration sorts
    CASE WHEN sqlc.arg(sort_order) = 'duration' THEN v.duration_seconds END ASC NULLS LAST,
    CASE WHEN sqlc.arg(sort_order) = 'duration-desc' THEN v.duration_seconds END DESC NULLS LAST,
    -- Activity sorts
    CASE WHEN sqlc.arg(sort_order) = 'most-clips' THEN (SELECT COUNT(*) FROM clips c WHERE c.video_id = v.id) END DESC NULLS LAST,
    CASE WHEN sqlc.arg(sort_order) = 'most-markers' THEN (SELECT COUNT(*) FROM markers m WHERE m.video_id = v.id) END DESC NULLS LAST,
    CASE WHEN sqlc.arg(sort_order) = 'recently-clipped' THEN (SELECT MAX(c.created_at) FROM clips c WHERE c.video_id = v.id) END DESC NULLS LAST,
    CASE WHEN sqlc.arg(sort_order) = 'recently-marked' THEN (SELECT MAX(m.created_at) FROM markers m WHERE m.video_id = v.id) END DESC NULLS LAST,
    -- Default fallback
    v.created_at DESC
LIMIT sqlc.arg(page_limit)
OFFSET sqlc.arg(page_offset);

-- ListCatalogCandidates returns matching metadata-only rows separately from
-- playable video cards.
-- name: ListCatalogCandidates :many
WITH params AS (
    SELECT NULLIF(btrim(COALESCE(sqlc.narg('tsquery')::text, '')), '') AS tsq
), hits AS (
    SELECT v.id AS video_id,
           COALESCE((v.search @@ to_tsquery('simple', p.tsq)), FALSE)::boolean AS metadata_match,
           EXISTS (SELECT 1 FROM video_comments c WHERE c.video_id = v.id AND c.search @@ to_tsquery('simple', p.tsq)) AS comment_match,
           EXISTS (SELECT 1 FROM video_transcripts t WHERE t.video_id = v.id AND t.search @@ to_tsquery('simple', p.tsq)) AS transcript_match,
           EXISTS (SELECT 1 FROM context_windows cw WHERE cw.video_id = v.id AND NOT cw.stale AND cw.search @@ to_tsquery('simple', p.tsq)) AS context_window_match
    FROM videos v CROSS JOIN params p
    WHERE p.tsq IS NOT NULL AND (
        v.search @@ to_tsquery('simple', p.tsq)
        OR EXISTS (SELECT 1 FROM video_comments c WHERE c.video_id = v.id AND c.search @@ to_tsquery('simple', p.tsq))
        OR EXISTS (SELECT 1 FROM video_transcripts t WHERE t.video_id = v.id AND t.search @@ to_tsquery('simple', p.tsq))
        OR EXISTS (SELECT 1 FROM context_windows cw WHERE cw.video_id = v.id AND NOT cw.stale AND cw.search @@ to_tsquery('simple', p.tsq))
    )
)
SELECT v.id, v.title, v.uploader, v.src, v.upload_date, v.duration_seconds,
       v.thumbnail_path, v.thumb_gradient_start, v.thumb_gradient_end, v.thumb_gradient_angle,
       h.metadata_match, h.comment_match, h.transcript_match, h.context_window_match,
       COUNT(*) OVER() AS total_count
FROM videos v JOIN hits h ON h.video_id = v.id
LEFT JOIN channels ch ON ch.id = v.channel_row_id
WHERE v.media = 'metadata'
  AND (sqlc.narg('uploader')::text IS NULL OR v.uploader ILIKE '%' || sqlc.narg('uploader') || '%')
  AND (COALESCE(cardinality(sqlc.narg('sources')::text[]),0)=0
    OR (h.metadata_match AND 'metadata'=ANY(sqlc.narg('sources')::text[]))
      OR ('title'=ANY(sqlc.narg('sources')::text[]) AND to_tsvector('simple',v.title) @@ to_tsquery('simple',sqlc.narg('tsquery')))
      OR ('description'=ANY(sqlc.narg('sources')::text[]) AND to_tsvector('simple',v.description) @@ to_tsquery('simple',sqlc.narg('tsquery')))
      OR ('uploader'=ANY(sqlc.narg('sources')::text[]) AND to_tsvector('simple',v.uploader) @@ to_tsquery('simple',sqlc.narg('tsquery')))
      OR ('tags'=ANY(sqlc.narg('sources')::text[]) AND to_tsvector('simple',array_to_string(v.tags,' ')) @@ to_tsquery('simple',sqlc.narg('tsquery')))
    OR (h.comment_match AND 'comments'=ANY(sqlc.narg('sources')::text[]))
    OR (h.transcript_match AND 'transcript'=ANY(sqlc.narg('sources')::text[]))
    OR (h.context_window_match AND 'context_windows'=ANY(sqlc.narg('sources')::text[])))
  AND (sqlc.narg('creator_id')::uuid IS NULL OR ch.creator_id = sqlc.narg('creator_id'))
  AND (sqlc.narg('channel_row_id')::uuid IS NULL OR ch.id = sqlc.narg('channel_row_id'))
ORDER BY v.upload_date DESC NULLS LAST, v.created_at DESC
LIMIT sqlc.arg(page_limit);

-- ListDistinctUploaders returns unique uploader names for filter dropdown
-- name: ListDistinctUploaders :many
SELECT DISTINCT uploader
FROM videos
WHERE uploader IS NOT NULL AND uploader != ''
ORDER BY uploader ASC
LIMIT 100;

-- name: SearchUploaders :many
SELECT uploader, COUNT(*)::bigint AS video_count
FROM videos
WHERE media <> 'metadata'
  AND uploader <> ''
  AND (
      NULLIF(btrim(sqlc.arg('prefix')::text), '') IS NULL
      OR lower(uploader) LIKE lower(btrim(sqlc.arg('prefix')::text)) || '%'
  )
GROUP BY uploader
ORDER BY COUNT(*) DESC, uploader ASC
LIMIT 50;

-- ListDistinctTags returns unique tags for filter dropdown
-- name: ListDistinctTags :many
SELECT DISTINCT unnest(tags) AS tag
FROM videos
WHERE tags IS NOT NULL AND array_length(tags, 1) > 0
ORDER BY tag ASC
LIMIT 200;

-- ListRecentVideos returns recent videos (by archive date)
-- name: ListRecentVideos :many
SELECT *
FROM videos
WHERE media <> 'metadata'
ORDER BY created_at DESC
LIMIT 15;

-- ListRecentlyPublishedVideos returns videos sorted by original publish date
-- name: ListRecentlyPublishedVideos :many
SELECT *
FROM videos
WHERE media <> 'metadata'
  AND upload_date IS NOT NULL
ORDER BY upload_date DESC
LIMIT 15;

-- GetHomeStats returns aggregate stats for the home page dashboard
-- name: GetHomeStats :one
SELECT
    (SELECT COUNT(*)::bigint FROM videos) AS video_count,
    (SELECT COUNT(*)::bigint FROM clips) AS clip_count,
    (SELECT COUNT(*)::bigint FROM stitch_projects) AS stitch_count,
    (SELECT COALESCE(SUM(file_size), 0)::bigint FROM videos WHERE file_size IS NOT NULL) AS storage_bytes,
    (SELECT COALESCE(SUM(duration_seconds), 0)::bigint FROM videos WHERE duration_seconds IS NOT NULL) AS total_duration_seconds;

-- ListRecentClips returns recently created clips with their source video title
-- name: ListRecentClips :many
SELECT
    c.id,
    c.video_id,
    c.title AS clip_title,
    c.start_ts,
    c.end_ts,
    c.duration,
    c.color,
    c.created_at,
    v.title AS video_title
FROM clips c
JOIN videos v ON v.id = c.video_id
ORDER BY c.created_at DESC
LIMIT 8;

-- GetVideoByID returns a video by ID
-- name: GetVideoByID :one
SELECT *
FROM videos
WHERE id = sqlc.arg(id);

-- ListDownloadJobsByVideoID returns all download jobs for a video.
-- Matches by video_id FK or by URL matching the video's src column.
-- name: ListDownloadJobsByVideoID :many
SELECT *
FROM download_jobs
WHERE video_id = sqlc.arg(video_id)
   OR url = sqlc.arg(video_src)
ORDER BY created_at DESC;

-- ListIngestJobsByDownloadJobIDs returns ingest jobs for a set of download job IDs.
-- name: ListIngestJobsByDownloadJobIDs :many
SELECT *
FROM ingest_jobs
WHERE download_job_id = ANY(sqlc.arg(download_job_ids)::uuid[])
ORDER BY created_at DESC;
