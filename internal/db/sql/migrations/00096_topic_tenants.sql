-- +goose Up
ALTER TABLE topics ADD COLUMN tenant_id UUID NOT NULL DEFAULT '00000000-0000-0000-0000-000000000000';
ALTER TABLE topic_aliases ADD COLUMN tenant_id UUID NOT NULL DEFAULT '00000000-0000-0000-0000-000000000000';
ALTER TABLE context_window_topics ADD COLUMN tenant_id UUID NOT NULL DEFAULT '00000000-0000-0000-0000-000000000000';
ALTER TABLE topic_aliases DROP CONSTRAINT topic_aliases_topic_slug_fkey;
ALTER TABLE context_window_topics DROP CONSTRAINT context_window_topics_topic_slug_fkey;
ALTER TABLE topics DROP CONSTRAINT topics_pkey;
ALTER TABLE topic_aliases DROP CONSTRAINT topic_aliases_pkey;
ALTER TABLE context_window_topics DROP CONSTRAINT context_window_topics_pkey;
ALTER TABLE topics ADD PRIMARY KEY (tenant_id, slug);
ALTER TABLE topic_aliases ADD PRIMARY KEY (tenant_id, alias_norm);
ALTER TABLE topic_aliases ADD CONSTRAINT topic_aliases_topic_fkey FOREIGN KEY (tenant_id, topic_slug) REFERENCES topics(tenant_id, slug) ON DELETE CASCADE;
ALTER TABLE context_window_topics ADD PRIMARY KEY (tenant_id, window_id, topic_slug);
ALTER TABLE context_window_topics ADD CONSTRAINT context_window_topics_topic_fkey FOREIGN KEY (tenant_id, topic_slug) REFERENCES topics(tenant_id, slug) ON DELETE CASCADE;
CREATE INDEX topic_aliases_tenant_slug_idx ON topic_aliases (tenant_id, topic_slug);
CREATE INDEX context_window_topics_tenant_topic_idx ON context_window_topics (tenant_id, topic_slug);

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN
 IF EXISTS (SELECT 1 FROM topics GROUP BY slug HAVING COUNT(DISTINCT tenant_id)>1) THEN RAISE EXCEPTION 'cannot downgrade topic tenants: duplicate slugs'; END IF;
 IF EXISTS (SELECT 1 FROM topic_aliases GROUP BY alias_norm HAVING COUNT(DISTINCT tenant_id)>1) THEN RAISE EXCEPTION 'cannot downgrade topic tenants: duplicate aliases'; END IF;
END $$;
-- +goose StatementEnd
ALTER TABLE context_window_topics DROP CONSTRAINT context_window_topics_topic_fkey;
ALTER TABLE topic_aliases DROP CONSTRAINT topic_aliases_topic_fkey;
ALTER TABLE context_window_topics DROP CONSTRAINT context_window_topics_pkey;
ALTER TABLE topic_aliases DROP CONSTRAINT topic_aliases_pkey;
ALTER TABLE topics DROP CONSTRAINT topics_pkey;
ALTER TABLE topics ADD PRIMARY KEY (slug);
ALTER TABLE topic_aliases ADD PRIMARY KEY (alias_norm);
ALTER TABLE context_window_topics ADD PRIMARY KEY (window_id, topic_slug);
ALTER TABLE topic_aliases ADD CONSTRAINT topic_aliases_topic_slug_fkey FOREIGN KEY (topic_slug) REFERENCES topics(slug) ON DELETE CASCADE;
ALTER TABLE context_window_topics ADD CONSTRAINT context_window_topics_topic_slug_fkey FOREIGN KEY (topic_slug) REFERENCES topics(slug) ON DELETE CASCADE;
DROP INDEX topic_aliases_tenant_slug_idx;
DROP INDEX context_window_topics_tenant_topic_idx;
ALTER TABLE topics DROP COLUMN tenant_id;
ALTER TABLE topic_aliases DROP COLUMN tenant_id;
ALTER TABLE context_window_topics DROP COLUMN tenant_id;
