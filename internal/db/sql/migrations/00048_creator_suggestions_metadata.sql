-- +goose Up

ALTER TABLE videos ADD COLUMN IF NOT EXISTS media TEXT NOT NULL DEFAULT 'file';
CREATE INDEX IF NOT EXISTS videos_media_idx ON videos (media);

CREATE TABLE creator_suggestions (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    kind           TEXT NOT NULL,
    creator_id     UUID REFERENCES creators(id) ON DELETE CASCADE,
    proposed_name  TEXT NOT NULL,
    reason         TEXT NOT NULL,
    evidence       TEXT NOT NULL DEFAULT '',
    status         TEXT NOT NULL DEFAULT 'pending',
    channel_key    TEXT NOT NULL UNIQUE
);
CREATE INDEX creator_suggestions_status_idx ON creator_suggestions (status);

CREATE TABLE creator_suggestion_members (
    suggestion_id  UUID NOT NULL REFERENCES creator_suggestions(id) ON DELETE CASCADE,
    channel_id     UUID NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
    PRIMARY KEY (suggestion_id, channel_id)
);

-- +goose Down
DROP TABLE IF EXISTS creator_suggestion_members;
DROP TABLE IF EXISTS creator_suggestions;
DROP INDEX IF EXISTS videos_media_idx;
ALTER TABLE videos DROP COLUMN IF EXISTS media;
