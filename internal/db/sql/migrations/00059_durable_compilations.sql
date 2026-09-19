-- +goose Up
CREATE TABLE compilation_executions (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 plan_id uuid NOT NULL REFERENCES compilation_plans(id),
 revision integer NOT NULL,
 created_by uuid NOT NULL REFERENCES users(id),
 title text NOT NULL,
 status text NOT NULL DEFAULT 'waiting_media' CHECK(status IN ('waiting_media','rendering','complete','failed')),
 stitch_project_id uuid REFERENCES stitch_projects(id),
 stitch_job_id uuid REFERENCES stitch_jobs(id),
 last_error text NOT NULL DEFAULT '',
 next_check timestamptz NOT NULL DEFAULT now(),
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(plan_id,revision)
);
CREATE TABLE compilation_execution_segments (
 execution_id uuid NOT NULL REFERENCES compilation_executions(id),
 position integer NOT NULL,
 video_id uuid NOT NULL REFERENCES videos(id),
 start_ts double precision NOT NULL,
 end_ts double precision NOT NULL,
 evidence jsonb NOT NULL,
 rationale text NOT NULL,
 download_job_id uuid REFERENCES download_jobs(id),
 PRIMARY KEY(execution_id,position)
);
CREATE INDEX compilation_executions_pending ON compilation_executions(next_check) WHERE status IN ('waiting_media','rendering');
-- +goose StatementBegin
CREATE FUNCTION notify_compilation_change() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN PERFORM pg_notify('compilation_changed',''); RETURN NULL; END $$;
-- +goose StatementEnd
CREATE TRIGGER compilation_execution_notify AFTER INSERT OR UPDATE ON compilation_executions FOR EACH STATEMENT EXECUTE FUNCTION notify_compilation_change();

-- +goose Down
DROP TABLE compilation_execution_segments;
DROP TABLE compilation_executions;
DROP FUNCTION notify_compilation_change();
