-- +goose Up
-- YouTube chapter markers are not editorial demand. Keep clips, playback, and
-- recently archived files in the context queue; park the rest.
UPDATE ml_jobs j
SET status = 'paused',
    priority = GREATEST(j.priority, 200),
    locked_at = NULL,
    locked_by = '',
    updated_at = NOW()
WHERE j.kind = 'context_windows'
  AND j.status IN ('queued', 'waiting_model', 'waiting_assets', 'retry_wait')
  AND j.priority > 100
  AND NOT EXISTS (SELECT 1 FROM clips c WHERE c.video_id = j.video_id)
  AND NOT EXISTS (SELECT 1 FROM playback_positions p WHERE p.video_id = j.video_id)
  AND NOT EXISTS (
      SELECT 1 FROM videos v
      WHERE v.id = j.video_id
        AND v.media = 'file'
        AND v.created_at > NOW() - INTERVAL '7 days'
  );

-- +goose Down
SELECT 1;
