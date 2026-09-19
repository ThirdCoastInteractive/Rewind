-- +goose Up
CREATE TABLE show_note_materializations (
 thread_id uuid PRIMARY KEY REFERENCES show_note_review_threads(id),
 show_note_id uuid NOT NULL REFERENCES show_notes(id),
 user_id uuid NOT NULL REFERENCES users(id),
 accepted_revision bigint NOT NULL,
 base_markdown text NOT NULL,
 proposed_markdown text NOT NULL,
 status text NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','complete','failed')),
 lease_token uuid,
 retry_at timestamptz NOT NULL DEFAULT now(),
 last_error text NOT NULL DEFAULT '',
 attempts integer NOT NULL DEFAULT 0
);

-- +goose Down
DROP TABLE show_note_materializations;
