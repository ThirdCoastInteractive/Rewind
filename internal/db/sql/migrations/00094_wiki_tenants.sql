-- +goose Up
ALTER TABLE wiki_pages ADD COLUMN tenant_id UUID NOT NULL DEFAULT '00000000-0000-0000-0000-000000000000';
ALTER TABLE wiki_revisions ADD COLUMN tenant_id UUID NOT NULL DEFAULT '00000000-0000-0000-0000-000000000000';
ALTER TABLE wiki_links ADD COLUMN tenant_id UUID NOT NULL DEFAULT '00000000-0000-0000-0000-000000000000';
ALTER TABLE wiki_search ADD COLUMN tenant_id UUID NOT NULL DEFAULT '00000000-0000-0000-0000-000000000000';

ALTER TABLE wiki_revisions DROP CONSTRAINT wiki_revisions_tree_slug_revision_key;
ALTER TABLE wiki_revisions DROP CONSTRAINT wiki_revisions_tree_slug_fkey;
ALTER TABLE wiki_links DROP CONSTRAINT wiki_links_pkey;
ALTER TABLE wiki_links DROP CONSTRAINT wiki_links_from_tree_from_slug_fkey;
ALTER TABLE wiki_search DROP CONSTRAINT wiki_search_pkey;
ALTER TABLE wiki_search DROP CONSTRAINT wiki_search_tree_slug_fkey;
ALTER TABLE wiki_pages DROP CONSTRAINT wiki_pages_pkey;

ALTER TABLE wiki_pages ADD PRIMARY KEY (tenant_id, tree, slug);
ALTER TABLE wiki_revisions ADD CONSTRAINT wiki_revisions_unique UNIQUE (tenant_id, tree, slug, revision);
ALTER TABLE wiki_revisions ADD CONSTRAINT wiki_revisions_page_fkey FOREIGN KEY (tenant_id, tree, slug) REFERENCES wiki_pages(tenant_id, tree, slug) ON DELETE CASCADE;
ALTER TABLE wiki_links ADD PRIMARY KEY (tenant_id, from_tree, from_slug, to_tree, to_slug);
ALTER TABLE wiki_links ADD CONSTRAINT wiki_links_from_page_fkey FOREIGN KEY (tenant_id, from_tree, from_slug) REFERENCES wiki_pages(tenant_id, tree, slug) ON DELETE CASCADE;
ALTER TABLE wiki_search ADD PRIMARY KEY (tenant_id, tree, slug);
ALTER TABLE wiki_search ADD CONSTRAINT wiki_search_page_fkey FOREIGN KEY (tenant_id, tree, slug) REFERENCES wiki_pages(tenant_id, tree, slug) ON DELETE CASCADE;

ALTER INDEX wiki_revisions_page_idx RENAME TO wiki_revisions_page_idx_old;
CREATE INDEX wiki_revisions_page_idx ON wiki_revisions (tenant_id, tree, slug, revision DESC);
DROP INDEX wiki_revisions_page_idx_old;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION wiki_search_refresh() RETURNS trigger AS $$
BEGIN
    INSERT INTO wiki_search (tenant_id, tree, slug, search)
    VALUES (
        NEW.tenant_id, NEW.tree, NEW.slug,
        setweight(to_tsvector('simple', coalesce(NEW.slug, '')), 'A') ||
        setweight(to_tsvector('simple', coalesce(NEW.title, '')), 'A') ||
        setweight(to_tsvector('simple', coalesce(NEW.body, '')), 'B')
    )
    ON CONFLICT (tenant_id, tree, slug) DO UPDATE SET search = EXCLUDED.search;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

-- +goose Down
-- Check before changing any schema so a rejected downgrade is non-destructive.
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM wiki_pages
        GROUP BY tree, slug
        HAVING COUNT(DISTINCT tenant_id) > 1
    ) THEN
        RAISE EXCEPTION 'cannot downgrade wiki tenant migration: duplicate cross-tenant tree/slug rows exist';
    END IF;
END;
$$;
-- +goose StatementEnd
DROP TRIGGER IF EXISTS wiki_search_trg ON wiki_pages;
DROP FUNCTION IF EXISTS wiki_search_refresh();
ALTER TABLE wiki_search DROP CONSTRAINT wiki_search_page_fkey;
ALTER TABLE wiki_search DROP CONSTRAINT wiki_search_pkey;
ALTER TABLE wiki_links DROP CONSTRAINT wiki_links_from_page_fkey;
ALTER TABLE wiki_links DROP CONSTRAINT wiki_links_pkey;
ALTER TABLE wiki_revisions DROP CONSTRAINT wiki_revisions_page_fkey;
ALTER TABLE wiki_revisions DROP CONSTRAINT wiki_revisions_unique;
ALTER TABLE wiki_pages DROP CONSTRAINT wiki_pages_pkey;
ALTER TABLE wiki_pages ADD PRIMARY KEY (tree, slug);
ALTER TABLE wiki_revisions ADD CONSTRAINT wiki_revisions_tree_slug_revision_key UNIQUE (tree, slug, revision);
ALTER TABLE wiki_revisions ADD CONSTRAINT wiki_revisions_tree_slug_fkey FOREIGN KEY (tree, slug) REFERENCES wiki_pages(tree, slug) ON DELETE CASCADE;
ALTER TABLE wiki_links ADD PRIMARY KEY (from_tree, from_slug, to_tree, to_slug);
ALTER TABLE wiki_links ADD CONSTRAINT wiki_links_from_tree_from_slug_fkey FOREIGN KEY (from_tree, from_slug) REFERENCES wiki_pages(tree, slug) ON DELETE CASCADE;
ALTER TABLE wiki_search ADD PRIMARY KEY (tree, slug);
ALTER TABLE wiki_search ADD CONSTRAINT wiki_search_tree_slug_fkey FOREIGN KEY (tree, slug) REFERENCES wiki_pages(tree, slug) ON DELETE CASCADE;
DROP INDEX IF EXISTS wiki_revisions_page_idx;
CREATE INDEX wiki_revisions_page_idx ON wiki_revisions (tree, slug, revision DESC);
-- wiki_search_gin predates this migration and remains in place.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION wiki_search_refresh() RETURNS trigger AS $$
BEGIN
    INSERT INTO wiki_search (tree, slug, search)
    VALUES (NEW.tree, NEW.slug,
        setweight(to_tsvector('simple', coalesce(NEW.slug, '')), 'A') ||
        setweight(to_tsvector('simple', coalesce(NEW.title, '')), 'A') ||
        setweight(to_tsvector('simple', coalesce(NEW.body, '')), 'B'))
    ON CONFLICT (tree, slug) DO UPDATE SET search = EXCLUDED.search;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd
CREATE TRIGGER wiki_search_trg AFTER INSERT OR UPDATE OF title, body, slug, tree ON wiki_pages
FOR EACH ROW EXECUTE FUNCTION wiki_search_refresh();
ALTER TABLE wiki_pages DROP COLUMN tenant_id;
ALTER TABLE wiki_revisions DROP COLUMN tenant_id;
ALTER TABLE wiki_links DROP COLUMN tenant_id;
ALTER TABLE wiki_search DROP COLUMN tenant_id;
