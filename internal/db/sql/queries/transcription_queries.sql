-- name: EnqueueTranscription :one
INSERT INTO ml_jobs(video_id,kind,status,priority,transcript_hash,model_digest,prompt_version,range_start,range_end)
VALUES(sqlc.arg(video_id),'transcribe','queued',10,sqlc.arg(request_key),'','asr-v1',sqlc.narg(range_start),sqlc.narg(range_end))
ON CONFLICT(video_id,kind,transcript_hash,model_digest,prompt_version)
DO UPDATE SET video_id=EXCLUDED.video_id
RETURNING *;

-- name: ListTranscriptCoverage :many
SELECT lang,coverage,updated_at FROM video_transcripts WHERE video_id=sqlc.arg(video_id) ORDER BY lang;

-- name: UpsertPartialTranscript :exec
INSERT INTO video_transcripts(video_id,lang,format,text,search,raw,cues,coverage)
VALUES(sqlc.arg(video_id),sqlc.arg(lang),'vtt',sqlc.arg(text),to_tsvector('simple',sqlc.arg(text)),'',sqlc.arg(cues),sqlc.arg(coverage))
ON CONFLICT(video_id,lang) DO UPDATE SET text=EXCLUDED.text,search=EXCLUDED.search,cues=EXCLUDED.cues,raw='',coverage=EXCLUDED.coverage,updated_at=now()
WHERE video_transcripts.coverage IS NOT NULL;

-- name: RepairVideoDuration :exec
UPDATE videos SET duration_seconds=sqlc.arg(duration_seconds) WHERE id=sqlc.arg(id) AND (duration_seconds IS NULL OR duration_seconds<=0);
