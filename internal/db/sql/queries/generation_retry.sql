-- name: EnqueueGenerationRetry :one
INSERT INTO ml_jobs(video_id,kind,status,priority,transcript_hash,model_digest,prompt_version,range_start,range_end,retry_instructions,repair_transcript)
VALUES(sqlc.arg(video_id),sqlc.arg(kind),'queued',10,sqlc.arg(transcript_hash),'',sqlc.arg(prompt_version),sqlc.narg(range_start),sqlc.narg(range_end),sqlc.arg(retry_instructions),sqlc.arg(repair_transcript))
RETURNING *;

-- name: ReplaceTranscriptCues :execrows
UPDATE video_transcripts SET text=sqlc.arg(text), search=to_tsvector('simple',sqlc.arg(text)), cues=sqlc.arg(cues),raw='',updated_at=now()
WHERE video_id=sqlc.arg(video_id) AND lang=sqlc.arg(lang);

-- name: BackupTranscriptRepair :exec
INSERT INTO transcript_repair_backups(job_id,transcript)
SELECT sqlc.arg(job_id),to_jsonb(t) FROM video_transcripts t
WHERE video_id=sqlc.arg(video_id) AND lang=sqlc.arg(lang)
ON CONFLICT(job_id) DO NOTHING;

-- name: LockTranscriptRepair :exec
SELECT video_id FROM video_transcripts WHERE video_id=sqlc.arg(video_id) FOR UPDATE;

-- name: GetRepairedVideoTranscript :one
SELECT t.* FROM video_transcripts t
WHERE t.video_id=sqlc.arg(video_id) AND EXISTS (
    SELECT 1 FROM transcript_repair_backups b JOIN ml_jobs j ON j.id=b.job_id
    WHERE j.video_id=t.video_id AND b.transcript->>'lang'=t.lang::text
)
ORDER BY CASE WHEN t.lang::text='en' THEN 0 ELSE 1 END,t.lang LIMIT 1;

-- name: TranscriptRepairPublished :one
SELECT EXISTS(SELECT 1 FROM transcript_repair_backups WHERE job_id=sqlc.arg(job_id));
