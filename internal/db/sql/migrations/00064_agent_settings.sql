-- +goose Up
CREATE TABLE runtime_settings (
 key text PRIMARY KEY, value jsonb NOT NULL, revision bigint NOT NULL DEFAULT 1,
 source text NOT NULL DEFAULT 'user', updated_by uuid REFERENCES users(id), updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE runtime_settings_consumers (
 service text PRIMARY KEY, snapshot jsonb NOT NULL, updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE user_interface_preferences (
 user_id uuid PRIMARY KEY REFERENCES users(id), preferences jsonb NOT NULL DEFAULT '{}', updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE job_configuration (
 kind text NOT NULL, job_id uuid NOT NULL, snapshot jsonb NOT NULL, created_at timestamptz NOT NULL DEFAULT now(), PRIMARY KEY(kind,job_id)
);
CREATE TABLE agent_conversations (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(), user_id uuid NOT NULL REFERENCES users(id),
 title text NOT NULL, created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE agent_runs (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(), conversation_id uuid NOT NULL REFERENCES agent_conversations(id),
 user_id uuid NOT NULL REFERENCES users(id), status text NOT NULL DEFAULT 'queued',
 runtime text NOT NULL DEFAULT 'local', provider_session_id text NOT NULL DEFAULT '', provider_run_id text NOT NULL DEFAULT '',
 pending_request jsonb, artifacts jsonb NOT NULL DEFAULT '[]', cancellation_acknowledged boolean NOT NULL DEFAULT false,
 messages jsonb NOT NULL DEFAULT '[]', settings jsonb NOT NULL DEFAULT '{}', model_digest text NOT NULL DEFAULT '',
 calls integer NOT NULL DEFAULT 0, cancel_requested boolean NOT NULL DEFAULT false,
 lease_owner text NOT NULL DEFAULT '', lease_until timestamptz, last_error text NOT NULL DEFAULT '',
 created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX agent_one_active ON agent_runs(conversation_id) WHERE status IN ('queued','running','waiting_capacity','waiting_input','waiting_approval');
CREATE TABLE agent_events (
 id bigserial PRIMARY KEY, run_id uuid NOT NULL REFERENCES agent_runs(id), kind text NOT NULL,
 data jsonb NOT NULL, created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX agent_events_run ON agent_events(run_id,id);
CREATE TABLE agent_tool_calls (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(), run_id uuid NOT NULL REFERENCES agent_runs(id),
 call_index integer NOT NULL, name text NOT NULL, arguments jsonb NOT NULL, result jsonb,
 status text NOT NULL DEFAULT 'started', created_at timestamptz NOT NULL DEFAULT now(), UNIQUE(run_id,call_index)
);
CREATE TABLE model_operations (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(), user_id uuid NOT NULL REFERENCES users(id),
 runtime text NOT NULL, model text NOT NULL, action text NOT NULL, status text NOT NULL DEFAULT 'queued',
 progress jsonb NOT NULL DEFAULT '{}', options jsonb NOT NULL DEFAULT '{}',
 created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX model_operation_active ON model_operations(runtime,model) WHERE status IN ('queued','running');
-- +goose StatementBegin
CREATE FUNCTION notify_runtime_settings() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN PERFORM pg_notify('runtime_settings',''); RETURN NULL; END $$;
-- +goose StatementEnd
CREATE TRIGGER runtime_settings_notify AFTER INSERT OR UPDATE ON runtime_settings FOR EACH STATEMENT EXECUTE FUNCTION notify_runtime_settings();
-- +goose Down
DROP TABLE model_operations, agent_tool_calls, agent_events, agent_runs, agent_conversations, job_configuration, user_interface_preferences, runtime_settings_consumers, runtime_settings;
DROP FUNCTION notify_runtime_settings();
