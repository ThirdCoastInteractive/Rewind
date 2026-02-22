-- SearchVideos searches title/description/tags + comments + transcript.
-- name: SearchVideos :many
WITH q AS (
  SELECT plainto_tsquery('simple', sqlc.arg(query)) AS tsq
),
video_hits AS (
  SELECT v.id AS video_id, ts_rank_cd(v.search, q.tsq) AS rank
  FROM videos v, q
  WHERE v.search @@ q.tsq
),
comment_hits AS (
  SELECT vc.video_id AS video_id, max(ts_rank_cd(vc.search, q.tsq)) AS rank
  FROM video_comments vc, q
  JOIN videos v ON v.id = vc.video_id
  WHERE vc.search @@ q.tsq
  GROUP BY vc.video_id
),
transcript_hits AS (
  SELECT vt.video_id AS video_id, max(ts_rank_cd(vt.search, q.tsq)) AS rank
  FROM video_transcripts vt, q
  JOIN videos v ON v.id = vt.video_id
  WHERE vt.search @@ q.tsq
  GROUP BY vt.video_id
),
hits AS (
  SELECT * FROM video_hits
  UNION ALL
  SELECT * FROM comment_hits
  UNION ALL
  SELECT * FROM transcript_hits
),
ranked AS (
  SELECT video_id, sum(rank) AS rank
  FROM hits
  GROUP BY video_id
)
SELECT v.*
FROM ranked r
JOIN videos v ON v.id = r.video_id
ORDER BY r.rank DESC, v.created_at DESC
LIMIT sqlc.arg(page_limit)
OFFSET sqlc.arg(page_offset);
