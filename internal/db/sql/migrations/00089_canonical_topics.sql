-- +goose Up
-- Canonical topic identities bound from context windows. Wiki topic pages
-- share slugs with topics.slug; harvested binds are not wiki_links.

CREATE TABLE topics (
    slug TEXT PRIMARY KEY,
    title TEXT NOT NULL,
    origin TEXT NOT NULL CHECK (origin IN ('wiki', 'resolver', 'stub')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE topic_aliases (
    alias_norm TEXT PRIMARY KEY,
    topic_slug TEXT NOT NULL REFERENCES topics(slug) ON DELETE CASCADE,
    raw TEXT NOT NULL,
    source TEXT NOT NULL CHECK (source IN ('wiki_title', 'window_label', 'entity', 'seed', 'merge'))
);
CREATE INDEX topic_aliases_slug_idx ON topic_aliases (topic_slug);

CREATE TABLE context_window_topics (
    window_id UUID NOT NULL REFERENCES context_windows(id) ON DELETE CASCADE,
    topic_slug TEXT NOT NULL REFERENCES topics(slug) ON DELETE CASCADE,
    raw TEXT NOT NULL,
    match_kind TEXT NOT NULL CHECK (match_kind IN ('topic_label', 'entity', 'title', 'alias')),
    PRIMARY KEY (window_id, topic_slug)
);
CREATE INDEX context_window_topics_topic_idx ON context_window_topics (topic_slug);

ALTER TABLE context_windows
    ADD COLUMN topic_resolved_at TIMESTAMPTZ;

-- +goose Down
ALTER TABLE context_windows DROP COLUMN IF EXISTS topic_resolved_at;
DROP TABLE IF EXISTS context_window_topics;
DROP TABLE IF EXISTS topic_aliases;
DROP TABLE IF EXISTS topics;
