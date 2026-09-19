-- +goose Up
CREATE TABLE stitch_folders (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    created_by  UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    parent_id   UUID REFERENCES stitch_folders(id) ON DELETE CASCADE,
    name        TEXT NOT NULL CHECK (length(btrim(name)) BETWEEN 1 AND 120),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX idx_stitch_folders_root_name
    ON stitch_folders (created_by, lower(name))
    WHERE parent_id IS NULL;
CREATE UNIQUE INDEX idx_stitch_folders_child_name
    ON stitch_folders (created_by, parent_id, lower(name))
    WHERE parent_id IS NOT NULL;
CREATE INDEX idx_stitch_folders_user ON stitch_folders (created_by, parent_id);

ALTER TABLE stitch_projects
    ADD COLUMN folder_id UUID REFERENCES stitch_folders(id) ON DELETE SET NULL;
CREATE INDEX idx_stitch_projects_folder ON stitch_projects (created_by, folder_id, updated_at DESC);

-- Group Ben Avery chapter cuts together.
INSERT INTO stitch_folders (created_by, name)
SELECT DISTINCT created_by, 'The Show — Sept 3'
FROM stitch_projects
WHERE title ~ '^Chapter [0-9]+:'
  AND NOT EXISTS (
      SELECT 1 FROM stitch_folders f
      WHERE f.created_by = stitch_projects.created_by
        AND f.parent_id IS NULL
        AND f.name = 'The Show — Sept 3'
  );

UPDATE stitch_projects p
SET folder_id = f.id
FROM stitch_folders f
WHERE f.created_by = p.created_by
  AND f.parent_id IS NULL
  AND f.name = 'The Show — Sept 3'
  AND p.title ~ '^Chapter [0-9]+:'
  AND p.folder_id IS NULL;

-- Group Truth Seekers cuts.
INSERT INTO stitch_folders (created_by, name)
SELECT DISTINCT created_by, 'Truth Seekers'
FROM stitch_projects
WHERE title ILIKE 'Truth Seekers%'
  AND NOT EXISTS (
      SELECT 1 FROM stitch_folders f
      WHERE f.created_by = stitch_projects.created_by
        AND f.parent_id IS NULL
        AND f.name = 'Truth Seekers'
  );

UPDATE stitch_projects p
SET folder_id = f.id
FROM stitch_folders f
WHERE f.created_by = p.created_by
  AND f.parent_id IS NULL
  AND f.name = 'Truth Seekers'
  AND p.title ILIKE 'Truth Seekers%'
  AND p.folder_id IS NULL;

-- Remaining duplicate titles (same owner, same name) get a folder.
INSERT INTO stitch_folders (created_by, name)
SELECT d.created_by, d.title
FROM (
    SELECT created_by, title
    FROM stitch_projects
    WHERE folder_id IS NULL
    GROUP BY created_by, title
    HAVING COUNT(*) >= 2
) d
WHERE NOT EXISTS (
    SELECT 1 FROM stitch_folders f
    WHERE f.created_by = d.created_by
      AND f.parent_id IS NULL
      AND lower(f.name) = lower(d.title)
);

UPDATE stitch_projects p
SET folder_id = f.id
FROM stitch_folders f
WHERE f.created_by = p.created_by
  AND f.parent_id IS NULL
  AND f.name = p.title
  AND p.folder_id IS NULL
  AND (
      SELECT COUNT(*) FROM stitch_projects p2
      WHERE p2.created_by = p.created_by AND p2.title = p.title
  ) >= 2;

-- +goose Down
ALTER TABLE stitch_projects DROP COLUMN IF EXISTS folder_id;
DROP TABLE IF EXISTS stitch_folders;
