-- +goose Up
-- Stop enqueueing context windows for every transcript write. Catalog imports
-- and YouTube captions were filling the GPU with archive-wide 27B jobs.
DROP TRIGGER IF EXISTS transcript_context_job ON video_transcripts;
DROP FUNCTION IF EXISTS enqueue_transcript_context();

-- Park the historical dump. ClaimMLJob never picks paused rows. In-flight
-- processing jobs are left alone so a long generate is not killed.
UPDATE ml_jobs
SET status = 'paused',
    locked_at = NULL,
    locked_by = '',
    updated_at = NOW()
WHERE kind = 'context_windows'
  AND status IN ('queued', 'waiting_model', 'waiting_assets', 'retry_wait')
  AND priority >= 200;

-- Keep videos people actually use in the queue: clips, markers, playback, and
-- recently archived files (not metadata-only catalog rows).
UPDATE ml_jobs j
SET status = 'queued',
    retry_at = NOW(),
    updated_at = NOW(),
    priority = LEAST(j.priority, CASE
        WHEN EXISTS (SELECT 1 FROM clips c WHERE c.video_id = j.video_id)
          OR EXISTS (SELECT 1 FROM markers m WHERE m.video_id = j.video_id) THEN 120
        WHEN EXISTS (SELECT 1 FROM playback_positions p WHERE p.video_id = j.video_id) THEN 130
        ELSE 160
    END)
WHERE j.kind = 'context_windows'
  AND j.status = 'paused'
  AND EXISTS (SELECT 1 FROM video_transcripts t WHERE t.video_id = j.video_id AND t.text <> '')
  AND (
      EXISTS (SELECT 1 FROM clips c WHERE c.video_id = j.video_id)
      OR EXISTS (SELECT 1 FROM markers m WHERE m.video_id = j.video_id)
      OR EXISTS (SELECT 1 FROM playback_positions p WHERE p.video_id = j.video_id)
      OR EXISTS (
          SELECT 1 FROM videos v
          WHERE v.id = j.video_id
            AND v.media = 'file'
            AND v.created_at > NOW() - INTERVAL '7 days'
      )
  );

-- +goose Down
-- +goose StatementBegin
CREATE FUNCTION enqueue_transcript_context() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='UPDATE' AND NEW.text IS NOT DISTINCT FROM OLD.text AND NEW.cues IS NOT DISTINCT FROM OLD.cues THEN RETURN NEW; END IF;
 INSERT INTO ml_jobs(video_id,kind,priority,transcript_hash,prompt_version)
 VALUES(NEW.video_id,'context_windows',200,transcript_fingerprint(NEW.video_id),'context-v3-bytes-24k') ON CONFLICT DO NOTHING;
 RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER transcript_context_job AFTER INSERT OR UPDATE ON video_transcripts FOR EACH ROW EXECUTE FUNCTION enqueue_transcript_context();
UPDATE ml_jobs
SET status = 'queued',
    retry_at = NOW(),
    updated_at = NOW()
WHERE kind = 'context_windows'
  AND status = 'paused';
