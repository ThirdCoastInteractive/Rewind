-- +goose Up
ALTER TABLE ml_jobs ADD COLUMN retry_instructions text NOT NULL DEFAULT '';
ALTER TABLE ml_jobs ADD COLUMN repair_transcript boolean NOT NULL DEFAULT false;
ALTER TABLE ml_jobs ADD CONSTRAINT repair_requires_range CHECK (NOT repair_transcript OR (kind = 'transcribe' AND range_start IS NOT NULL AND range_end IS NOT NULL));
CREATE TABLE transcript_repair_backups (
    job_id uuid PRIMARY KEY REFERENCES ml_jobs(id),
    transcript jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE transcript_repair_backups;
ALTER TABLE ml_jobs DROP CONSTRAINT repair_requires_range;
ALTER TABLE ml_jobs DROP COLUMN retry_instructions, DROP COLUMN repair_transcript;
