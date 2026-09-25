-- +goose Up
UPDATE videos SET tenant_id = '00000000-0000-0000-0000-000000000000' WHERE tenant_id IS NULL;
ALTER TABLE videos ALTER COLUMN tenant_id SET DEFAULT '00000000-0000-0000-0000-000000000000';
ALTER TABLE videos ALTER COLUMN tenant_id SET NOT NULL;
DROP INDEX IF EXISTS videos_src_unique;
CREATE UNIQUE INDEX videos_src_tenant_unique ON videos (tenant_id, src);

-- +goose Down
DROP INDEX IF EXISTS videos_src_tenant_unique;
ALTER TABLE videos ALTER COLUMN tenant_id DROP NOT NULL;
ALTER TABLE videos ALTER COLUMN tenant_id DROP DEFAULT;
CREATE UNIQUE INDEX videos_src_unique ON videos (src);
