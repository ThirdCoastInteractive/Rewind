-- +goose Up
ALTER TABLE videos ADD COLUMN tenant_id UUID;
CREATE INDEX videos_tenant_id_idx ON videos (tenant_id) WHERE tenant_id IS NOT NULL;

-- +goose Down
DROP INDEX IF EXISTS videos_tenant_id_idx;
ALTER TABLE videos DROP COLUMN IF EXISTS tenant_id;
