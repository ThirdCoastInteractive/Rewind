-- +goose Up
-- Wiki vault: versioned pages with link graph and full-text search.

CREATE TABLE wiki_pages (
    tree TEXT NOT NULL CHECK (tree IN ('creator', 'channel', 'clipping', 'topic')),
    slug TEXT NOT NULL,
    title TEXT NOT NULL,
    body TEXT NOT NULL DEFAULT '',
    revision INT NOT NULL DEFAULT 1,
    creator_id UUID NULL REFERENCES creators(id) ON DELETE SET NULL,
    channel_id UUID NULL REFERENCES channels(id) ON DELETE SET NULL,
    updated_by TEXT NOT NULL DEFAULT '',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (tree, slug)
);
CREATE INDEX wiki_pages_creator_idx ON wiki_pages (creator_id) WHERE creator_id IS NOT NULL;
CREATE INDEX wiki_pages_channel_idx ON wiki_pages (channel_id) WHERE channel_id IS NOT NULL;

CREATE TABLE wiki_revisions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tree TEXT NOT NULL,
    slug TEXT NOT NULL,
    revision INT NOT NULL,
    title TEXT NOT NULL,
    body TEXT NOT NULL,
    diff TEXT NOT NULL DEFAULT '',
    summary TEXT NOT NULL,
    actor_kind TEXT NOT NULL CHECK (actor_kind IN ('user', 'agent', 'system')),
    actor_id TEXT NOT NULL,
    user_id UUID NULL REFERENCES users(id) ON DELETE SET NULL,
    session_id TEXT NOT NULL DEFAULT '',
    client_name TEXT NOT NULL DEFAULT '',
    client_version TEXT NOT NULL DEFAULT '',
    token_name TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tree, slug, revision),
    FOREIGN KEY (tree, slug) REFERENCES wiki_pages(tree, slug) ON DELETE CASCADE
);
CREATE INDEX wiki_revisions_page_idx ON wiki_revisions (tree, slug, revision DESC);

CREATE TABLE wiki_links (
    from_tree TEXT NOT NULL,
    from_slug TEXT NOT NULL,
    to_tree TEXT NOT NULL,
    to_slug TEXT NOT NULL,
    PRIMARY KEY (from_tree, from_slug, to_tree, to_slug),
    FOREIGN KEY (from_tree, from_slug) REFERENCES wiki_pages(tree, slug) ON DELETE CASCADE
);

CREATE TABLE wiki_search (
    tree TEXT NOT NULL,
    slug TEXT NOT NULL,
    search tsvector NOT NULL,
    PRIMARY KEY (tree, slug),
    FOREIGN KEY (tree, slug) REFERENCES wiki_pages(tree, slug) ON DELETE CASCADE
);
CREATE INDEX wiki_search_gin ON wiki_search USING GIN (search);

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION wiki_search_refresh() RETURNS trigger AS $$
BEGIN
    INSERT INTO wiki_search (tree, slug, search)
    VALUES (
        NEW.tree,
        NEW.slug,
        setweight(to_tsvector('simple', coalesce(NEW.slug, '')), 'A') ||
        setweight(to_tsvector('simple', coalesce(NEW.title, '')), 'A') ||
        setweight(to_tsvector('simple', coalesce(NEW.body, '')), 'B')
    )
    ON CONFLICT (tree, slug) DO UPDATE SET search = EXCLUDED.search;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER wiki_search_trg
AFTER INSERT OR UPDATE OF title, body, slug, tree ON wiki_pages
FOR EACH ROW EXECUTE FUNCTION wiki_search_refresh();

-- +goose Down
DROP TRIGGER IF EXISTS wiki_search_trg ON wiki_pages;
DROP FUNCTION IF EXISTS wiki_search_refresh();
DROP TABLE IF EXISTS wiki_search;
DROP TABLE IF EXISTS wiki_links;
DROP TABLE IF EXISTS wiki_revisions;
DROP TABLE IF EXISTS wiki_pages;
