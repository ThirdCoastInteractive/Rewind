-- +goose Up
-- A failed archive must release its show-note reference so the inline action
-- can be tried again. Keep the job and its logs for audit; only detach it from
-- the transient resolving state.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION notify_show_note_reference_download_updated() RETURNS TRIGGER AS $$
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

        IF NEW.status = 'failed' THEN
            UPDATE show_note_references
            SET status = 'unresolved',
                download_job_id = NULL,
                diagnostic = COALESCE(NULLIF(NEW.last_error, ''), 'Download failed'),
                updated_at = NOW()
            WHERE download_job_id = NEW.id;
        END IF;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

UPDATE show_note_references ref
SET status = 'unresolved',
    download_job_id = NULL,
    diagnostic = COALESCE(NULLIF(job.last_error, ''), 'Download failed'),
    updated_at = NOW()
FROM download_jobs job
WHERE ref.download_job_id = job.id
  AND job.status = 'failed';

-- +goose Down
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION notify_show_note_reference_download_updated() RETURNS TRIGGER AS $$
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
