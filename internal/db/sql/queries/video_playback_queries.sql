-- GetVideoWithDownloadJob gets a video with its download job info for playback
-- name: GetVideoWithDownloadJob :one
SELECT 
    v.id as video_id,
    v.src,
    v.title,
    v.info,
    v.comments,
    v.video_path,
    v.thumbnail_path,
    v.created_at as video_created_at,
    v.updated_at as video_updated_at,
    dj.id as download_job_id,
    dj.spool_dir,
    dj.info_json_path
FROM videos v
LEFT JOIN download_jobs dj ON dj.video_id = v.id
WHERE v.id = sqlc.arg(video_id);
