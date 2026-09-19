-- +goose Up
CREATE TABLE agent_delegated_messages (
 user_id uuid NOT NULL REFERENCES users(id),
 message_id text NOT NULL,
 request_hash text NOT NULL,
 run_id uuid NOT NULL REFERENCES agent_runs(id),
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(user_id,message_id)
);
-- +goose Down
DROP TABLE agent_delegated_messages;
