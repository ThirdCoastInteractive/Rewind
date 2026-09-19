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
        'speech_tone'
    )
);

-- +goose Down
ALTER TABLE ml_jobs DROP CONSTRAINT IF EXISTS ml_jobs_kind_check;
ALTER TABLE ml_jobs ADD CONSTRAINT ml_jobs_kind_check CHECK (
    kind IN (
        'transcribe',
        'refine_boundaries',
        'context_windows',
        'visual_index',
        'face_index'
    )
);
