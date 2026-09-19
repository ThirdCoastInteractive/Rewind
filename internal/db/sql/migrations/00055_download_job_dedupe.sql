-- +goose Up
ALTER TABLE download_jobs ADD COLUMN dedupe_key TEXT;

CREATE UNIQUE INDEX download_jobs_active_dedupe_idx
ON download_jobs (dedupe_key)
WHERE dedupe_key IS NOT NULL
  AND status IN ('queued', 'processing');

-- Turn download/ingest progress for referenced jobs into room events. The
-- browser receives these over SSE and performs a single materialization check
-- when video_id becomes available; no client polling loop is required.
-- +goose StatementBegin
CREATE FUNCTION notify_show_note_reference_download_updated() RETURNS TRIGGER AS $$
BEGIN
    IF OLD.status IS DISTINCT FROM NEW.status OR OLD.video_id IS DISTINCT FROM NEW.video_id THEN
        INSERT INTO show_note_room_events (show_note_id, event_type, actor_kind, actor_name, payload)
        SELECT
            ref.show_note_id,
            'reference_download_updated',
            'system',
            'Rewind',
            jsonb_build_object(
                'reference_id', ref.id,
                'download_job_id', NEW.id,
                'status', NEW.status,
                'video_id', NEW.video_id
            )
        FROM show_note_references ref
        WHERE ref.download_job_id = NEW.id;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER show_note_reference_download_updated
AFTER UPDATE OF status, video_id ON download_jobs
FOR EACH ROW EXECUTE FUNCTION notify_show_note_reference_download_updated();

-- +goose Down
DROP TRIGGER IF EXISTS show_note_reference_download_updated ON download_jobs;
DROP FUNCTION IF EXISTS notify_show_note_reference_download_updated();
DROP INDEX IF EXISTS download_jobs_active_dedupe_idx;
ALTER TABLE download_jobs DROP COLUMN IF EXISTS dedupe_key;
