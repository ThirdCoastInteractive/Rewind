-- +goose Up
ALTER TABLE runtime_settings_consumers
    ADD COLUMN IF NOT EXISTS hostname text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS stopped_at timestamptz;

-- Collapse web/<container> rows from previous deploys onto one row per role.
DELETE FROM runtime_settings_consumers a
    USING runtime_settings_consumers b
WHERE a.service LIKE '%/%'
  AND b.service LIKE '%/%'
  AND split_part(a.service, '/', 1) = split_part(b.service, '/', 1)
  AND a.updated_at < b.updated_at;

UPDATE runtime_settings_consumers
SET hostname = split_part(service, '/', 2),
    service = split_part(service, '/', 1)
WHERE service LIKE '%/%'
  AND NOT EXISTS (
      SELECT 1 FROM runtime_settings_consumers other
      WHERE other.service = split_part(runtime_settings_consumers.service, '/', 1)
  );

DELETE FROM runtime_settings_consumers WHERE service LIKE '%/%';

-- Statement-level UPDATE notifies even when zero rows change, which spun the
-- compilation coordinator (WakePending → notify → WakePending) millions of times.
DROP TRIGGER IF EXISTS compilation_execution_notify ON compilation_executions;
CREATE TRIGGER compilation_execution_notify
    AFTER INSERT OR UPDATE ON compilation_executions
    FOR EACH ROW EXECUTE FUNCTION notify_compilation_change();

CREATE INDEX IF NOT EXISTS channel_edges_unresolved_idx
    ON channel_edges (to_url)
    WHERE to_channel_id IS NULL;

-- +goose Down
DROP INDEX IF EXISTS channel_edges_unresolved_idx;
DROP TRIGGER IF EXISTS compilation_execution_notify ON compilation_executions;
CREATE TRIGGER compilation_execution_notify
    AFTER INSERT OR UPDATE ON compilation_executions
    FOR EACH STATEMENT EXECUTE FUNCTION notify_compilation_change();
ALTER TABLE runtime_settings_consumers
    DROP COLUMN IF EXISTS hostname,
    DROP COLUMN IF EXISTS stopped_at;
