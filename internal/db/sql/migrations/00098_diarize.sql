-- +goose Up
ALTER TABLE ml_jobs DROP CONSTRAINT IF EXISTS ml_jobs_kind_check;
ALTER TABLE ml_jobs ADD CONSTRAINT ml_jobs_kind_check CHECK (
    kind IN (
        'transcribe',
        'refine_boundaries',
        'context_windows',
        'visual_index',
        'face_index',
        'comment_classify',
        'speech_tone',
        'diarize'
    )
);

CREATE TABLE speaker_turns (
    video_id UUID PRIMARY KEY REFERENCES videos(id) ON DELETE CASCADE,
    model TEXT NOT NULL,
    fingerprint TEXT NOT NULL,
    turns JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- +goose Down
DROP TABLE IF EXISTS speaker_turns;
DELETE FROM ml_jobs WHERE kind = 'diarize';
ALTER TABLE ml_jobs DROP CONSTRAINT IF EXISTS ml_jobs_kind_check;
ALTER TABLE ml_jobs ADD CONSTRAINT ml_jobs_kind_check CHECK (
    kind IN (
        'transcribe',
        'refine_boundaries',
        'context_windows',
        'visual_index',
        'face_index',
        'comment_classify',
        'speech_tone'
    )
);
