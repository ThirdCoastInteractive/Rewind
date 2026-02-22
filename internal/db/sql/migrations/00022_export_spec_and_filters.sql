-- +goose Up
-- Change export variant from enum to TEXT for flexibility.
-- The enum only supported 'full' and 'cropped', but we need to store
-- values like 'crop:<uuid>' and future filter variants.
ALTER TABLE clip_exports ALTER COLUMN variant DROP DEFAULT;
ALTER TABLE clip_exports ALTER COLUMN variant TYPE TEXT USING variant::TEXT;
ALTER TABLE clip_exports ALTER COLUMN variant SET DEFAULT 'full';
DROP TYPE IF EXISTS export_variant;

-- Add spec column for storing full export specification (filter pipeline, format, quality).
ALTER TABLE clip_exports ADD COLUMN IF NOT EXISTS spec JSONB;

-- Add filter_stack column to clips for persisting filter presets.
-- This allows Watch page to preview filters and exports to inherit defaults.
ALTER TABLE clips ADD COLUMN IF NOT EXISTS filter_stack JSONB DEFAULT '[]';

-- +goose Down
ALTER TABLE clips DROP COLUMN IF EXISTS filter_stack;
ALTER TABLE clip_exports DROP COLUMN IF EXISTS spec;

-- Restore enum type (values that don't match will be cast to 'full')
CREATE TYPE export_variant AS ENUM ('full', 'cropped');
UPDATE clip_exports SET variant = 'full' WHERE variant NOT IN ('full', 'cropped');
ALTER TABLE clip_exports ALTER COLUMN variant TYPE export_variant USING variant::export_variant;
