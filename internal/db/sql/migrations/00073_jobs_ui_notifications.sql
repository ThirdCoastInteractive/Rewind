-- +goose Up
-- UI invalidation is separate from worker wakeups. PostgreSQL coalesces identical
-- notifications within a transaction, including batch actions. Row triggers avoid
-- waking the UI for empty worker claim statements and unchanged rows.
-- +goose StatementBegin
CREATE FUNCTION notify_jobs_ui() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'UPDATE' THEN
        IF NEW IS NOT DISTINCT FROM OLD THEN
            RETURN NULL;
        END IF;
    END IF;
    PERFORM pg_notify('jobs_ui', '');
    RETURN NULL;
END $$;
-- +goose StatementEnd
CREATE TRIGGER download_jobs_ui_notify AFTER INSERT OR UPDATE OR DELETE ON download_jobs
FOR EACH ROW EXECUTE FUNCTION notify_jobs_ui();
CREATE TRIGGER ml_jobs_ui_notify AFTER INSERT OR UPDATE OR DELETE ON ml_jobs
FOR EACH ROW EXECUTE FUNCTION notify_jobs_ui();
CREATE TRIGGER ml_runtime_health_ui_notify AFTER INSERT OR UPDATE OR DELETE ON ml_runtime_health
FOR EACH ROW EXECUTE FUNCTION notify_jobs_ui();

-- +goose Down
DROP TRIGGER IF EXISTS ml_runtime_health_ui_notify ON ml_runtime_health;
DROP TRIGGER IF EXISTS ml_jobs_ui_notify ON ml_jobs;
DROP TRIGGER IF EXISTS download_jobs_ui_notify ON download_jobs;
DROP FUNCTION IF EXISTS notify_jobs_ui();
