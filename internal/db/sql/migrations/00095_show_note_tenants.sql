-- +goose Up
ALTER TABLE show_notes ADD COLUMN tenant_id UUID NOT NULL DEFAULT '00000000-0000-0000-0000-000000000000';
CREATE INDEX show_notes_tenant_id_idx ON show_notes (tenant_id);

-- +goose Down
DROP INDEX IF EXISTS show_notes_tenant_id_idx;
ALTER TABLE show_notes DROP COLUMN tenant_id;
