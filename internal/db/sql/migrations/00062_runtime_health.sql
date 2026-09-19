-- +goose Up
CREATE TABLE ml_runtime_health (
 kind text PRIMARY KEY, failures integer NOT NULL DEFAULT 0,
 retry_at timestamptz NOT NULL DEFAULT now(), last_error text NOT NULL DEFAULT '',
 verified_at timestamptz, updated_at timestamptz NOT NULL DEFAULT now()
);
-- +goose Down
DROP TABLE ml_runtime_health;
